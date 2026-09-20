package repository

import (
	"context"

	"gorm.io/gorm"
)

// 本地统计从条目身份读取，文件缺失或重新绑定不能改变历史事件的归属。
func (r *NFORepository) playbackStatsQueries(ctx context.Context, f PlaybackStatsFilter) playbackStatsQueries {
	display := func() *gorm.DB {
		return r.playbackStatsQuery(ctx, f).
			Joins("LEFT JOIN nfo_items season ON season.id = item.parent_id AND season.kind = 'season'").
			Joins("LEFT JOIN nfo_items series ON series.id = season.parent_id AND series.kind = 'series'")
	}
	details := display().
		Joins("LEFT JOIN users u ON u.id = pe.user_id AND u.deleted_at IS NULL").
		Joins("LEFT JOIN libraries l ON l.id = pe.library_id AND l.deleted_at IS NULL").
		Joins("LEFT JOIN media m ON m.id = pe.media_id AND m.catalog_source = 'nfo'").
		Joins("LEFT JOIN nfo_media_bindings b ON b.media_id = m.id AND b.item_id = pe.item_id").
		Select(`'nfo' AS system, '' AS source_id, pe.id, pe.played_at, pe.user_id,
			COALESCE(NULLIF(u.nickname, ''), u.username, '已删除账户') AS user_name,
			pe.library_id, COALESCE(l.name, '已删除媒体库') AS library_name,
			pe.media_id, '' AS metadata_id, COALESCE(NULLIF(item.title, ''), '媒体已不可用') AS title,
			COALESCE(series.title, '') AS series_title, COALESCE(season.season_num, 0) AS season_num,
			COALESCE(item.episode_num, 0) AS episode_num,
			COALESCE('/api/artwork/' || COALESCE(NULLIF(item.poster_asset_id, ''), NULLIF(season.poster_asset_id, ''), NULLIF(series.poster_asset_id, '')), '') AS poster_url,
			(b.media_id IS NOT NULL) AS media_available`)
	ranking := display().Where("pe.played_at >= ? AND pe.played_at < ?", f.RankFrom, f.RankTo).
		Select(`'nfo' AS system, 'nfo-' || COALESCE(season.id, pe.item_id) AS group_id,
			COALESCE(NULLIF(series.title, ''), NULLIF(item.title, ''), '媒体已不可用') AS title,
			COALESCE(series.title, '') AS series_title, COALESCE(season.season_num, 0) AS season_num,
			COALESCE('/api/artwork/' || COALESCE(NULLIF(season.poster_asset_id, ''), NULLIF(series.poster_asset_id, ''), NULLIF(item.poster_asset_id, '')), '') AS poster_url`)
	return playbackStatsQueries{
		events:  r.playbackStatsQuery(ctx, f).Select("pe.played_at"),
		details: details, ranking: ranking,
	}
}

func (r *NFORepository) playbackStatsQuery(ctx context.Context, f PlaybackStatsFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("nfo_playback_events pe").
		Joins("LEFT JOIN nfo_items item ON item.id = pe.item_id").
		Where("pe.played_at >= ? AND pe.played_at < ?", f.From, f.To)
	if f.UserID != "" {
		q = q.Where("pe.user_id = ?", f.UserID)
	}
	if len(f.LibraryIDs) > 0 {
		q = q.Where("pe.library_id = ANY(?)", &f.LibraryIDs)
	}
	if f.MediaType == "movie" {
		q = q.Where("item.kind = 'movie'")
	} else if f.MediaType == "tv" {
		q = q.Where("item.kind = 'episode'")
	}
	return q
}
