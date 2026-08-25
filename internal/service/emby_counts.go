package service

import (
	"context"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (e *EmbyService) ItemCounts(ctx context.Context, userID string) (map[string]any, error) {
	base := func() *gorm.DB {
		q := e.repo.DB.WithContext(ctx).Model(&model.Media{})
		return e.applyUserMediaVisibility(ctx, q, userID)
	}

	var itemCount int64
	if err := base().Distinct("media.metadata_id").Count(&itemCount).Error; err != nil {
		return nil, err
	}

	var movieCount int64
	if err := e.filterMovieItems(ctx, base()).Distinct("media.metadata_id").Count(&movieCount).Error; err != nil {
		return nil, err
	}

	var episodeCount int64
	if err := e.filterEpisodeItems(ctx, base()).Distinct("media.metadata_id").Count(&episodeCount).Error; err != nil {
		return nil, err
	}

	seriesCount, err := e.countVisibleSeries(ctx, userID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"MovieCount":   movieCount,
		"SeriesCount":  seriesCount,
		"EpisodeCount": episodeCount,
		"ItemCount":    itemCount,
	}, nil
}

func (e *EmbyService) countVisibleSeries(ctx context.Context, userID string) (int, error) {
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("media.season_num > 0 OR media.episode_num > 0")
	q = e.applyUserMediaVisibility(ctx, q, userID)
	q = seriesScopeQuery(q)
	var count int64
	if err := q.Distinct("scope_series.id").Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}
