package repository

import (
	"context"
	"strings"
	"unicode"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Search runs a LIKE search against the title field. Empty query returns the
// most recently added items.
func (r *MediaRepository) Search(ctx context.Context, query string, limit int) ([]model.Media, error) {
	return r.SearchFiltered(ctx, query, limit, MediaQueryFilter{IncludeNSFW: true})
}

func (r *MediaRepository) SearchFiltered(ctx context.Context, query string, limit int, filter MediaQueryFilter) ([]model.Media, error) {
	items, _, err := r.SearchFilteredPage(ctx, query, 0, limit, filter)
	return items, err
}

func (r *MediaRepository) SearchFilteredPage(ctx context.Context, query string, offset, limit int, filter MediaQueryFilter) ([]model.Media, int64, error) {
	views, total, err := r.viewRepository().SearchFilteredPage(ctx, query, offset, limit, filter)
	if err != nil {
		return nil, 0, err
	}
	return mediaViewsToMedia(views), total, nil
}

func mediaViewsToMedia(views []model.MediaView) []model.Media {
	rows := make([]model.Media, 0, len(views))
	for _, view := range views {
		row := view.Media
		row.SeriesID = view.SeriesID
		row.Title = view.Title
		row.OriginalName = view.OriginalName
		row.EpisodeTitle = view.EpisodeTitle
		row.PosterURL = view.PosterURL
		row.BackdropURL = view.BackdropURL
		row.Overview = view.Overview
		row.Rating = view.Rating
		row.Year = view.Year
		row.ReleaseDate = view.ReleaseDate
		row.SeasonNum = view.SeasonNum
		row.EpisodeNum = view.EpisodeNum
		row.TMDbID = view.TMDbID
		row.BangumiID = view.BangumiID
		row.DoubanID = view.DoubanID
		row.TheTVDBID = view.TheTVDBID
		row.Languages = view.Languages
		row.Countries = view.Countries
		row.Genres = view.Genres
		row.NSFW = view.NSFW
		rows = append(rows, row)
	}
	return rows
}

func mediaSearchTerms(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		lower := strings.ToLower(field)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, field)
	}
	return out
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func (r *MediaRepository) BackfillSearchIndex(ctx context.Context, batchLimit int) (int64, error) {
	return r.viewRepository().BackfillSearchIndex(ctx, batchLimit)
}

func (r *MediaViewRepository) BackfillSearchIndex(ctx context.Context, batchLimit int) (int64, error) {
	if backend, ok := r.searchBackend.(MediaSearchSyncBackend); ok {
		return r.backfillExternalSearchIndex(ctx, backend, batchLimit)
	}
	return 0, nil
}

func (r *MediaViewRepository) backfillExternalSearchIndex(ctx context.Context, backend MediaSearchSyncBackend, batchLimit int) (int64, error) {
	if batchLimit <= 0 {
		batchLimit = 1000
	}
	if err := backend.EnsureIndex(ctx); err != nil {
		return 0, err
	}
	var lastID string
	for {
		var ids []string
		q := r.db.WithContext(ctx).
			Model(&model.Media{}).
			Select("id").
			Where("deleted_at IS NULL")
		if lastID != "" {
			q = q.Where("id > ?", lastID)
		}
		if err := q.Order("id ASC").Limit(batchLimit).Find(&ids).Error; err != nil {
			return 0, err
		}
		if len(ids) == 0 {
			return 0, nil
		}
		rows, err := r.FindByIDs(ctx, ids, MediaQueryFilter{IncludeNSFW: true})
		if err != nil {
			return 0, err
		}
		if err := backend.IndexMedia(ctx, rows); err != nil {
			return 0, err
		}
		lastID = ids[len(ids)-1]
		if len(ids) < batchLimit {
			return 0, nil
		}
	}
}

func (r *MediaViewRepository) indexMediaIDsBestEffort(ctx context.Context, ids []string) {
	backend, ok := r.searchBackend.(MediaSearchSyncBackend)
	if !ok || len(ids) == 0 {
		return
	}
	rows, err := r.FindByIDs(ctx, ids, MediaQueryFilter{IncludeNSFW: true})
	if err == nil && len(rows) > 0 {
		_ = backend.IndexMedia(ctx, rows)
	}
}
