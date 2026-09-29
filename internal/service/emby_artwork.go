package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// ImageURL returns artwork for a library/media/series/season item id.
func (e *EmbyService) ImageURL(ctx context.Context, id, imageType string) (string, error) {
	if strings.HasPrefix(id, "nfo-") {
		view, err := e.repo.MediaView.NFOPresentation(ctx, id)
		if err != nil || view == nil {
			return "", err
		}
		if view.MetadataKind == model.MetadataKindMovie || view.MetadataKind == model.MetadataKindEpisode {
			files, err := e.repo.MediaView.NFOItemViews(ctx, id, e.mediaQueryFilter(ctx, ""))
			if err != nil || len(files) == 0 {
				return "", err
			}
			view = &files[0]
		}
		if strings.EqualFold(imageType, "backdrop") || strings.EqualFold(imageType, "art") {
			return view.BackdropURL, nil
		}
		return mediaPrimaryArtworkForType(view, view.MetadataKind == model.MetadataKindEpisode), nil
	}
	if strings.HasPrefix(id, "hg-person-") {
		if !strings.EqualFold(imageType, "Primary") {
			return "", nil
		}
		var rows []model.HongGuoArtwork
		if err := e.repo.DB.WithContext(ctx).Where("person_id = ? AND local_key <> ''", strings.TrimPrefix(id, "hg-person-")).Limit(1).Find(&rows).Error; err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "", nil
		}
		return "/api/catalogs/hongguo/artwork/" + rows[0].ID, nil
	}
	if strings.HasPrefix(id, "hg-") {
		if !strings.EqualFold(imageType, "Primary") {
			return "", nil
		}
		artwork := e.repo.DB.WithContext(ctx).Table("hongguo_works AS w").
			Joins("JOIN hongguo_artworks a ON a.work_id = w.id AND a.local_key <> ''")
		switch {
		case strings.HasPrefix(id, "hg-group-"):
			artwork = artwork.Where("w.related_album_id = ? AND w.related_album_id <> '' AND w.kind = 'series' AND w.season_index > 0", strings.TrimPrefix(id, "hg-group-"))
		case strings.HasPrefix(id, "hg-work-"):
			artwork = artwork.Where("w.id = ? AND NOT (w.kind = 'series' AND w.related_album_id <> '' AND w.season_index > 0)", strings.TrimPrefix(id, "hg-work-"))
		case strings.HasPrefix(id, "hg-season-"):
			artwork = artwork.Where("w.id = ? AND w.kind = 'series'", strings.TrimPrefix(id, "hg-season-"))
		default:
			return "", nil
		}
		// 图片只探测目标作品的可见文件；不展开季集或计算播放状态。
		files := e.hongGuoVisibleFiles(ctx, "", "").Select("1").
			Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").Where("b.work_id = w.id")
		var artworkID string
		if err := artwork.Where("EXISTS (? OFFSET 0)", files).Select("a.id").
			Order("NULLIF(w.season_index,0) NULLS LAST,w.id").Limit(1).Scan(&artworkID).Error; err != nil {
			return "", err
		}
		if artworkID == "" {
			return "", nil
		}
		return "/api/catalogs/hongguo/artwork/" + artworkID, nil
	}
	if e.repo != nil && e.repo.Person != nil {
		person, personErr := e.repo.Person.FindByID(ctx, id)
		if personErr != nil && !isMissingPeopleTable(personErr) {
			return "", personErr
		}
		if personErr == nil && person != nil {
			return "", nil
		}
	}
	if e.repo != nil && e.repo.Library != nil {
		lib, err := FindLibraryBasic(ctx, e.repo, e.cache, id)
		if err != nil {
			return "", err
		}
		if lib != nil {
			if strings.EqualFold(imageType, "Primary") {
				return strings.TrimSpace(lib.CoverURL), nil
			}
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
			parentFallback := true
			switch strings.ToLower(imageType) {
			case "backdrop", "art":
				parentFallback = false
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
			if raw := e.metadataArtworkURL(ctx, metadata.ID, artworkType); raw != "" {
				return raw, nil
			}
			if metadata.Kind == model.MetadataKindEpisode && parentFallback {
				if raw := e.metadataArtworkURL(ctx, metadata.ID, model.ArtworkTypeBackdrop); raw != "" {
					return raw, nil
				}
			}
			if parentFallback {
				return e.parentArtworkFallback(ctx, metadata), nil
			}
			return "", nil
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

func (e *EmbyService) parentArtworkFallback(ctx context.Context, metadata *model.MetadataItem) string {
	if metadata == nil || metadata.ParentID == nil {
		return ""
	}
	parent, err := e.repo.Metadata.FindByID(ctx, *metadata.ParentID)
	if err != nil || parent == nil {
		return ""
	}
	if metadata.Kind == model.MetadataKindSeason {
		return e.metadataArtworkURL(ctx, parent.ID, model.ArtworkTypePoster)
	}
	if metadata.Kind != model.MetadataKindEpisode || parent.ParentID == nil {
		return ""
	}
	series, err := e.repo.Metadata.FindByID(ctx, *parent.ParentID)
	if err != nil || series == nil {
		return ""
	}
	if raw := e.metadataArtworkURL(ctx, series.ID, model.ArtworkTypeBackdrop); raw != "" {
		return raw
	}
	return e.metadataArtworkURL(ctx, series.ID, model.ArtworkTypePoster)
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
	return mediaPrimaryArtworkForType(m, m != nil && e.mediaShouldBeEpisode(ctx, &m.Media))
}

func mediaPrimaryArtworkForType(m *model.MediaView, episode bool) string {
	if m == nil {
		return ""
	}
	if episode && strings.TrimSpace(m.BackdropURL) != "" {
		return m.BackdropURL
	}
	return m.PosterURL
}

func (e *EmbyService) mediaBackdropArtwork(ctx context.Context, m *model.MediaView) string {
	return mediaBackdropArtworkForType(m, m != nil && e.mediaShouldBeEpisode(ctx, &m.Media))
}

func mediaBackdropArtworkForType(m *model.MediaView, episode bool) string {
	if m == nil {
		return ""
	}
	if episode {
		return ""
	}
	return m.BackdropURL
}
