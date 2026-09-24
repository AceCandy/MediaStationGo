package repository

import (
	"context"
	"strings"
)

// SearchCandidateDetails 只读取排序字段；文件版本、图片和用户详情留到最终页再加载。
func (r *MediaViewRepository) SearchCandidateDetails(ctx context.Context, ids []string) ([]MetadataSearchCandidate, error) {
	legacyIDs, localIDs := []string{}, []string{}
	for _, id := range ids {
		if strings.HasPrefix(id, "nfo-") {
			localIDs = append(localIDs, strings.TrimPrefix(id, "nfo-"))
		} else {
			legacyIDs = append(legacyIDs, id)
		}
	}
	candidates := []MetadataSearchCandidate{}
	for _, source := range []struct {
		table, prefix string
		ids           []string
	}{{"metadata_items", "", legacyIDs}, {"nfo_items", "nfo-", localIDs}} {
		if len(source.ids) == 0 {
			continue
		}
		var rows []struct {
			MetadataSearchCandidate
			ItemKind string `gorm:"column:kind"`
		}
		if err := r.db.WithContext(ctx).Table(source.table).Select("id, kind, title, original_name, overview, genres, year").Where("id = ANY(?)", &source.ids).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			row.ID, row.Kind = source.prefix+row.ID, row.ItemKind
			candidates = append(candidates, row.MetadataSearchCandidate)
		}
	}
	return candidates, nil
}
