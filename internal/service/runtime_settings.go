package service

import (
	"context"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func ApplyRuntimeSettings(ctx context.Context, cfg *config.Config, repos *repository.Container, log *zap.Logger) {
	if cfg == nil || repos == nil || repos.Setting == nil {
		return
	}
	rows, err := repos.Setting.All(ctx)
	if err != nil {
		if log != nil {
			log.Warn("load runtime settings failed", zap.Error(err))
		}
		return
	}
	for _, row := range rows {
		ApplyRuntimeSetting(cfg, row.Key, row.Value)
	}
}

func ApplyRuntimeSetting(cfg *config.Config, key, value string) {
	if cfg == nil {
		return
	}
	value = strings.TrimSpace(value)
	switch key {
	case "app.server_url", "server.url", "public.server_url", "strm.base_url":
		cfg.App.ServerURL = strings.TrimRight(value, "/")
	case "ffprobe.path", "app.ffprobe_path":
		cfg.App.FFprobePath = value
	case "ffprobe.max_concurrent", "app.ffprobe_max_concurrent":
		if n, err := strconv.Atoi(value); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 8 {
				n = 8
			}
			cfg.App.FFprobeMaxConcurrent = n
		}
	case "app.max_cpu_threads", "runtime.max_cpu_threads":
		if n, err := strconv.Atoi(value); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 8 {
				n = 8
			}
			cfg.App.MaxCPUThreads = n
		}
	}
}

// ParseBoolSetting is the exported variant of parseBoolSetting for handlers
// that need to interpret persisted on/off settings (accepts zh + en tokens).
func ParseBoolSetting(value string, fallback bool) bool {
	return parseBoolSetting(value, fallback)
}

func parseBoolSetting(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled", "启用", "开启":
		return true
	case "0", "false", "no", "off", "disabled", "禁用", "关闭":
		return false
	default:
		return fallback
	}
}
