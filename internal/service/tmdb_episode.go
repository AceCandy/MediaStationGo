package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (t *TMDbProvider) GetTVEpisodeDetails(ctx context.Context, tmdbID, season, episode int) (*TMDbEpisodeDetails, error) {
	if tmdbID <= 0 || episode <= 0 {
		return nil, nil
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
		Credits    tmdbCredits `json:"credits"`
		GuestStars []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Character   string `json:"character"`
			Order       int    `json:"order"`
			ProfilePath string `json:"profile_path"`
		} `json:"guest_stars"`
		Crew []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Job         string `json:"job"`
			Order       int    `json:"order"`
			ProfilePath string `json:"profile_path"`
		} `json:"crew"`
	}
	raw, err := t.getJSONRaw(ctx, u, &r)
	if err != nil {
		return nil, err
	}
	details := &TMDbEpisodeDetails{
		ID:          r.ID,
		Name:        r.Name,
		Overview:    r.Overview,
		AirDate:     normalizeReleaseDate(r.AirDate),
		Rating:      r.VoteAverage,
		Runtime:     r.Runtime,
		ExternalIDs: TMDbExternalIDs{IMDbID: strings.TrimSpace(r.ExternalIDs.IMDbID), TVDBID: r.ExternalIDs.TVDBID},
		RawJSON:     raw,
	}
	details.LoadedCreditTypes = []string{model.CreditTypeGuestStar, model.CreditTypeDirector, model.CreditTypeWriter}
	for _, cast := range r.GuestStars {
		if cast.ID > 0 && strings.TrimSpace(cast.Name) != "" {
			details.Credits = append(details.Credits, PersonCredit{Provider: "tmdb", ExternalID: strconv.Itoa(cast.ID), Name: strings.TrimSpace(cast.Name), Type: model.CreditTypeGuestStar, OriginalRole: strings.TrimSpace(cast.Character), SortOrder: cast.Order, ProfileURL: tmdbProfileURL(t.imgCDN, cast.ProfilePath)})
		}
	}
	for _, crew := range r.Crew {
		typ := ""
		switch strings.ToLower(strings.TrimSpace(crew.Job)) {
		case "director":
			typ = model.CreditTypeDirector
		case "writer", "screenplay", "story", "teleplay":
			typ = model.CreditTypeWriter
		}
		if typ != "" && crew.ID > 0 && strings.TrimSpace(crew.Name) != "" {
			details.Credits = append(details.Credits, PersonCredit{Provider: "tmdb", ExternalID: strconv.Itoa(crew.ID), Name: strings.TrimSpace(crew.Name), Type: typ, OriginalRole: strings.TrimSpace(crew.Job), SortOrder: crew.Order, ProfileURL: tmdbProfileURL(t.imgCDN, crew.ProfilePath)})
		}
	}
	if appended, _ := tmdbCreditsToPersonCredits(r.Credits, t.imgCDN, true); len(appended) > 0 {
		details.Credits = appended
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
