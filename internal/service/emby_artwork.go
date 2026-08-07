package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// ImageURL returns artwork for a media/series/season item id.
func (e *EmbyService) ImageURL(ctx context.Context, id, imageType string) (string, error) {
	pick := func(primary, backdrop string) string {
		switch strings.ToLower(imageType) {
		case "backdrop", "art":
			if backdrop != "" {
				return backdrop
			}
		}
		if primary != "" {
			return primary
		}
		return backdrop
	}
	if raw, ok := e.cachedArtworkURL(id, imageType); ok {
		return raw, nil
	}
	if e.repo != nil && e.repo.Person != nil {
		if person, personErr := e.repo.Person.FindByID(ctx, id); personErr != nil && !isMissingPeopleTable(personErr) {
			return "", personErr
		} else if person != nil {
			if strings.EqualFold(imageType, "primary") || strings.TrimSpace(imageType) == "" {
				return person.ProfileURL, nil
			}
			return "", nil
		}
	}
	m, err := e.mediaViewForItemID(ctx, id, "")
	if err == nil && m != nil {
		if e.mediaShouldBeEpisode(ctx, &m.Media) {
			switch strings.ToLower(imageType) {
			case "backdrop", "art":
				return "", nil
			}
		}
		return pick(e.mediaPrimaryArtwork(ctx, m), e.mediaBackdropArtwork(ctx, m)), nil
	}
	if err != nil {
		return "", err
	}
	if season, ok, err := e.findSeasonGroup(ctx, id, ""); err != nil {
		return "", err
	} else if ok {
		return pick(season.Series.PosterURL, season.Series.BackdropURL), nil
	}
	if series, ok, err := e.findSeriesGroup(ctx, id, ""); err != nil {
		return "", err
	} else if ok {
		return pick(series.PosterURL, series.BackdropURL), nil
	}
	return "", nil
}

func (e *EmbyService) mediaPrimaryArtwork(ctx context.Context, m *model.MediaView) string {
	if m == nil {
		return ""
	}
	if e.mediaShouldBeEpisode(ctx, &m.Media) && strings.TrimSpace(m.BackdropURL) != "" {
		return m.BackdropURL
	}
	return m.PosterURL
}

func (e *EmbyService) mediaBackdropArtwork(ctx context.Context, m *model.MediaView) string {
	if m == nil {
		return ""
	}
	if e.mediaShouldBeEpisode(ctx, &m.Media) {
		return ""
	}
	return m.BackdropURL
}
