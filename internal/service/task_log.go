package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultTaskLogTailBytes int64 = 256 * 1024
	maxTaskLogTailBytes     int64 = 1024 * 1024
	taskLogDateLayout             = "2006-01-02"
)

var ErrTaskLogDateNotFound = errors.New("task log date not found")

type taskLogStore struct {
	root string
	now  func() time.Time
	mu   sync.Mutex
}

func newTaskLogStore(dataDir string, now func() time.Time) *taskLogStore {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	return &taskLogStore{root: filepath.Join(dataDir, "task-logs"), now: now}
}

func (s *taskLogStore) append(definitionKey, level, message string) error {
	if s == nil || strings.TrimSpace(message) == "" {
		return nil
	}
	if !isTaskDefinitionKey(definitionKey) {
		return ErrTaskDefinitionNotFound
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	dir := filepath.Join(s.root, now.Format(taskLogDateLayout))
	path := filepath.Join(dir, definitionKey+".log")
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s [%s] %s\n", now.Format(time.RFC3339), strings.ToUpper(level), strings.TrimSpace(message))
	return err
}

func (s *taskLogStore) read(definitionKey, date string, tailBytes int64) (string, bool, error) {
	if s == nil {
		return "", false, nil
	}
	if !isTaskDefinitionKey(definitionKey) {
		return "", false, ErrTaskDefinitionNotFound
	}
	if !validTaskLogDate(date) {
		return "", false, ErrTaskLogDateNotFound
	}
	if tailBytes <= 0 {
		tailBytes = defaultTaskLogTailBytes
	}
	if tailBytes > maxTaskLogTailBytes {
		tailBytes = maxTaskLogTailBytes
	}
	data, truncated, err := readFileTail(filepath.Join(s.root, date, definitionKey+".log"), tailBytes)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, ErrTaskLogDateNotFound
	}
	return string(data), truncated, err
}

func (s *taskLogStore) dates(definitionKey string) ([]string, error) {
	if s == nil {
		return []string{}, nil
	}
	if !isTaskDefinitionKey(definitionKey) {
		return nil, ErrTaskDefinitionNotFound
	}
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	dates := make([]string, 0, len(entries))
	for _, entry := range entries {
		date := entry.Name()
		if !entry.IsDir() || !validTaskLogDate(date) {
			continue
		}
		if info, statErr := os.Stat(filepath.Join(s.root, date, definitionKey+".log")); statErr == nil && info.Mode().IsRegular() {
			dates = append(dates, date)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates, nil
}

func validTaskLogDate(value string) bool {
	parsed, err := time.Parse(taskLogDateLayout, value)
	return err == nil && parsed.Format(taskLogDateLayout) == value
}

func readFileTail(path string, limit int64) ([]byte, bool, error) {
	if limit <= 0 {
		return nil, true, nil
	}
	f, err := os.Open(path) // #nosec G304 -- path uses validated server-owned definition keys and dates.
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	readSize := info.Size()
	truncated := readSize > limit
	if truncated {
		readSize = limit
	}
	if _, err := f.Seek(-readSize, io.SeekEnd); err != nil {
		return nil, false, err
	}
	data := make([]byte, readSize)
	_, err = io.ReadFull(f, data)
	return data, truncated, err
}
