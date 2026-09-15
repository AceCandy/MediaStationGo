package service

import (
	"fmt"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func mediaReleaseSortTime(media model.Media) time.Time {
	if value := normalizeReleaseDate(media.ReleaseDate); value != "" {
		if t, err := time.Parse("2006-01-02", value); err == nil {
			return t
		}
	}
	if media.Year > 0 {
		if t, err := time.Parse("2006-01-02", fmt.Sprintf("%04d-12-31", media.Year)); err == nil {
			return t
		}
	}
	if !media.UpdatedAt.IsZero() {
		return media.UpdatedAt
	}
	return media.CreatedAt
}

func mediaViewReleaseSortTime(media model.MediaView) time.Time {
	media.Media.ReleaseDate = media.ReleaseDate
	media.Media.Year = media.Year
	return mediaReleaseSortTime(media.Media)
}

func mediaReleaseOrderSQL(desc bool) string {
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	return fmt.Sprintf("COALESCE(emby_metadata.release_date, '') %s, COALESCE(emby_metadata.year, 0) %s, media.created_at %s, media.id %s", dir, dir, dir, dir)
}

func embyPremiereDate(value string) (string, bool) {
	value = normalizeReleaseDate(value)
	if value == "" {
		return "", false
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", false
	}
	return formatEmbyDateTime(t), true
}
