package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func ffprobeExecutableCandidates(configured string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 12)
	add := func(path string) {
		path = strings.Trim(path, `" `)
		if path == "" {
			return
		}
		if !strings.EqualFold(filepath.Ext(path), ".exe") && runtime.GOOS == "windows" && !strings.ContainsAny(path, `\/`) {
			path += ".exe"
		}
		if resolved, err := exec.LookPath(path); err == nil {
			path = resolved
		}
		if stat, err := os.Stat(path); err != nil || stat.IsDir() {
			return
		}
		clean, err := filepath.Abs(path)
		if err == nil {
			path = clean
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}

	if configured != "" {
		add(configured)
	}
	add("ffprobe")

	for _, path := range localFFprobeExecutableCandidates() {
		add(path)
	}
	return out
}

func resolveFFprobeExecutable(configured string) (string, error) {
	for _, path := range ffprobeExecutableCandidates(configured) {
		return path, nil
	}
	return "", fmt.Errorf("ffprobe not found in PATH or common local app directories")
}

func localFFprobeExecutableCandidates() []string {
	exe := "ffprobe"
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(exe), ".exe") {
		exe += ".exe"
	}

	out := make([]string, 0, 24)
	add := func(path string) {
		if path != "" {
			out = append(out, path)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		add(filepath.Join(wd, "bin", exe))
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		add(filepath.Join(exeDir, exe))
	}
	return out
}
