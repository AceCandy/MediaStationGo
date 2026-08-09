package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// ImageURL returns artwork for a media/series/season item id.
func (e *EmbyService) ImageURL(ctx context.Context, id, imageType string) (string, error) {
	if e.repo != nil && e.repo.Person != nil {
		person, personErr := e.repo.Person.FindByID(ctx, id)
		if personErr != nil && !isMissingPeopleTable(personErr) {
			return "", personErr
		}
		if personErr == nil && person != nil {
			return "", nil
		}
	}
	if e.repo != nil && e.repo.Metadata != nil {
		metadata, metadataErr := e.repo.Metadata.FindByID(ctx, id)
		if metadataErr != nil {
			return "", metadataErr
		}
		if metadata != nil {
			artworkType := ""
			switch strings.ToLower(imageType) {
			case "backdrop", "art":
				if metadata.Kind == model.MetadataKindMovie || metadata.Kind == model.MetadataKindSeries {
					artworkType = model.ArtworkTypeBackdrop
				}
			default:
				switch metadata.Kind {
				case model.MetadataKindEpisode:
					artworkType = model.ArtworkTypeStill
				default:
					artworkType = model.ArtworkTypePoster
				}
			}
			return e.metadataArtworkURL(ctx, metadata.ID, artworkType), nil
		}
	}
	if raw, ok := e.cachedArtworkURL(id, imageType); ok {
		return raw, nil
	}
	m, err := e.mediaViewForItemID(ctx, id, "")
	if err == nil && m != nil {
		if e.mediaShouldBeEpisode(ctx, &m.Media) {
			switch strings.ToLower(imageType) {
			case "backdrop", "art":
				return "", nil
			}
		}
		switch strings.ToLower(imageType) {
		case "backdrop", "art":
			return e.mediaBackdropArtwork(ctx, m), nil
		default:
			return e.mediaPrimaryArtwork(ctx, m), nil
		}
	}
	if err != nil {
		return "", err
	}
	return "", nil
}

func (e *EmbyService) metadataArtworkURL(ctx context.Context, metadataID, artworkType string) string {
	if e == nil || e.repo == nil || e.repo.Artwork == nil || strings.TrimSpace(artworkType) == "" {
		return ""
	}
	asset, err := e.repo.Artwork.FindSelection(ctx, metadataID, artworkType)
	if err != nil || asset == nil {
		return ""
	}
	return ArtworkURL(asset.ID)
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
