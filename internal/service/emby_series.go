package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type embySeriesGroup struct {
	ID          string
	LibraryID   string
	Name        string
	PosterURL   string
	BackdropURL string
	Overview    string
	Rating      float32
	Year        int
	ReleaseDate string
	TMDbID      int
	BangumiID   int
	CreatedAt   time.Time
	Episodes    []model.MediaView
	// Summary 保存列表及详情的可见分集/季计数；播放查询持有完整 Episodes。
	Summary *embySeriesSummary
}

type embySeriesSummary struct {
	EpisodeCount int
	SeasonCount  int
}

type embySeasonGroup struct {
	ID        string
	SeriesID  string
	LibraryID string
	Name      string
	SeasonNum int
	Series    embySeriesGroup
	Episodes  []model.MediaView
	// EpisodeCount 用于不加载文件详情的季列表摘要。
	EpisodeCount int
}

func (e *EmbyService) findSeriesGroup(ctx context.Context, id, userID string) (embySeriesGroup, bool, error) {
	if strings.TrimSpace(id) == "" {
		return embySeriesGroup{}, false, nil
	}
	var rows []model.Media
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("media.season_num > 0 OR media.episode_num > 0")
	q = e.applyUserMediaVisibility(ctx, q, userID)
	q = seriesScopeQuery(q).Where("scope_series.id = ?", id)
	if err := q.Order("scope_season.season_num asc, emby_metadata.episode_num asc, media.created_at desc").Find(&rows).Error; err != nil {
		return embySeriesGroup{}, false, err
	}
	displayRows, err := e.mediaViewsForRows(ctx, rows, userID)
	if err != nil {
		return embySeriesGroup{}, false, err
	}
	for _, group := range e.seriesGroupsFromMedia(preferredMetadataViewsInOrder(displayRows)) {
		if group.ID == id {
			e.rememberSeriesGroup(group)
			return group, true, nil
		}
	}
	return embySeriesGroup{}, false, nil
}

func (e *EmbyService) findSeasonGroup(ctx context.Context, id, userID string) (embySeasonGroup, bool, error) {
	if strings.TrimSpace(id) == "" {
		return embySeasonGroup{}, false, nil
	}
	var rows []model.Media
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("media.season_num > 0 OR media.episode_num > 0")
	q = e.applyUserMediaVisibility(ctx, q, userID)
	q = q.Where("emby_metadata.parent_id = ?", id)
	if err := q.
		Order("media.season_num asc, media.episode_num asc, media.created_at asc").
		Find(&rows).Error; err != nil {
		return embySeasonGroup{}, false, err
	}
	displayRows, err := e.mediaViewsForRows(ctx, rows, userID)
	if err != nil {
		return embySeasonGroup{}, false, err
	}
	for _, series := range e.seriesGroupsFromMedia(preferredMetadataViewsInOrder(displayRows)) {
		for _, season := range e.seasonsForSeries(series) {
			if season.ID == id {
				e.rememberSeriesGroup(series)
				return season, true, nil
			}
		}
	}
	return embySeasonGroup{}, false, nil
}

func (e *EmbyService) seriesGroupsFromMedia(rows []model.MediaView) []embySeriesGroup {
	byID := map[string]*embySeriesGroup{}
	order := []string{}
	for _, row := range rows {
		row := row
		seriesID := e.seriesIDForMedia(&row)
		if seriesID == "" {
			continue
		}
		group, ok := byID[seriesID]
		if !ok {
			group = &embySeriesGroup{
				ID:          seriesID,
				LibraryID:   row.LibraryID,
				Name:        e.seriesNameForMedia(&row),
				Year:        row.Year,
				ReleaseDate: row.ReleaseDate,
				TMDbID:      row.TMDbID,
				BangumiID:   row.BangumiID,
				CreatedAt:   row.CreatedAt,
			}
			byID[seriesID] = group
			order = append(order, seriesID)
		}
		if row.CreatedAt.After(group.CreatedAt) {
			group.CreatedAt = row.CreatedAt
		}
		if strings.TrimSpace(row.ReleaseDate) != "" && mediaViewReleaseSortTime(row).After(embySeriesReleaseSortTime(*group)) {
			group.ReleaseDate = row.ReleaseDate
			if row.Year > 0 {
				group.Year = row.Year
			}
		} else if group.ReleaseDate == "" && group.Year == 0 && row.Year > 0 {
			group.Year = row.Year
		}
		if group.PosterURL == "" && row.PosterURL != "" {
			group.PosterURL = row.PosterURL
		}
		if group.BackdropURL == "" && row.BackdropURL != "" {
			group.BackdropURL = row.BackdropURL
		}
		if group.Overview == "" && row.Overview != "" {
			group.Overview = row.Overview
		}
		if group.Rating == 0 && row.Rating > 0 {
			group.Rating = row.Rating
		}
		if group.Year == 0 && row.Year > 0 {
			group.Year = row.Year
		}
		group.Episodes = append(group.Episodes, row)
	}
	groups := make([]embySeriesGroup, 0, len(order))
	for _, id := range order {
		group := *byID[id]
		sort.SliceStable(group.Episodes, func(i, j int) bool {
			if group.Episodes[i].SeasonNum != group.Episodes[j].SeasonNum {
				return group.Episodes[i].SeasonNum < group.Episodes[j].SeasonNum
			}
			if group.Episodes[i].EpisodeNum != group.Episodes[j].EpisodeNum {
				return group.Episodes[i].EpisodeNum < group.Episodes[j].EpisodeNum
			}
			return group.Episodes[i].CreatedAt.Before(group.Episodes[j].CreatedAt)
		})
		groups = append(groups, group)
	}
	return groups
}

func (e *EmbyService) seasonsForSeries(series embySeriesGroup) []embySeasonGroup {
	byID := map[string]*embySeasonGroup{}
	for _, episode := range series.Episodes {
		seasonID := strings.TrimSpace(episode.SeasonID)
		if seasonID == "" {
			continue
		}
		seasonNum := episode.SeasonNum
		if seasonNum < 0 {
			seasonNum = 1
		}
		season, ok := byID[seasonID]
		if !ok {
			season = &embySeasonGroup{
				ID:        seasonID,
				SeriesID:  series.ID,
				LibraryID: series.LibraryID,
				Name:      seasonName(seasonNum),
				SeasonNum: seasonNum,
				Series:    series,
			}
			byID[seasonID] = season
		}
		season.Episodes = append(season.Episodes, episode)
	}
	out := make([]embySeasonGroup, 0, len(byID))
	for _, season := range byID {
		out = append(out, *season)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SeasonNum < out[j].SeasonNum })
	return out
}
