package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// 三个查询保留各来源的筛选及展示语义，统一在数据库中汇总和分页。
// UNION 各分支的列顺序必须一致：事件仅含 played_at，明细/排行按对应 DTO 投影。
type playbackStatsQueries struct {
	events  *gorm.DB
	details *gorm.DB
	ranking *gorm.DB
}

var ErrPlaybackStatsSystem = errors.New("system must be all, catalog, hongguo or nfo")

// PlaybackStats 合并查看只合并读投影，事件和排行榜身份始终按体系隔离。
func (r *Container) PlaybackStats(ctx context.Context, system string, f PlaybackStatsFilter) (*PlaybackStatsResult, error) {
	var queries playbackStatsQueries
	switch system {
	case "catalog":
		queries = r.PlaybackEvent.playbackStatsQueries(ctx, f)
	case "hongguo":
		queries = r.HongGuo.playbackStatsQueries(ctx, f)
	case "nfo":
		queries = r.NFO.playbackStatsQueries(ctx, f)
	case "all":
		catalog := r.PlaybackEvent.playbackStatsQueries(ctx, f)
		hongguo := r.HongGuo.playbackStatsQueries(ctx, f)
		nfo := r.NFO.playbackStatsQueries(ctx, f)
		queries = playbackStatsQueries{
			events:  r.DB.Raw("? UNION ALL ? UNION ALL ?", catalog.events, hongguo.events, nfo.events),
			details: r.DB.Raw("? UNION ALL ? UNION ALL ?", catalog.details, hongguo.details, nfo.details),
			ranking: r.DB.Raw("? UNION ALL ? UNION ALL ?", catalog.ranking, hongguo.ranking, nfo.ranking),
		}
	default:
		return nil, ErrPlaybackStatsSystem
	}
	return queryPlaybackStats(r.DB.WithContext(ctx), f, queries)
}

func queryPlaybackStats(db *gorm.DB, f PlaybackStatsFilter, q playbackStatsQueries) (*PlaybackStatsResult, error) {
	result := &PlaybackStatsResult{
		Buckets: []PlaybackStatsBucket{},
		Details: PlaybackStatsDetailsPage{Items: []PlaybackStatsDetail{}, Page: f.Page, PageSize: f.PageSize},
		Ranking: PlaybackStatsRanking{Grain: f.RankGrain, Period: f.RankPeriod, Items: []PlaybackStatsRankItem{}},
	}
	if err := db.Table("(?) AS events", q.events).Count(&result.Total).Error; err != nil {
		return nil, err
	}
	result.Details.Total = result.Total
	var buckets []struct {
		Period time.Time
		Count  int64
	}
	if err := db.Table("(?) AS events", q.events).
		Select("DATE_TRUNC(?, played_at AT TIME ZONE ?) AS period, COUNT(*) AS count", f.Grain, f.TimeZone).
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
	if err := db.Table("(?) AS details", q.details).
		Order("played_at DESC, id DESC, system ASC").Limit(f.PageSize).Offset((f.Page - 1) * f.PageSize).
		Scan(&result.Details.Items).Error; err != nil {
		return nil, err
	}
	if err := db.Table("(?) AS ranked", q.ranking).
		Select("system, group_id, MAX(title) AS title, MAX(series_title) AS series_title, MAX(season_num) AS season_num, MAX(poster_url) AS poster_url, COUNT(*) AS count").
		Where("group_id IS NOT NULL").Group("system, group_id").
		Order("count DESC, group_id ASC, system ASC").Limit(10).Scan(&result.Ranking.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}
