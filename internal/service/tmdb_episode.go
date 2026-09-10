package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (t *TMDbProvider) GetTVEpisodeDetails(ctx context.Context, tmdbID, season, episode int) (*TMDbEpisodeDetails, error) {
	if tmdbID <= 0 || episode <= 0 {
		return nil, nil
	}
	if tmdbSeasonBatchFromContext(ctx) != nil {
		details, err := t.GetTVSeasonDetails(ctx, tmdbID, season)
		if err != nil || details == nil {
			return nil, err
		}
		return t.episodeFromSeason(details.RawJSON, season, episode)
	}
	apiKey := t.resolveAPIKey(ctx)
	if apiKey == "" {
		return nil, nil
	}
	base := t.resolveBaseURL(ctx)
	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("language", "zh-CN")
	q.Set("append_to_response", "external_ids,credits,translations,videos")
	u := base + "/tv/" + fmt.Sprint(tmdbID) + "/season/" + fmt.Sprint(season) + "/episode/" + fmt.Sprint(episode) + "?" + q.Encode()
	raw, err := t.getJSONRaw(ctx, u, &json.RawMessage{})
	if err != nil {
		return nil, err
	}
	return t.parseTVEpisodeDetails(raw, episode)
}

// parseTVEpisodeDetails 共用单集与整季拆分的字段投影；演职员只保存于季。
func (t *TMDbProvider) parseTVEpisodeDetails(raw []byte, episode int) (*TMDbEpisodeDetails, error) {
	var r struct {
		Name        string  `json:"name"`
		Overview    string  `json:"overview"`
		StillPath   string  `json:"still_path"`
		AirDate     string  `json:"air_date"`
		VoteAverage float32 `json:"vote_average"`
		Runtime     int     `json:"runtime"`
		ID          int     `json:"id"`
		ExternalIDs struct {
			IMDbID string `json:"imdb_id"`
			TVDBID int    `json:"tvdb_id"`
		} `json:"external_ids"`
		Translations struct {
			Translations []tmdbTranslation `json:"translations"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	details := &TMDbEpisodeDetails{
		ID:          r.ID,
		Name:        preferredTMDbEntityTitle(r.Name, r.Translations.Translations, model.MetadataKindEpisode, episode),
		Overview:    preferredTMDbEntityOverview(r.Overview, r.Translations.Translations),
		AirDate:     normalizeReleaseDate(r.AirDate),
		Rating:      r.VoteAverage,
		Runtime:     r.Runtime,
		ExternalIDs: TMDbExternalIDs{IMDbID: strings.TrimSpace(r.ExternalIDs.IMDbID), TVDBID: r.ExternalIDs.TVDBID},
		RawJSON:     raw,
	}
	if r.StillPath != "" {
		details.StillURL = t.imgCDN + "/w500" + r.StillPath
		details.CatalogStillURL = tmdbOriginalImageURL(t.imgCDN, r.StillPath)
	}
	if len(r.AirDate) >= 4 {
		_, _ = fmt.Sscanf(r.AirDate[:4], "%d", &details.AirYear)
	}
	return details, nil
}

func (t *TMDbProvider) GetTVEpisodeCount(ctx context.Context, tmdbID int) (int, error) {
	if tmdbID <= 0 {
		return 0, nil
	}
	apiKey := t.resolveAPIKey(ctx)
	if apiKey == "" {
		return 0, nil
	}
	base := t.resolveBaseURL(ctx)
	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("language", "zh-CN")
	u := base + "/tv/" + fmt.Sprint(tmdbID) + "?" + q.Encode()
	var r struct {
		NumberOfEpisodes int `json:"number_of_episodes"`
	}
	if err := t.getJSON(ctx, u, &r); err != nil {
		return 0, err
	}
	return r.NumberOfEpisodes, nil
}
