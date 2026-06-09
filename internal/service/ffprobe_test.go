package service

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestFFprobeServiceDefaultsToSingleConcurrentProbe(t *testing.T) {
	svc := NewFFprobeService(&config.Config{}, zap.NewNop())
	if got := cap(svc.limiter); got != 1 {
		t.Fatalf("limiter capacity = %d, want 1", got)
	}
}

func TestFFprobeServiceClampsConfiguredConcurrency(t *testing.T) {
	cfg := &config.Config{}
	cfg.App.FFprobeMaxConcurrent = 3
	svc := NewFFprobeService(cfg, zap.NewNop())
	if got := cap(svc.limiter); got != 3 {
		t.Fatalf("limiter capacity = %d, want 3", got)
	}

	cfg.App.FFprobeMaxConcurrent = 99
	svc = NewFFprobeService(cfg, zap.NewNop())
	if got := cap(svc.limiter); got != 8 {
		t.Fatalf("limiter capacity = %d, want max clamp 8", got)
	}
}

func TestFFprobeAcquireHonorsContextWhenLimitReached(t *testing.T) {
	svc := &FFprobeService{limiter: make(chan struct{}, 1)}
	token, err := svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := svc.acquire(ctx); err == nil {
		t.Fatal("second acquire should block until context deadline")
	}
	svc.release(token)
	token, err = svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	svc.release(token)
}

func TestFFprobeSetMaxConcurrentHotSwapsLimiter(t *testing.T) {
	svc := NewFFprobeService(&config.Config{}, zap.NewNop())
	firstToken, err := svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	svc.SetMaxConcurrent(2)
	secondToken, err := svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire after resize: %v", err)
	}
	thirdToken, err := svc.acquire(t.Context())
	if err != nil {
		t.Fatalf("second acquire after resize: %v", err)
	}
	svc.release(firstToken)
	svc.release(secondToken)
	svc.release(thirdToken)
}

func TestApplyRuntimeSettingFFprobeMaxConcurrent(t *testing.T) {
	cfg := &config.Config{}
	ApplyRuntimeSetting(cfg, "ffprobe.max_concurrent", "4")
	if cfg.App.FFprobeMaxConcurrent != 4 {
		t.Fatalf("FFprobeMaxConcurrent = %d, want 4", cfg.App.FFprobeMaxConcurrent)
	}
	ApplyRuntimeSetting(cfg, "ffprobe.max_concurrent", "99")
	if cfg.App.FFprobeMaxConcurrent != 8 {
		t.Fatalf("FFprobeMaxConcurrent = %d, want clamp 8", cfg.App.FFprobeMaxConcurrent)
	}
	ApplyRuntimeSetting(cfg, "ffprobe.max_concurrent", "0")
	if cfg.App.FFprobeMaxConcurrent != 1 {
		t.Fatalf("FFprobeMaxConcurrent = %d, want clamp 1", cfg.App.FFprobeMaxConcurrent)
	}
}
