package repository

import (
	"context"
	"errors"
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// DiscoverIdentity 是发现卡片的作品身份，电影与整剧的数字 ID 互不通用。
type DiscoverIdentity struct {
	TMDbID    int    `json:"tmdb_id"`
	MediaType string `json:"media_type"`
}

func (id DiscoverIdentity) Kind() string {
	if id.MediaType == "movie" {
		return model.MetadataKindMovie
	}
	if id.MediaType == "tv" {
		return model.MetadataKindSeries
	}
	return ""
}

func (id DiscoverIdentity) Valid() bool { return id.TMDbID > 0 && id.Kind() != "" }

// FindDiscoverPresentation 读取共享目录作品资料，不以媒体文件存在或库权限为前提。
func (r *MediaViewRepository) FindDiscoverPresentation(ctx context.Context, metadataID string) (*model.MediaView, error) {
	rows, err := r.metadataSearchPresentations(ctx, []string{metadataID})
	if err != nil {
		return nil, err
	}
	row, ok := rows[metadataID]
	if !ok || (row.Kind != model.MetadataKindMovie && row.Kind != model.MetadataKindSeries) {
		return nil, nil
	}
	views := []model.MediaView{{}}
	applyMetadataSearchPresentation(&views[0], row)
	if err := attachMediaViewDoubanRatings(r.db.WithContext(ctx), views); err != nil {
		return nil, err
	}
	return &views[0], nil
}

// FindDiscoverLibraryItems 只投影当前可见的作品身份，不加载每个分集或媒体版本。
func (r *MediaViewRepository) FindDiscoverLibraryItems(ctx context.Context, items []DiscoverIdentity, filter MediaQueryFilter) ([]DiscoverIdentity, error) {
	out := []DiscoverIdentity{}
	if len(items) == 0 {
		return out, nil
	}
	if len(items) > 100 {
		return nil, errors.New("最多查询 100 个作品")
	}
	pairs := make([][]any, 0, len(items))
	for _, item := range items {
		if !item.Valid() {
			return nil, errors.New("作品身份无效")
		}
		pairs = append(pairs, []any{item.Kind(), strconv.Itoa(item.TMDbID)})
	}
	q := r.db.WithContext(ctx).Table("metadata_identifiers AS ident").
		Joins("JOIN metadata_items AS work ON work.id = ident.metadata_id AND work.kind = ident.entity_kind").
		Joins(`JOIN LATERAL (
			SELECT work.id
			UNION ALL SELECT s.id FROM metadata_items s WHERE work.kind = 'series' AND s.parent_id = work.id AND s.kind = 'season'
			UNION ALL SELECT e.id FROM metadata_items s JOIN metadata_items e ON e.parent_id = s.id AND e.kind = 'episode'
			WHERE work.kind = 'series' AND s.parent_id = work.id AND s.kind = 'season'
		) AS owned ON TRUE`).
		Joins("JOIN media AS m ON m.metadata_id = owned.id").
		Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id").
		Joins("JOIN libraries AS lib ON lib.id = m.library_id AND lib.enabled = TRUE AND lib.deleted_at IS NULL").
		Where("ident.provider = 'tmdb' AND (ident.entity_kind, ident.external_id) IN ?", pairs)
	if !filter.IncludeNSFW {
		q = q.Where("COALESCE(work.nsfw, FALSE) = FALSE AND COALESCE(mi.nsfw, FALSE) = FALSE")
	}
	err := applyMediaViewFilter(q, filter).
		Select("DISTINCT ident.external_id::bigint AS tm_db_id, CASE WHEN work.kind = 'series' THEN 'tv' ELSE 'movie' END AS media_type").Scan(&out).Error
	return out, err
}
