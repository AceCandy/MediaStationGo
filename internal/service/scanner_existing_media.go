package service

import (
	"context"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (s *ScannerService) existingLocalMediaSnapshot(ctx context.Context, libraryID string) (map[string]existingLocalMedia, error) {
	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Select("path", "scan_file_size_bytes", "scan_file_mtime_ns", "file_id").
		Where("library_id = ? AND path NOT LIKE ?", libraryID, "cloud://%").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	snapshot := make(map[string]existingLocalMedia, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Path) == "" {
			continue
		}
		snapshot[filepath.Clean(row.Path)] = existingLocalMedia{
			ScanFileSizeBytes: row.ScanFileSizeBytes,
			ScanFileMTimeNS:   row.ScanFileMTimeNS,
			FileID:            row.FileID,
		}
	}
	return snapshot, nil
}

type dirtyMovieMedia struct {
	ID           string
	MetadataID   string
	MetadataKind string
	ScrapeStatus string
}

// reconcileMovieLibraryEpisodes 清理本次重扫路径中历史遗留的电影季集字段。
func (s *ScannerService) reconcileMovieLibraryEpisodes(ctx context.Context, lib *model.Library, rootID string) (int, error) {
	if s == nil || s.repo == nil || s.repo.DB == nil || !libraryIsMovieType(lib) {
		return 0, nil
	}
	query := s.repo.DB.WithContext(ctx).Table("media AS m").
		Select("m.id, m.metadata_id, COALESCE(mi.kind, '') AS metadata_kind, m.scrape_status").
		Joins("LEFT JOIN metadata_items AS mi ON mi.id = m.metadata_id").
		Where("m.library_id = ? AND (m.season_num <> 0 OR m.episode_num <> 0)", lib.ID)
	if strings.TrimSpace(rootID) != "" {
		query = query.Where("(m.library_root_id = ? OR m.library_root_id = '')", rootID)
	}
	var rows []dirtyMovieMedia
	if err := query.Scan(&rows).Error; err != nil || len(rows) == 0 {
		return 0, err
	}

	staleMetadataIDs := make([]string, 0, len(rows))
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			wrongBinding := row.MetadataID != "" && row.MetadataKind != model.MetadataKindMovie
			updates := map[string]any{"season_num": 0, "episode_num": 0, "series_hint": ""}
			if wrongBinding {
				updates["metadata_id"] = nil
				staleMetadataIDs = append(staleMetadataIDs, row.MetadataID)
			}
			if wrongBinding || row.ScrapeStatus == "error" || row.ScrapeStatus == "no_match" {
				updates["scrape_status"] = "pending"
				updates["scrape_error"] = ""
			}
			if err := tx.Model(&model.Media{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if s.repo.MediaView != nil {
		s.repo.MediaView.RefreshMetadataIDs(ctx, staleMetadataIDs...)
	}
	return len(rows), nil
}
