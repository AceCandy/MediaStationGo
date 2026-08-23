package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// RemovePath deletes the media row for a path that has disappeared from disk
// (incremental delete used by the watcher on Remove/Rename events).
func (s *ScannerService) RemovePath(ctx context.Context, path string) (int64, error) {
	if _, err := os.Stat(path); err == nil {
		return 0, nil // still exists; nothing to remove
	}
	var metadataIDs []string
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("path = ?", path).Where("metadata_id IS NOT NULL").Pluck("metadata_id", &metadataIDs).Error; err != nil {
		return 0, err
	}
	res := s.repo.DB.WithContext(ctx).
		Unscoped().
		Where("path = ?", path).
		Delete(&model.Media{})
	if res.Error == nil && res.RowsAffected > 0 {
		s.repo.MediaView.RefreshMetadataIDs(ctx, metadataIDs...)
		s.invalidateMediaCache(ctx)
	}
	return res.RowsAffected, res.Error
}

func (s *ScannerService) pruneMissingMedia(ctx context.Context, libraryID string, seen map[string]struct{}) (int64, error) {
	// 只取 id/path，并把删除按批提交：此前整表载入完整 Media 结构体、
	// 每行一条 DELETE，大库 prune 既费内存又长期占用写锁。
	var rows []struct {
		ID   string
		Path string
	}
	if err := s.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Select("id, path").
		Where("library_id = ?", libraryID).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	stale := make([]string, 0)
	for _, row := range rows {
		if row.Path == "" {
			continue
		}
		if _, ok := seen[filepath.Clean(row.Path)]; ok {
			continue
		}
		if _, err := os.Stat(row.Path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			continue
		}
		stale = append(stale, row.ID)
	}
	return s.deleteMediaByIDs(ctx, stale)
}

func (s *ScannerService) pruneMissingMediaForRoot(ctx context.Context, libraryID, rootID, rootPath string, seen map[string]struct{}) (int64, []string, error) {
	var rows []struct {
		ID            string
		Path          string
		LibraryRootID string
	}
	q := s.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Select("id, path, library_root_id").
		Where("library_id = ? AND path NOT LIKE ?", libraryID, "cloud://%")
	if strings.TrimSpace(rootID) != "" {
		q = q.Where("library_root_id = ? OR library_root_id = '' OR library_root_id IS NULL", rootID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return 0, nil, err
	}
	stale := make([]string, 0)
	stalePaths := make([]string, 0)
	for _, row := range rows {
		if row.Path == "" {
			continue
		}
		if row.LibraryRootID == "" && !pathBelongsToRoot(row.Path, rootPath) {
			continue
		}
		if _, ok := seen[filepath.Clean(row.Path)]; ok {
			continue
		}
		if _, err := os.Stat(row.Path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			continue
		}
		stale = append(stale, row.ID)
		stalePaths = append(stalePaths, row.Path)
	}
	removed, err := s.deleteMediaByIDs(ctx, stale)
	if err != nil {
		return removed, nil, err
	}
	return removed, stalePaths, nil
}

func pathBelongsToRoot(pathValue, rootPath string) bool {
	pathValue = filepath.Clean(strings.TrimSpace(pathValue))
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	if pathValue == "" || rootPath == "" || pathValue == "." || rootPath == "." {
		return false
	}
	return strings.EqualFold(pathValue, rootPath) || pathWithin(pathValue, rootPath)
}

// deleteMediaByIDs removes media rows in fixed-size batches so each write
// transaction stays short and the global write gate is released frequently.
func (s *ScannerService) deleteMediaByIDs(ctx context.Context, ids []string) (int64, error) {
	const batch = 500
	var removed int64
	for i := 0; i < len(ids); i += batch {
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		var metadataIDs []string
		if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id IN ?", ids[i:end]).Where("metadata_id IS NOT NULL").Pluck("metadata_id", &metadataIDs).Error; err != nil {
			return removed, err
		}
		res := s.repo.DB.WithContext(ctx).Unscoped().Where("id IN ?", ids[i:end]).Delete(&model.Media{})
		if res.Error != nil {
			return removed, res.Error
		}
		removed += res.RowsAffected
		s.repo.MediaView.RefreshMetadataIDs(ctx, metadataIDs...)
	}
	return removed, nil
}
