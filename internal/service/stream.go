// Package service — direct-play / range request streaming.
package service

import (
	"errors"
	"net/http"
	"os"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// StreamService serves media files with proper Range support so browsers can
// seek into the stream.
//
// HLS / on-demand transcoding is intentionally omitted from this initial
// scaffold. The HTTP handler returns 501 (NotImplemented) for that path,
// while direct-play already works for browser-friendly containers (mp4 /
// webm / m4v).
type StreamService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

// NewStreamService is the constructor.
func NewStreamService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *StreamService {
	return &StreamService{cfg: cfg, log: log, repo: repo}
}

// ErrMediaNotFound is returned when the media row or its file is missing.
var ErrMediaNotFound = errors.New("media not found")

// ServeFile streams the file backing the given media ID using
// http.ServeContent so HEAD / Range / If-Modified-Since are handled for free.
func (s *StreamService) ServeFile(w http.ResponseWriter, r *http.Request, mediaID string) error {
	m, err := s.repo.Media.FindByID(r.Context(), mediaID)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrMediaNotFound
	}
	f, err := os.Open(m.Path)
	if err != nil {
		return ErrMediaNotFound
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	return nil
}
