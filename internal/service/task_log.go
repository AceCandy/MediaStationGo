package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultTaskLogTailBytes int64 = 256 * 1024
	maxTaskLogTailBytes     int64 = 1024 * 1024
)

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

func (s *taskLogStore) append(taskID, level, message string) error {
	if s == nil || strings.TrimSpace(message) == "" {
		return nil
	}
	if _, err := uuid.Parse(taskID); err != nil {
		return errors.New("invalid task id")
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	dir := filepath.Join(s.root, now.Format("2006-01-02"))
	path := filepath.Join(dir, taskID+".log")
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

func (s *taskLogStore) read(taskID string, startedAt, endedAt time.Time, tailBytes int64) (string, bool, error) {
	if s == nil {
		return "", false, nil
	}
	if _, err := uuid.Parse(taskID); err != nil {
		return "", false, errors.New("invalid task id")
	}
	if tailBytes <= 0 {
		tailBytes = defaultTaskLogTailBytes
	}
	if tailBytes > maxTaskLogTailBytes {
		tailBytes = maxTaskLogTailBytes
	}
	if endedAt.IsZero() {
		endedAt = time.Now()
		if s.now != nil {
			endedAt = s.now()
		}
	}
	location := endedAt.Location()
	if s.now != nil {
		location = s.now().Location()
	}
	startedAt = startedAt.In(location)
	endedAt = endedAt.In(location)
	var content []byte
	truncated := false
	startDay := dateOnly(startedAt)
	for day := dateOnly(endedAt); !day.Before(startDay); day = day.AddDate(0, 0, -1) {
		path := filepath.Join(s.root, day.Format("2006-01-02"), taskID+".log")
		data, fileTruncated, err := readFileTail(path, tailBytes-int64(len(content)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", false, err
		}
		content = append(data, content...)
		truncated = truncated || fileTruncated
		if int64(len(content)) >= tailBytes {
			if day.After(startDay) {
				truncated = true
			}
			break
		}
	}
	return string(content), truncated, nil
}

func readFileTail(path string, limit int64) ([]byte, bool, error) {
	if limit <= 0 {
		return nil, true, nil
	}
	f, err := os.Open(path) // #nosec G304 -- path is derived from a validated UUID and server-owned dates.
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

func dateOnly(value time.Time) time.Time {
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, value.Location())
}
