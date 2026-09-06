package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var ErrTMDbRefreshIdentity = errors.New("metadata has no unique TMDB identity")

// RefreshMetadataTMDb 按当前实体身份重新读取 TMDB，不重新匹配或修改媒体文件关联。
func (s *ScraperService) RefreshMetadataTMDb(ctx context.Context, metadataID string) error {
	if s == nil || s.tmdb == nil || s.tmdb.resolveAPIKey(ctx) == "" {
		return errors.New("TMDB provider unavailable")
	}
	item, err := s.repo.Metadata.FindByID(ctx, metadataID)
	if err != nil {
		return err
	}
	if item == nil {
		return ErrMediaNotFound
	}
	tmdbID, err := s.metadataTMDbRefreshID(ctx, item)
	if err != nil {
		return err
	}
	var payload []byte
	var credits []PersonCredit
	var loaded []string
	var next *model.MetadataItem
	artwork := map[string]string{}
	switch item.Kind {
	case model.MetadataKindMovie, model.MetadataKindSeries:
		var match *Match
		if item.Kind == model.MetadataKindMovie {
			match, err = s.tmdb.GetMovieMatch(ctx, tmdbID)
		} else {
			match, err = s.tmdb.GetTVMatch(ctx, tmdbID)
		}
		if err != nil {
			return err
		}
		if match == nil || match.TMDbID != tmdbID {
			return ErrTMDbRefreshIdentity
		}
		next = metadataItemFromMatch(match, item.Kind, "tmdb")
		next.RuntimeSec = item.RuntimeSec
		payload, credits, loaded = match.RawJSON, match.Credits, match.LoadedCreditTypes
		artwork[model.ArtworkTypePoster] = match.CatalogPosterURL
		artwork[model.ArtworkTypeBackdrop] = match.CatalogBackdropURL
	case model.MetadataKindSeason, model.MetadataKindEpisode:
		// 坐标仅从 metadata 父链读取，不使用 media 的扫描提示。
		season := item
		if item.Kind == model.MetadataKindEpisode && item.ParentID != nil {
			season, err = s.repo.Metadata.FindByID(ctx, *item.ParentID)
			if err != nil {
				return err
			}
		}
		if season == nil || season.Kind != model.MetadataKindSeason || season.ParentID == nil {
			return ErrTMDbRefreshIdentity
		}
		series, err := s.repo.Metadata.FindByID(ctx, *season.ParentID)
		if err != nil {
			return err
		}
		if series == nil || series.Kind != model.MetadataKindSeries {
			return ErrTMDbRefreshIdentity
		}
		seriesID, err := s.metadataTMDbRefreshID(ctx, series)
		if err != nil {
			return err
		}
		if item.Kind == model.MetadataKindSeason {
			details, err := s.tmdb.GetTVSeasonDetails(ctx, seriesID, season.SeasonNum)
			if err != nil {
				return err
			}
			if details == nil || details.ID != tmdbID {
				return ErrTMDbRefreshIdentity
			}
			next = catalogSeasonItem(series.ID, details)
			payload, credits, loaded = details.RawJSON, details.Credits, details.LoadedCreditTypes
			artwork[model.ArtworkTypePoster] = details.PosterURL
		} else {
			details, err := s.tmdb.GetTVEpisodeDetails(ctx, seriesID, season.SeasonNum, item.EpisodeNum)
			if err != nil {
				return err
			}
			if details == nil || details.ID != tmdbID {
				return ErrTMDbRefreshIdentity
			}
			next = &model.MetadataItem{Title: details.Name, Overview: details.Overview, Rating: details.Rating,
				RuntimeSec: details.Runtime * 60, ReleaseDate: details.AirDate, Year: details.AirYear}
			payload, credits, loaded = details.RawJSON, details.Credits, details.LoadedCreditTypes
			artwork[model.ArtworkTypeStill] = details.CatalogStillURL
		}
	default:
		return ErrTMDbRefreshIdentity
	}
	if !json.Valid(payload) {
		return errors.New("invalid TMDB detail snapshot")
	}
	// 仅替换详情字段，保留实体身份、父链、其他来源标识及目录检查点。
	item.Title, item.Overview, item.Rating = next.Title, next.Overview, next.Rating
	item.Year, item.ReleaseDate, item.RuntimeSec = next.Year, next.ReleaseDate, next.RuntimeSec
	if item.Kind == model.MetadataKindMovie || item.Kind == model.MetadataKindSeries {
		item.OriginalName = next.OriginalName
		item.Languages, item.Countries, item.Genres = next.Languages, next.Countries, next.Genres
	}
	item.Source = "tmdb"
	if err := s.repo.Metadata.Update(ctx, item); err != nil {
		return err
	}
	defer s.invalidateMediaCache(ctx)
	if err := s.persistCredits(ctx, item.ID, loaded, credits, true); err != nil {
		return err
	}
	for kind, source := range artwork {
		if source == "" {
			continue
		}
		if s.artwork == nil || s.artwork.imageProxy == nil {
			return errors.New("artwork store unavailable")
		}
		if err := s.artwork.imageProxy.RemoveCached(source); err != nil {
			return err
		}
		if _, err := s.artwork.ImportRemote(ctx, item.ID, kind, "tmdb", source); err != nil {
			return err
		}
	}
	return s.repo.Metadata.UpsertProviderSnapshot(ctx, item.ID, "tmdb", payload, time.Now().UTC())
}

func (s *ScraperService) metadataTMDbRefreshID(ctx context.Context, item *model.MetadataItem) (int, error) {
	identifiers, err := s.repo.Metadata.ListIdentifiers(ctx, item.ID)
	if err != nil {
		return 0, err
	}
	id := 0
	for _, identifier := range identifiers {
		if identifier.Provider != "tmdb" || identifier.EntityKind != item.Kind {
			continue
		}
		value, err := strconv.Atoi(identifier.ExternalID)
		if err != nil || value <= 0 || id != 0 {
			return 0, ErrTMDbRefreshIdentity
		}
		id = value
	}
	if id == 0 {
		return 0, ErrTMDbRefreshIdentity
	}
	return id, nil
}
