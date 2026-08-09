package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// GetTVSeasonDetails 返回一季自身详情和该季完整 Episode 清单。
func (t *TMDbProvider) GetTVSeasonDetails(ctx context.Context, tmdbID, seasonNumber int) (*TMDbSeasonDetails, error) {
	if tmdbID <= 0 || seasonNumber < 0 {
		return nil, nil
	}
	apiKey := t.resolveAPIKey(ctx)
	if apiKey == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("language", "zh-CN")
	q.Set("append_to_response", "external_ids,credits,translations,videos")
	u := t.resolveBaseURL(ctx) + "/tv/" + fmt.Sprint(tmdbID) + "/season/" + fmt.Sprint(seasonNumber) + "?" + q.Encode()
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
		Credits  tmdbCredits `json:"credits"`
		Episodes []struct {
			ID            int    `json:"id"`
			EpisodeNumber int    `json:"episode_number"`
			Name          string `json:"name"`
		} `json:"episodes"`
	}
	raw, err := t.getJSONRaw(ctx, u, &response)
	if err != nil {
		return nil, err
	}
	details := &TMDbSeasonDetails{
		ID: response.ID, SeasonNumber: response.SeasonNumber, Name: strings.TrimSpace(response.Name),
		Overview: strings.TrimSpace(response.Overview), AirDate: normalizeReleaseDate(response.AirDate),
		Rating: response.VoteAverage, PosterURL: tmdbOriginalImageURL(t.imgCDN, response.PosterPath),
		ExternalIDs: TMDbExternalIDs{IMDbID: strings.TrimSpace(response.ExternalIDs.IMDbID), TVDBID: response.ExternalIDs.TVDBID},
		RawJSON:     raw,
	}
	details.Credits, details.LoadedCreditTypes = tmdbCreditsToPersonCredits(response.Credits, t.imgCDN, false)
	for _, episode := range response.Episodes {
		if episode.EpisodeNumber > 0 {
			details.Episodes = append(details.Episodes, TMDbEpisodeSummary{ID: episode.ID, EpisodeNumber: episode.EpisodeNumber, Name: strings.TrimSpace(episode.Name)})
		}
	}
	return details, nil
}
