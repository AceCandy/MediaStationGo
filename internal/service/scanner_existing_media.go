package service

import (
	"context"
	"path/filepath"
	"strings"

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
