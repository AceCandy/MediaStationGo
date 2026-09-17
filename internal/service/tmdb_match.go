package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (t *TMDbProvider) GetMovieMatch(ctx context.Context, tmdbID int) (*Match, error) {
	if tmdbID <= 0 {
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
	q.Set("append_to_response", "alternative_titles,translations,credits,external_ids,keywords,videos")
	u := base + "/movie/" + fmt.Sprint(tmdbID) + "?" + q.Encode()
	var r struct {
		ID               int     `json:"id"`
		Title            string  `json:"title"`
		OriginalTitle    string  `json:"original_title"`
		OriginalLanguage string  `json:"original_language"`
		Overview         string  `json:"overview"`
		PosterPath       string  `json:"poster_path"`
		BackdropPath     string  `json:"backdrop_path"`
		ReleaseDate      string  `json:"release_date"`
		VoteAverage      float32 `json:"vote_average"`
		Runtime          int     `json:"runtime"`
		Genres           []struct {
			Name string `json:"name"`
		} `json:"genres"`
		ProductionCountries []struct {
			Iso3166_1 string `json:"iso_3166_1"`
		} `json:"production_countries"`
		SpokenLanguages []struct {
			Iso639_1 string `json:"iso_639_1"`
		} `json:"spoken_languages"`
		AlternativeTitles struct {
			Titles []tmdbAlternativeTitle `json:"titles"`
		} `json:"alternative_titles"`
		Translations struct {
			Translations []tmdbTranslation `json:"translations"`
		} `json:"translations"`
		Credits     tmdbCredits `json:"credits"`
		ExternalIDs struct {
			IMDbID string `json:"imdb_id"`
		} `json:"external_ids"`
	}
	raw, err := t.getJSONRaw(ctx, u, &r)
	if err != nil {
		return nil, err
	}
	m := &Match{
		TMDbID:            r.ID,
		TMDbDetailsLoaded: true,
		MediaType:         "movie",
		Title:             r.Title,
		OriginalName:      r.OriginalTitle,
		Overview:          r.Overview,
		Rating:            r.VoteAverage,
		RuntimeSec:        r.Runtime * 60,
		Languages:         nonEmptyStrings(r.OriginalLanguage),
		IMDbID:            strings.TrimSpace(r.ExternalIDs.IMDbID),
		RawJSON:           raw,
	}
	if m.Title == "" {
		m.Title = r.OriginalTitle
	}
	applyTMDbChineseTitle(m, r.AlternativeTitles.Titles, r.Translations.Translations)
	if r.PosterPath != "" {
		m.PosterURL = t.imgCDN + "/w500" + r.PosterPath
		m.CatalogPosterURL = tmdbOriginalImageURL(t.imgCDN, r.PosterPath)
	}
	if r.BackdropPath != "" {
		m.BackdropURL = t.imgCDN + "/w1280" + r.BackdropPath
		m.CatalogBackdropURL = tmdbOriginalImageURL(t.imgCDN, r.BackdropPath)
	}
	m.ReleaseDate = normalizeReleaseDate(r.ReleaseDate)
	if len(r.ReleaseDate) >= 4 {
		_, _ = fmt.Sscanf(r.ReleaseDate[:4], "%d", &m.Year)
	}
	for _, g := range r.Genres {
		m.Genres = append(m.Genres, g.Name)
	}
	for _, c := range r.ProductionCountries {
		m.Countries = append(m.Countries, c.Iso3166_1)
	}
	for _, l := range r.SpokenLanguages {
		m.Languages = append(m.Languages, l.Iso639_1)
	}
	m.Credits, m.LoadedCreditTypes = tmdbCreditsToPersonCredits(r.Credits, t.imgCDN, false)
	m.Genres = deduplicate(m.Genres)
	m.Countries = deduplicate(m.Countries)
	m.Languages = deduplicate(m.Languages)
	return m, nil
}

func (t *TMDbProvider) GetTVMatch(ctx context.Context, tmdbID int) (*Match, error) {
	if tmdbID <= 0 {
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
	q.Set("append_to_response", "alternative_titles,translations,credits,external_ids,keywords,videos,content_ratings")
	u := base + "/tv/" + fmt.Sprint(tmdbID) + "?" + q.Encode()
	var r struct {
		ID               int      `json:"id"`
		Name             string   `json:"name"`
		OriginalName     string   `json:"original_name"`
		OriginalLanguage string   `json:"original_language"`
		OriginCountry    []string `json:"origin_country"`
		Overview         string   `json:"overview"`
		PosterPath       string   `json:"poster_path"`
		BackdropPath     string   `json:"backdrop_path"`
		FirstAirDate     string   `json:"first_air_date"`
		VoteAverage      float32  `json:"vote_average"`
		Genres           []struct {
			Name string `json:"name"`
		} `json:"genres"`
		SpokenLanguages []struct {
			Iso639_1 string `json:"iso_639_1"`
		} `json:"spoken_languages"`
		AlternativeTitles struct {
			Results []tmdbAlternativeTitle `json:"results"`
		} `json:"alternative_titles"`
		Translations struct {
			Translations []tmdbTranslation `json:"translations"`
		} `json:"translations"`
		Credits     tmdbCredits `json:"credits"`
		ExternalIDs struct {
			IMDbID string `json:"imdb_id"`
			TVDBID int    `json:"tvdb_id"`
		} `json:"external_ids"`
		Seasons []struct {
			ID           int    `json:"id"`
			SeasonNumber int    `json:"season_number"`
			Name         string `json:"name"`
			Overview     string `json:"overview"`
			AirDate      string `json:"air_date"`
			PosterPath   string `json:"poster_path"`
		} `json:"seasons"`
	}
	raw, err := t.getJSONRaw(ctx, u, &r)
	if err != nil {
		return nil, err
	}
	m := &Match{
		TMDbID:            r.ID,
		TMDbDetailsLoaded: true,
		MediaType:         "tv",
		Title:             r.Name,
		OriginalName:      r.OriginalName,
		Overview:          r.Overview,
		Rating:            r.VoteAverage,
		Languages:         nonEmptyStrings(r.OriginalLanguage),
		Countries:         deduplicate(r.OriginCountry),
		IMDbID:            strings.TrimSpace(r.ExternalIDs.IMDbID),
		RawJSON:           raw,
	}
	if r.ExternalIDs.TVDBID > 0 {
		m.TheTVDBID = strconv.Itoa(r.ExternalIDs.TVDBID)
	}
	for _, season := range r.Seasons {
		m.Seasons = append(m.Seasons, TMDbSeasonSummary{ID: season.ID, SeasonNumber: season.SeasonNumber, Name: season.Name, Overview: season.Overview, AirDate: season.AirDate, PosterPath: season.PosterPath})
	}
	if m.Title == "" {
		m.Title = r.OriginalName
	}
	applyTMDbChineseTitle(m, r.AlternativeTitles.Results, r.Translations.Translations)
	if r.PosterPath != "" {
		m.PosterURL = t.imgCDN + "/w500" + r.PosterPath
		m.CatalogPosterURL = tmdbOriginalImageURL(t.imgCDN, r.PosterPath)
	}
	if r.BackdropPath != "" {
		m.BackdropURL = t.imgCDN + "/w1280" + r.BackdropPath
		m.CatalogBackdropURL = tmdbOriginalImageURL(t.imgCDN, r.BackdropPath)
	}
	m.ReleaseDate = normalizeReleaseDate(r.FirstAirDate)
	if len(r.FirstAirDate) >= 4 {
		_, _ = fmt.Sscanf(r.FirstAirDate[:4], "%d", &m.Year)
	}
	for _, g := range r.Genres {
		m.Genres = append(m.Genres, g.Name)
	}
	for _, l := range r.SpokenLanguages {
		m.Languages = append(m.Languages, l.Iso639_1)
	}
	m.Credits, m.LoadedCreditTypes = tmdbCreditsToPersonCredits(r.Credits, t.imgCDN, false)
	m.Genres = deduplicate(m.Genres)
	m.Languages = deduplicate(m.Languages)
	return m, nil
}

type tmdbCredits struct {
	Cast []struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Character   string `json:"character"`
		Order       int    `json:"order"`
		ProfilePath string `json:"profile_path"`
	} `json:"cast"`
	Crew []struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Job         string `json:"job"`
		Order       int    `json:"order"`
		ProfilePath string `json:"profile_path"`
	} `json:"crew"`
}

