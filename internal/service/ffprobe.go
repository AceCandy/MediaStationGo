// Package service — ffprobe wrapper.
//
// FFprobeService shells out to the `ffprobe` binary configured in
// app.ffprobe_path and parses its JSON output into a typed struct. It is
// extracts a safe typed document plus the scalar summary stored on model.Media.
package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// FFprobeService wraps the external ffprobe binary.
type FFprobeService struct {
	cfg     *config.Config
	log     *zap.Logger
	mu      sync.RWMutex
	limiter chan struct{}
}

// NewFFprobeService is the constructor.
func NewFFprobeService(cfg *config.Config, log *zap.Logger) *FFprobeService {
	maxConcurrent := normalizeFFprobeMaxConcurrent(cfg.App.FFprobeMaxConcurrent)
	return &FFprobeService{cfg: cfg, log: log, limiter: make(chan struct{}, maxConcurrent)}
}

func normalizeFFprobeMaxConcurrent(n int) int {
	if n <= 0 {
		return 1
	}
	if n > 8 {
		return 8
	}
	return n
}

func (f *FFprobeService) SetMaxConcurrent(n int) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limiter = make(chan struct{}, normalizeFFprobeMaxConcurrent(n))
}

// ProbeResult is the subset of ffprobe output consumed by the scanner.
type ProbeResult struct {
	DurationSec int
	Width       int
	Height      int
	VideoCodec  string
	AudioCodec  string
	Container   string
	Document    *ProbeDocument
}

// Probe runs ffprobe against path and returns a typed result. A 30s timeout
// is applied so a single broken file does not hang the scanner.
func (f *FFprobeService) Probe(ctx context.Context, path string) (*ProbeResult, error) {
	if f == nil {
		return nil, errors.New("ffprobe service nil")
	}
	token, err := f.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer f.release(token)
	bin, err := resolveFFprobeExecutable(f.cfg.App.FFprobePath)
	if err != nil {
		return nil, fmt.Errorf("ffprobe unavailable: %w", err)
	}
	f.cfg.App.FFprobePath = bin
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, bin, // #nosec G204 -- bin is resolved by resolveFFprobeExecutable before execution.
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-show_chapters",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		if f.log != nil {
			f.log.Debug("ffprobe failed", zap.String("path", path), zap.Error(err))
		}
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}
	return parseProbeJSON(out)
}

// ProbeHTTP runs ffprobe against a public HTTP(S) media URL.
func (f *FFprobeService) ProbeHTTP(ctx context.Context, rawURL string) (*ProbeResult, error) {
	if f == nil {
		return nil, errors.New("ffprobe service nil")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("empty probe url")
	}
	token, err := f.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer f.release(token)
	bin, err := resolveFFprobeExecutable(f.cfg.App.FFprobePath)
	if err != nil {
		return nil, fmt.Errorf("ffprobe unavailable: %w", err)
	}
	f.cfg.App.FFprobePath = bin
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"-v", "error", "-print_format", "json", "-show_format", "-show_streams", "-show_chapters", rawURL}
	cmd := exec.CommandContext(probeCtx, bin, args...) // #nosec G204 -- bin is resolved by resolveFFprobeExecutable before execution.
	out, err := cmd.Output()
	if err != nil {
		if f.log != nil {
			f.log.Debug("remote ffprobe failed", zap.Error(err))
		}
		return nil, fmt.Errorf("remote ffprobe failed: %w", err)
	}
	return parseProbeJSON(out)
}

func (f *FFprobeService) acquire(ctx context.Context) (chan struct{}, error) {
	f.mu.RLock()
	limiter := f.limiter
	f.mu.RUnlock()
	if limiter == nil {
		return nil, nil
	}
	select {
	case limiter <- struct{}{}:
		return limiter, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *FFprobeService) release(limiter chan struct{}) {
	if limiter == nil {
		return
	}
	select {
	case <-limiter:
	default:
	}
}
