// Package service — filesystem scanner.
//
// ScannerService walks the configured library roots looking for video files,
// then upserts a model.Media row per file. A future iteration will plug
// ffprobe / a metadata-provider chain on top of this skeleton, but the
// scaffold keeps the surface narrow and synchronous so handlers can call
// "POST /api/libraries/:id/scan" today.
package service

import (
	"context"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// videoExtensions lists the file extensions treated as media. Matches the
// MediaStation Python defaults.
var videoExtensions = map[string]struct{}{
	".mkv":  {},
	".mp4":  {},
	".m4v":  {},
	".avi":  {},
	".mov":  {},
	".webm": {},
	".ts":   {},
	".rmvb": {},
	".rm":   {},
	".3gp":  {},
	".mpg":  {},
	".mpeg": {},
	".strm": {},
}

// ScannerService walks libraries on disk and upserts model.Media rows.
type ScannerService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
	hub  *Hub
}

// NewScannerService is the constructor.
func NewScannerService(cfg *config.Config, log *zap.Logger, repo *repository.Container, hub *Hub) *ScannerService {
	return &ScannerService{cfg: cfg, log: log, repo: repo, hub: hub}
}

// ScanResult summarises a scan run.
type ScanResult struct {
	LibraryID string `json:"library_id"`
	Visited   int    `json:"visited"`
	Added     int    `json:"added"`
}

// ScanLibrary walks the library root and persists discovered media files.
//
// This is a synchronous skeleton: large libraries should call it in a
// goroutine. WebSocket progress events are pushed to the hub on the
// "scan" topic so the React UI can display a progress indicator.
func (s *ScannerService) ScanLibrary(ctx context.Context, libraryID string) (*ScanResult, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil {
		return nil, err
	}
	res := &ScanResult{LibraryID: lib.ID}
	walkFn := func(path string, info walkInfo) error {
		if info.isDir {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := videoExtensions[ext]; !ok {
			return nil
		}
		res.Visited++
		title := strings.TrimSuffix(filepath.Base(path), ext)
		m := &model.Media{
			LibraryID: lib.ID,
			Title:     title,
			Path:      path,
			SizeBytes: info.size,
			Container: strings.TrimPrefix(ext, "."),
		}
		if err := s.repo.Media.Upsert(ctx, m); err != nil {
			s.log.Warn("upsert media failed", zap.String("path", path), zap.Error(err))
			return nil
		}
		res.Added++
		s.hub.Publish("scan", map[string]any{
			"library_id": lib.ID,
			"path":       path,
			"visited":    res.Visited,
			"added":      res.Added,
		})
		return nil
	}
	if err := walk(lib.Path, walkFn); err != nil {
		return res, err
	}
	s.hub.Publish("scan", map[string]any{
		"library_id": lib.ID,
		"finished":   true,
		"visited":    res.Visited,
		"added":      res.Added,
	})
	return res, nil
}