func tmdbCreditsToPersonCredits(raw tmdbCredits, imageCDN string, episode bool) ([]PersonCredit, []string) {
	credits := make([]PersonCredit, 0, len(raw.Cast)+len(raw.Crew))
	loaded := []string{model.CreditTypeActor, model.CreditTypeDirector, model.CreditTypeWriter}
	if episode {
		loaded = []string{model.CreditTypeGuestStar, model.CreditTypeDirector, model.CreditTypeWriter}
	}
	for _, cast := range raw.Cast {
		if cast.ID <= 0 || strings.TrimSpace(cast.Name) == "" {
			continue
		}
		typ := model.CreditTypeActor
		if episode {
			typ = model.CreditTypeGuestStar
		}
		credits = append(credits, PersonCredit{Provider: "tmdb", ExternalID: strconv.Itoa(cast.ID), Name: strings.TrimSpace(cast.Name), Type: typ, OriginalRole: strings.TrimSpace(cast.Character), SortOrder: cast.Order, ProfileURL: tmdbProfileURL(imageCDN, cast.ProfilePath)})
	}
	for _, crew := range raw.Crew {
		typ := ""
		switch strings.ToLower(strings.TrimSpace(crew.Job)) {
		case "director":
			typ = model.CreditTypeDirector
		case "writer", "screenplay", "story", "teleplay":
			typ = model.CreditTypeWriter
		}
		if typ == "" || crew.ID <= 0 || strings.TrimSpace(crew.Name) == "" {
			continue
		}
		credits = append(credits, PersonCredit{Provider: "tmdb", ExternalID: strconv.Itoa(crew.ID), Name: strings.TrimSpace(crew.Name), Type: typ, OriginalRole: strings.TrimSpace(crew.Job), SortOrder: crew.Order, ProfileURL: tmdbProfileURL(imageCDN, crew.ProfilePath)})
	}
	return credits, loaded
}

func tmdbProfileURL(imageCDN, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return strings.TrimRight(imageCDN, "/") + "/w185" + path
}

func tmdbOriginalImageURL(imageCDN, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return strings.TrimRight(imageCDN, "/") + "/original" + path
}
