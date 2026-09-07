package service

import (
	"context"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// containerDetail 在数据库折叠版本和 Part，仅加载一个代表文件以保留详情的库归属与展示回退。
// 完整分集查询仍供播放使用；摘要不得写入完整剧集缓存。
func (e *EmbyService) containerDetail(ctx context.Context, metadata *model.MetadataItem, userID string) (map[string]any, error) {
	q := e.applyUserMediaVisibility(ctx, e.repo.DB.WithContext(ctx).Model(&model.Media{}), userID)
	q = seriesScopeQuery(q).Where("media.season_num > 0 OR media.episode_num > 0")
	order := "scope_season.season_num, emby_metadata.episode_num, media.created_at DESC, media.id"
	if metadata.Kind == model.MetadataKindSeason {
		q = q.Where("scope_season.id = ?", metadata.ID)
		order = "media.season_num, media.episode_num, media.created_at, media.id"
	} else {
		q = q.Where("scope_series.id = ?", metadata.ID)
	}
	// 与旧加载链路第二次 MediaView 可见性过滤保持一致。
	if hidden := e.mediaQueryFilter(ctx, userID).HiddenLibraryIDs; len(hidden) > 0 {
		q = q.Where("media.library_id <> ALL(?)", &hidden)
	}
	q = q.Joins("LEFT JOIN media_probe_metadata AS probe ON probe.media_id = media.id").
		Select(`media.id, media.metadata_id, media.created_at, scope_season.id AS season_id,
			COALESCE(media.part_group_key, '') AS part_group_key, COALESCE(media.part_index, 0) AS part_index,
			COALESCE(media.strm_url, '') AS strm_url, COALESCE(probe.width, 0) AS width,
			COALESCE(probe.size_bytes, 0) AS size_bytes, ROW_NUMBER() OVER (ORDER BY ` + order + `) AS input_order`)
	var summary struct {
		MediaID      string
		CreatedAt    time.Time
		EpisodeCount int
		SeasonCount  int
	}
	// 顺序对应 collapseMediaParts、preferredMetadataViewsInOrder：先保留最小 Part，再优选每集版本。
	err := e.repo.DB.WithContext(ctx).Raw(`WITH scoped AS (?), parts AS (
		SELECT *, ROW_NUMBER() OVER (
			PARTITION BY (part_group_key <> '' AND part_index > 0),
			CASE WHEN part_group_key <> '' AND part_index > 0 THEN part_group_key ELSE id END
			ORDER BY part_index, input_order) AS part_rank,
			MIN(input_order) OVER (PARTITION BY metadata_id) AS episode_order
		FROM scoped
	), preferred AS (
		SELECT DISTINCT ON (metadata_id) * FROM parts WHERE part_rank = 1
		ORDER BY metadata_id, (BTRIM(strm_url, E' \t\n\r') <> ''), width DESC, size_bytes DESC, created_at DESC, input_order
	)
	SELECT (ARRAY_AGG(id ORDER BY episode_order))[1] AS media_id, MAX(created_at) AS created_at,
		COUNT(*) AS episode_count, COUNT(DISTINCT season_id) AS season_count
	FROM preferred HAVING COUNT(*) > 0`, q).Scan(&summary).Error
	if err != nil || summary.EpisodeCount == 0 {
		return nil, err
	}
	views, err := e.repo.MediaView.FindByIDs(ctx, []string{summary.MediaID}, e.mediaQueryFilter(ctx, userID))
	if err != nil || len(views) == 0 {
		return nil, err
	}
	groups := e.seriesGroupsFromMedia(views)
	if len(groups) == 0 {
		return nil, nil
	}
	group := groups[0]
	if metadata.Kind == model.MetadataKindSeason {
		for _, season := range e.seasonsForSeries(group) {
			if season.ID == metadata.ID {
				season.Episodes, season.Series.Episodes = nil, nil
				season.EpisodeCount = summary.EpisodeCount
				return e.seasonPayload(ctx, season, userID), nil
			}
		}
		return nil, nil
	}
	group.Episodes = nil
	group.CreatedAt = summary.CreatedAt
	group.Summary = &embySeriesSummary{EpisodeCount: summary.EpisodeCount, SeasonCount: summary.SeasonCount}
	return e.seriesPayload(ctx, group, userID), nil
}
