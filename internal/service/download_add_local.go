package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func (d *DownloadService) localMediaAlreadyExists(ctx context.Context, title string) bool {
	rows, ok := d.localMediaAvailabilityRows(ctx, title)
	if !ok {
		return false
	}
	return localMediaRowsMatchDownloadTitle(title, rows)
}

func (d *DownloadService) localMediaAvailabilityRows(ctx context.Context, title string) ([]model.Media, bool) {
	if d == nil || d.repo == nil || d.repo.DB == nil {
		return nil, false
	}
	if !d.repo.DB.Migrator().HasTable(&model.Media{}) {
		return nil, false
	}
	queries := localAvailabilityTitleCandidates(title)
	if len(queries) == 0 {
		return nil, false
	}
	rows := make([]model.Media, 0)
	seen := make(map[string]struct{})
	for _, query := range queries {
		matches, err := d.repo.Media.SearchFiltered(ctx, query, 200, repository.MediaQueryFilter{IncludeNSFW: true})
		if err != nil {
			return nil, false
		}
		for _, row := range matches {
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

func localMediaRowsMatchDownloadTitle(title string, rows []model.Media) bool {
	wanted := episodeRefsFromTitle(title)
	if len(wanted) == 0 {
		return true
	}
	existing := map[string]struct{}{}
	hasSeriesPack := false
	for _, row := range rows {
		rowSeason, rowEpisode := localMediaRowSeasonEpisode(row)
		if rowEpisode > 0 {
			existing[episodeKey(rowSeason, rowEpisode)] = struct{}{}
			continue
		}
		if rowEpisode <= 0 && isSeriesPackTitle(row.Title+" "+row.OriginalName+" "+row.Path) {
			hasSeriesPack = true
		}
	}
	if hasSeriesPack {
		return len(wanted) == 0
	}
	for _, ref := range wanted {
		if _, ok := existing[episodeKey(ref.Season, ref.Episode)]; !ok {
			return false
		}
	}
	return true
}

func localMediaRowSeasonEpisode(row model.Media) (int, int) {
	rowSeason := row.SeasonNum
	rowEpisode := row.EpisodeNum
	if rowSeason <= 0 || rowEpisode <= 0 {
		parsedSeason, parsedEpisode := ParseEpisode(row.Path)
		if rowSeason <= 0 {
			rowSeason = parsedSeason
		}
		if rowEpisode <= 0 {
			rowEpisode = parsedEpisode
		}
	}
	if rowSeason <= 0 {
		rowSeason = 1
	}
	return rowSeason, rowEpisode
}
