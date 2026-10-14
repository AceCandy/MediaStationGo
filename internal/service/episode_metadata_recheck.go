package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func tmdbMetadataCandidateNeedsRecheck(item repository.TMDbMetadataRecheckCandidate) bool {
	if item.SourceState {
		return item.BasicsMissing || item.TMDbID == ""
	}
	// 标题只随其他缺失信息顺带更新，不单独触发复查。
	return item.BasicsMissing || strings.TrimSpace(item.Overview) == "" || strings.TrimSpace(item.ReleaseDate) == "" || item.ArtworkMissing ||
		(item.Kind == model.MetadataKindSeason && (item.TMDbID == "" || item.SnapshotMissing))
}

// tmdbMetadataRecheckDetails 将季、集响应投影到共用的补全保存流程。
type tmdbMetadataRecheckDetails struct {
	id      int
	updates map[string]any
	rawJSON []byte
	timing  *repository.TMDbRecheckTiming
}

// tmdbSeasonRecheckTiming 复用整季响应的日期，缺集结果也能采用当前播出安排。
func tmdbSeasonRecheckTiming(season *TMDbSeasonDetails) *repository.TMDbRecheckTiming {
	dates := make([]string, 0, len(season.Episodes))
	for _, episode := range season.Episodes {
		dates = append(dates, episode.AirDate)
	}
	raw, _ := json.Marshal(dates)
	return &repository.TMDbRecheckTiming{SeasonReleaseDate: season.AirDate, SeasonEpisodeDates: string(raw)}
}

func (s *ScraperService) fetchTMDbMetadataRecheck(ctx context.Context, candidate repository.TMDbMetadataRecheckCandidate, seriesTMDbID int) (*tmdbMetadataRecheckDetails, error) {
	if candidate.Kind == model.MetadataKindSeason {
		season, err := s.tmdb.GetTVSeasonDetails(ctx, seriesTMDbID, candidate.SeasonNum)
		if err != nil || season == nil {
			return nil, err
		}
		if season.SeasonNumber != candidate.SeasonNum {
			return nil, ErrTMDbRefreshIdentity
		}
		return &tmdbMetadataRecheckDetails{id: season.ID, updates: tmdbMetadataUpdates(catalogSeasonItem("", season)), rawJSON: season.RawJSON, timing: tmdbSeasonRecheckTiming(season)}, nil
	}
	if candidate.Kind != model.MetadataKindEpisode {
		return nil, ErrTMDbRefreshIdentity
	}
	// 后台已共享整季响应，缺集时也用本次播出安排决定冷却；手动单集请求保持原入口。
	var timing *repository.TMDbRecheckTiming
	if tmdbSeasonBatchFromContext(ctx) != nil {
		season, err := s.tmdb.GetTVSeasonDetails(ctx, seriesTMDbID, candidate.SeasonNum)
		if err != nil || season == nil {
			return nil, err
		}
		if season.ID <= 0 || season.SeasonNumber != candidate.SeasonNum {
			return nil, ErrTMDbRefreshIdentity
		}
		timing = tmdbSeasonRecheckTiming(season)
	}
	episode, err := s.tmdb.GetTVEpisodeDetails(ctx, seriesTMDbID, candidate.SeasonNum, candidate.EpisodeNum)
	if err != nil || episode == nil {
		return &tmdbMetadataRecheckDetails{timing: timing}, err
	}
	updates, _ := tmdbEpisodeMetadataUpdates(nil, episode, 0)
	return &tmdbMetadataRecheckDetails{id: episode.ID, updates: updates, rawJSON: episode.RawJSON, timing: timing}, nil
}
