// Package service — disk usage breakdown.
//
// StorageService aggregates "how much disk does each library use" for
// the React Storage tab. Numbers are computed from the in-DB
// media_probe_metadata.size_bytes column so we never hit the disk on the hot path.
package service

import (
	"context"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// StorageService is the read-only aggregator.
type StorageService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

// NewStorageService is the constructor.
func NewStorageService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *StorageService {
	return &StorageService{cfg: cfg, log: log, repo: repo}
}

// Breakdown is what /api/storage returns.
type Breakdown struct {
	TotalBytes   int64           `json:"total_bytes"`
	TotalSeconds int64           `json:"total_seconds"`
	ByLibrary    []LibraryUsage  `json:"by_library"`
	ByContainer  []ContainerStat `json:"by_container"`
}

// LibraryUsage is per-library disk + duration totals.
type LibraryUsage struct {
	LibraryID    string `json:"library_id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Path         string `json:"path"`
	MediaCount   int64  `json:"media_count"`
	TotalBytes   int64  `json:"total_bytes"`
	TotalSeconds int64  `json:"total_seconds"`
}

// ContainerStat counts media items per container (mp4 / mkv / …).
type ContainerStat struct {
	Container string `json:"container"`
	Count     int64  `json:"count"`
	Bytes     int64  `json:"bytes"`
}

// Compute returns the full breakdown.
func (s *StorageService) Compute(ctx context.Context) (*Breakdown, error) {
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		return nil, err
	}
	out := &Breakdown{ByLibrary: make([]LibraryUsage, 0, len(libs))}
	for _, l := range libs {
		var usage LibraryUsage
		name, mediaType := s.libraryDisplay(l)
		usage.LibraryID = l.ID
		usage.Name = name
		usage.Type = mediaType
		usage.Path = l.Path
		row := struct {
			Count   int64
			Size    int64
			Seconds int64
		}{}
		err := s.repo.DB.WithContext(ctx).
			Table("media AS m").
			Joins("LEFT JOIN media_probe_metadata AS pm ON pm.media_id = m.id").
			Where("m.library_id = ? AND m.deleted_at IS NULL", l.ID).
			Select("COUNT(*) as count, COALESCE(SUM(pm.size_bytes),0) as size, COALESCE(SUM(pm.duration_ms),0)::bigint / 1000 as seconds").
			Scan(&row).Error
		if err != nil {
			return nil, err
		}
		usage.MediaCount = row.Count
		usage.TotalBytes = row.Size
		usage.TotalSeconds = row.Seconds
		out.TotalBytes += row.Size
		out.TotalSeconds += row.Seconds
		out.ByLibrary = append(out.ByLibrary, usage)
	}

	rows, err := s.containerStats(ctx)
	if err != nil {
		return nil, err
	}
	out.ByContainer = rows
	return out, nil
}

func (s *StorageService) libraryDisplay(l model.Library) (string, string) {
	var categories map[string]string
	if s.cfg != nil {
		categories = s.cfg.Organizer.Categories
	}
	for _, candidate := range []string{l.Name, filepath.Base(filepath.Clean(l.Path))} {
		if hint, ok := findSourceCategoryHint(candidate, l.Type, categories); ok {
			return categoryName(categories, hint.Key, hint.Fallback), hint.MediaType
		}
	}
	return l.Name, l.Type
}

func (s *StorageService) containerStats(ctx context.Context) ([]ContainerStat, error) {
	rows, err := s.repo.DB.WithContext(ctx).
		Table("media AS m").
		Joins("LEFT JOIN media_probe_metadata AS pm ON pm.media_id = m.id").
		Where("m.deleted_at IS NULL").
		Select("COALESCE(NULLIF(pm.container,''),'unknown') as container, COUNT(*) as count, COALESCE(SUM(pm.size_bytes),0) as bytes").
		Group("pm.container").
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContainerStat{}
	for rows.Next() {
		var c ContainerStat
		if err := rows.Scan(&c.Container, &c.Count, &c.Bytes); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
