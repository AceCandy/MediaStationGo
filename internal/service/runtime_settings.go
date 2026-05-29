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
	case "ffmpeg.path", "app.ffmpeg_path":
		cfg.App.FFmpegPath = value
	case "ffprobe.path", "app.ffprobe_path":
		cfg.App.FFprobePath = value
	case "transcode.hw_accel", "transcoder.encoder":
		switch value {
		case "", "auto", "none", "software":
			cfg.Transcoder.Encoder = ""
		case "nvenc", "qsv", "vaapi":
			cfg.Transcoder.Encoder = value
		}
	case "transcode.max_height", "transcoder.max_height":
		if n, err := strconv.Atoi(value); err == nil {
			cfg.Transcoder.MaxHeight = n
		}
	case "transcode.video_bitrate", "transcoder.video_bitrate":
		cfg.Transcoder.VideoBitrate = value
	case "license.server_url":
		cfg.License.ServerURL = value
	case "license.hmac_secret":
		cfg.License.HMACSecret = value
	}
}
