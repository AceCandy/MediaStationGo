package repository

import (
	"context"

	"gorm.io/gorm"
)

// PlaybackStats 只读取红果播放事实；资料或文件删除不删除统计事件。
func (r *HongGuoRepository) PlaybackStats(ctx context.Context, f PlaybackStatsFilter) (*PlaybackStatsResult, error) {
	return queryPlaybackStats(r.db.WithContext(ctx), f, r.playbackStatsQueries(ctx, f))
}

func (r *HongGuoRepository) playbackStatsQueries(ctx context.Context, f PlaybackStatsFilter) playbackStatsQueries {
	const display = `COALESCE(NULLIF(w.title, ''), '媒体已不可用') AS title,
		CASE WHEN w.kind = 'series' THEN COALESCE(g.title, w.title, '') ELSE '' END AS series_title,
		CASE WHEN w.kind = 'series' THEN CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END ELSE 0 END AS season_num,
		CASE WHEN a.id IS NULL THEN '' ELSE '/api/catalogs/hongguo/artwork/' || a.id END AS poster_url`
	details := hongGuoStatsDisplay(r.playbackStatsQuery(ctx, f)).
		Joins("LEFT JOIN users u ON u.id = pe.user_id AND u.deleted_at IS NULL").
		Joins("LEFT JOIN libraries l ON l.id = pe.library_id AND l.deleted_at IS NULL").
		Joins("LEFT JOIN media m ON m.id = pe.media_id").
		Joins("LEFT JOIN hongguo_media_bindings b ON b.media_id = m.id AND b.work_id = w.id").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id").
		Select(`'hongguo' AS system, pe.source_id, pe.id, pe.played_at, pe.user_id,
			COALESCE(NULLIF(u.nickname, ''), u.username, '已删除账户') AS user_name,
			pe.library_id, COALESCE(l.name, '已删除媒体库') AS library_name, pe.media_id, '' AS metadata_id,
			COALESCE(NULLIF(w.title, ''), '媒体已不可用') AS title,
			CASE WHEN w.kind = 'series' THEN COALESCE(g.title, w.title, '') ELSE '' END AS series_title,
			CASE WHEN w.kind = 'series' THEN CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END ELSE 0 END AS season_num,
			CASE WHEN w.kind = 'series' THEN pe.episode_number ELSE 0 END AS episode_num,
			CASE WHEN a.id IS NULL THEN '' ELSE '/api/catalogs/hongguo/artwork/' || a.id END AS poster_url,
			(b.media_id IS NOT NULL AND (w.kind = 'movie' OR ep.number = pe.episode_number)) AS media_available`)
	rank := hongGuoStatsDisplay(r.playbackStatsQuery(ctx, f)).
		Where("pe.played_at >= ? AND pe.played_at < ?", f.RankFrom, f.RankTo).
		Select("'hongguo' AS system, 'hongguo-' || pe.source_id AS group_id, " + display)
	return playbackStatsQueries{
		events:  r.playbackStatsQuery(ctx, f).Select("pe.played_at"),
		details: details, ranking: rank,
	}
}

func (r *HongGuoRepository) playbackStatsQuery(ctx context.Context, f PlaybackStatsFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("hongguo_playback_events pe").
		Joins("LEFT JOIN hongguo_works w ON w.source_id = pe.source_id").
		Where("pe.played_at >= ? AND pe.played_at < ?", f.From, f.To)
	if f.UserID != "" {
		q = q.Where("pe.user_id = ?", f.UserID)
	}
	if len(f.LibraryIDs) > 0 {
		q = q.Where("pe.library_id = ANY(?)", &f.LibraryIDs)
	}
	if f.MediaType == "movie" {
		q = q.Where("w.kind = 'movie'")
	} else if f.MediaType == "tv" {
		q = q.Where("w.kind = 'series'")
	}
	return q
}

func hongGuoStatsDisplay(q *gorm.DB) *gorm.DB {
	return q.Joins(HongGuoAlbumJoin).
		Joins("LEFT JOIN hongguo_artworks a ON a.work_id = w.id")
}
