package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// PlaybackStats 只读取红果播放事实；资料或文件删除不删除统计事件。
func (r *HongGuoRepository) PlaybackStats(ctx context.Context, f PlaybackStatsFilter) (*PlaybackStatsResult, error) {
	result := &PlaybackStatsResult{
		Buckets: []PlaybackStatsBucket{},
		Details: PlaybackStatsDetailsPage{Items: []PlaybackStatsDetail{}, Page: f.Page, PageSize: f.PageSize},
		Ranking: PlaybackStatsRanking{Grain: f.RankGrain, Period: f.RankPeriod, Items: []PlaybackStatsRankItem{}},
	}
	if err := r.playbackStatsQuery(ctx, f).Count(&result.Total).Error; err != nil {
		return nil, err
	}
	result.Details.Total = result.Total
	var buckets []struct {
		Period time.Time
		Count  int64
	}
	if err := r.playbackStatsQuery(ctx, f).
		Select("DATE_TRUNC(?, pe.played_at AT TIME ZONE ?) AS period, COUNT(*) AS count", f.Grain, f.TimeZone).
		Group("period").Order("period ASC").Scan(&buckets).Error; err != nil {
		return nil, err
	}
	for _, bucket := range buckets {
		format := "2006-01-02"
		if f.Grain == "month" {
			format = "2006-01"
		}
		result.Buckets = append(result.Buckets, PlaybackStatsBucket{Period: bucket.Period.Format(format), Count: bucket.Count})
	}
	const display = `COALESCE(NULLIF(w.title, ''), '媒体已不可用') AS title,
		CASE WHEN w.kind = 'series' THEN COALESCE(g.title, w.title, '') ELSE '' END AS series_title,
		CASE WHEN w.kind = 'series' THEN CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END ELSE 0 END AS season_num,
		CASE WHEN a.id IS NULL THEN '' ELSE '/api/catalogs/hongguo/artwork/' || a.id END AS poster_url`
	if err := hongGuoStatsDisplay(r.playbackStatsQuery(ctx, f)).
		Joins("LEFT JOIN users u ON u.id = pe.user_id AND u.deleted_at IS NULL").
		Joins("LEFT JOIN libraries l ON l.id = pe.library_id AND l.deleted_at IS NULL").
		Joins("LEFT JOIN media m ON m.id = pe.media_id").
		Joins("LEFT JOIN hongguo_media_bindings b ON b.media_id = m.id AND b.work_id = w.id").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id").
		Select(`pe.id, pe.played_at, pe.user_id, pe.library_id, pe.media_id, pe.source_id,
			COALESCE(NULLIF(u.nickname, ''), u.username, '已删除账户') AS user_name,
			COALESCE(l.name, '已删除媒体库') AS library_name, '' AS metadata_id,
			CASE WHEN w.kind = 'series' THEN pe.episode_number ELSE 0 END AS episode_num,
			(b.media_id IS NOT NULL AND (w.kind = 'movie' OR ep.number = pe.episode_number)) AS media_available, ` + display).
		Order("pe.played_at DESC, pe.id DESC").Limit(f.PageSize).Offset((f.Page - 1) * f.PageSize).
		Scan(&result.Details.Items).Error; err != nil {
		return nil, err
	}
	rank := hongGuoStatsDisplay(r.playbackStatsQuery(ctx, f)).
		Where("pe.played_at >= ? AND pe.played_at < ?", f.RankFrom, f.RankTo).
		Select("'hongguo-' || pe.source_id AS group_id, " + display)
	if err := r.db.WithContext(ctx).Table("(?) AS ranked", rank).
		Select("group_id, MAX(title) AS title, MAX(series_title) AS series_title, MAX(season_num) AS season_num, MAX(poster_url) AS poster_url, COUNT(*) AS count").
		Group("group_id").Order("count DESC, group_id ASC").Limit(10).Scan(&result.Ranking.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (r *HongGuoRepository) playbackStatsQuery(ctx context.Context, f PlaybackStatsFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("hongguo_playback_events pe").
		Joins("LEFT JOIN hongguo_works w ON w.source_id = pe.source_id").
		Where("pe.played_at >= ? AND pe.played_at < ?", f.From, f.To)
	if f.UserID != "" {
		q = q.Where("pe.user_id = ?", f.UserID)
	}
	if len(f.LibraryIDs) > 0 {
		q = q.Where("pe.library_id IN ?", f.LibraryIDs)
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
