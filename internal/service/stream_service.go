package service

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	STRMEnabledSettingKey                     = "strm.enabled"
	PlaybackPathMappingsSettingKey            = "playback.path_mappings"
	PlaybackRedirectResolvePrefixesSettingKey = "playback.redirect_resolve_prefixes"
)

// StreamService serves media files with proper Range support so browsers can
// seek into the stream.
type StreamService struct {
	cfg              *config.Config
	log              *zap.Logger
	repo             *repository.Container
	mediaProbe       *MediaProbeService
	redirectResolver *playbackRedirectResolver
}

func (s *StreamService) SetMediaProbe(mediaProbe *MediaProbeService) {
	if s != nil {
		s.mediaProbe = mediaProbe
	}
}

type localMediaProber interface {
	Probe(ctx context.Context, path string) (*ProbeResult, error)
}

// NewStreamService is the constructor.
func NewStreamService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *StreamService {
	return &StreamService{
		cfg:              cfg,
		log:              log,
		repo:             repo,
		redirectResolver: newPlaybackRedirectResolver(),
	}
}

// ErrMediaNotFound is returned when the media row or its file is missing.
var ErrMediaNotFound = errors.New("media not found")

// Probe re-runs ffprobe against an existing media row and refreshes the
// extracted metadata. Used by the admin UI's "rescan" button.
func (s *StreamService) Probe(ctx context.Context, mediaID string, _ localMediaProber) error {
	if s.mediaProbe == nil {
		return errors.New("media probe unavailable")
	}
	_, err := s.mediaProbe.ProbeMedia(ctx, mediaID)
	return err
}
