package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
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
		next.RuntimeSec = match.RuntimeSec
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
			if err := s.refreshSeasonEpisodesFromDetails(ctx, season, details); err != nil {
				return err
			}
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
	mergeTMDbMetadata(item, next)
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

// refreshSeasonEpisodesFromDetails 将季接口内的集摘要同步到已有本地单集。
func (s *ScraperService) refreshSeasonEpisodesFromDetails(ctx context.Context, season *model.MetadataItem, details *TMDbSeasonDetails) error {
	if season == nil || details == nil || season.ParentID == nil {
		return nil
	}
	now := time.Now().UTC()
	for _, summary := range details.Episodes {
		if summary.EpisodeNumber <= 0 || summary.ID <= 0 {
			continue
		}
		episode, err := s.repo.Metadata.FindEpisode(ctx, *season.ParentID, season.SeasonNum, summary.EpisodeNumber)
		if err != nil {
			return err
		}
		if episode == nil {
			continue
		}
		mergeTMDbMetadata(episode, &model.MetadataItem{
			Title: summary.Name, Overview: summary.Overview, Rating: summary.Rating,
			RuntimeSec: summary.Runtime * 60, ReleaseDate: summary.AirDate,
		})
		episode.Source = "tmdb"
		if err := s.repo.Metadata.Update(ctx, episode); err != nil {
			return err
		}
		epPayload, err := json.Marshal(map[string]any{
			"id": summary.ID, "episode_number": summary.EpisodeNumber, "name": summary.Name,
			"overview": summary.Overview, "air_date": summary.AirDate,
			"vote_average": summary.Rating, "runtime": summary.Runtime, "still_path": summary.StillPath,
		})
		if err != nil {
			return err
		}
		if source := tmdbOriginalImageURL(s.tmdb.imgCDN, summary.StillPath); source != "" {
			if s.artwork == nil || s.artwork.imageProxy == nil {
				return errors.New("artwork store unavailable")
			}
			if err := s.artwork.imageProxy.RemoveCached(source); err != nil {
				return err
			}
			if _, err := s.artwork.ImportRemote(ctx, episode.ID, model.ArtworkTypeStill, "tmdb", source); err != nil {
				return err
			}
		}
		if err := s.repo.Metadata.ReplaceIdentifierWithSnapshot(ctx, episode.ID, "tmdb", model.MetadataKindEpisode, strconv.Itoa(summary.ID), epPayload, now); err != nil {
			return err
		}
	}
	return nil
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
		value, err := strconv.Atoi(strings.TrimSpace(identifier.ExternalID))
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

// RefreshMetadataTMDbByIdentity 将发现页的 TMDb 身份解析为现有 metadata，再复用媒体详情刷新逻辑。
// 三小时内已有成功快照则复用；不存在本地 metadata 时不创建目录记录。
func (s *ScraperService) RefreshMetadataTMDbByIdentity(ctx context.Context, id repository.DiscoverIdentity) error {
	if s == nil || s.repo == nil || !id.Valid() {
		return ErrTMDbRefreshIdentity
	}
	item, err := s.repo.Metadata.FindByIdentifier(ctx, "tmdb", id.Kind(), strconv.Itoa(id.TMDbID))
	if err != nil {
		return err
	}
	if item == nil {
		return ErrMediaNotFound
	}
	snapshot, err := s.repo.Metadata.FindProviderSnapshot(ctx, item.ID, "tmdb")
	if err != nil {
		return err
	}
	if snapshot != nil && !snapshot.Degraded && time.Since(snapshot.FetchedAt) < 3*time.Hour {
		return nil
	}
	return s.RefreshMetadataTMDb(ctx, item.ID)
}

// mergeTMDbMetadata 只接受来源返回的有效字段，避免一次缺项刷新清空本地资料。
func mergeTMDbMetadata(item, next *model.MetadataItem) {
	if item == nil || next == nil {
		return
	}
	if value := strings.TrimSpace(next.Title); value != "" {
		item.Title = value
	}
	if value := strings.TrimSpace(next.OriginalName); value != "" {
		item.OriginalName = value
	}
	if value := strings.TrimSpace(next.Overview); value != "" {
		item.Overview = value
	}
	if next.Rating > 0 {
		item.Rating = next.Rating
	}
	if next.Year > 0 {
		item.Year = next.Year
	}
	if value := strings.TrimSpace(next.ReleaseDate); value != "" {
		item.ReleaseDate = value
	}
	if next.RuntimeSec > 0 {
		item.RuntimeSec = next.RuntimeSec
	}
	if value := strings.TrimSpace(next.Languages); value != "" {
		item.Languages = value
	}
	if value := strings.TrimSpace(next.Countries); value != "" {
		item.Countries = value
	}
	if value := strings.TrimSpace(next.Genres); value != "" {
		item.Genres = value
	}
}
