package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestApplyRuntimeSettingMaxCPUThreads(t *testing.T) {
	cfg := &config.Config{}
	ApplyRuntimeSetting(cfg, "app.max_cpu_threads", "99")
	if cfg.App.MaxCPUThreads != 8 {
		t.Fatalf("max cpu threads = %d, want clamp 8", cfg.App.MaxCPUThreads)
	}

	ApplyRuntimeSetting(cfg, "app.max_cpu_threads", "0")
	if cfg.App.MaxCPUThreads != 1 {
		t.Fatalf("max cpu threads = %d, want clamp 1", cfg.App.MaxCPUThreads)
	}
}
