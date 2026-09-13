package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// GetTVSeasonDetails 返回一季自身详情和该季完整 Episode 清单。
func (t *TMDbProvider) GetTVSeasonDetails(ctx context.Context, tmdbID, seasonNumber int) (*TMDbSeasonDetails, error) {
	if batch := tmdbSeasonBatchFromContext(ctx); batch != nil {
		return batch.season(ctx, t, tmdbID, seasonNumber)
	}
	return t.getTVSeasonDetails(ctx, tmdbID, seasonNumber, "zh-CN")
}

func (t *TMDbProvider) getTVSeasonDetails(ctx context.Context, tmdbID, seasonNumber int, language string) (*TMDbSeasonDetails, error) {
	if tmdbID <= 0 || seasonNumber < 0 {
		return nil, nil
	}
	apiKey := t.resolveAPIKey(ctx)
	if apiKey == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("language", language)
	q.Set("append_to_response", "external_ids,credits,translations,videos")
	u := t.resolveBaseURL(ctx) + "/tv/" + fmt.Sprint(tmdbID) + "/season/" + fmt.Sprint(seasonNumber) + "?" + q.Encode()
	raw, err := t.getJSONRaw(ctx, u, &json.RawMessage{})
	if err != nil {
		return nil, err
	}
	return t.parseTVSeasonDetails(raw)
}

// parseTVSeasonDetails 让网络响应与已保存的整季快照使用相同的字段投影。
func (t *TMDbProvider) parseTVSeasonDetails(raw []byte) (*TMDbSeasonDetails, error) {
	var response struct {
		ID           int     `json:"id"`
		SeasonNumber int     `json:"season_number"`
		Name         string  `json:"name"`
		Overview     string  `json:"overview"`
		AirDate      string  `json:"air_date"`
		VoteAverage  float32 `json:"vote_average"`
		PosterPath   string  `json:"poster_path"`
		ExternalIDs  struct {
			IMDbID string `json:"imdb_id"`
			TVDBID int    `json:"tvdb_id"`
		} `json:"external_ids"`
		Credits      tmdbCredits `json:"credits"`
		Translations struct {
			Translations []tmdbTranslation `json:"translations"`
		} `json:"translations"`
		Episodes []struct {
			ID            int     `json:"id"`
			EpisodeNumber int     `json:"episode_number"`
			Name          string  `json:"name"`
			Overview      string  `json:"overview"`
			AirDate       string  `json:"air_date"`
			Rating        float32 `json:"vote_average"`
			Runtime       int     `json:"runtime"`
			StillPath     string  `json:"still_path"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	details := &TMDbSeasonDetails{
		ID: response.ID, SeasonNumber: response.SeasonNumber,
		Name:     preferredTMDbEntityTitle(response.Name, response.Translations.Translations, model.MetadataKindSeason, response.SeasonNumber),
		Overview: preferredTMDbEntityOverview(response.Overview, response.Translations.Translations), AirDate: normalizeReleaseDate(response.AirDate),
		Rating: response.VoteAverage, PosterURL: tmdbOriginalImageURL(t.imgCDN, response.PosterPath),
		ExternalIDs: TMDbExternalIDs{IMDbID: strings.TrimSpace(response.ExternalIDs.IMDbID), TVDBID: response.ExternalIDs.TVDBID},
		RawJSON:     raw,
	}
	details.Credits, details.LoadedCreditTypes = tmdbCreditsToPersonCredits(response.Credits, t.imgCDN, false)
	for _, episode := range response.Episodes {
		if episode.EpisodeNumber > 0 {
			details.Episodes = append(details.Episodes, TMDbEpisodeSummary{ID: episode.ID, EpisodeNumber: episode.EpisodeNumber, Name: strings.TrimSpace(episode.Name), Overview: strings.TrimSpace(episode.Overview), AirDate: normalizeReleaseDate(episode.AirDate), Rating: episode.Rating, Runtime: episode.Runtime, StillPath: episode.StillPath})
		}
	}
	return details, nil
}
