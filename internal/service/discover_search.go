package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SearchTMDb 搜索一页电影/电视剧摘要；人物结果不进入作品目录。
func (d *DiscoverService) SearchTMDb(ctx context.Context, query, kind string, page int) ([]ExternalMediaResult, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > 100 || page < 1 || page > 500 || (kind != "multi" && kind != "movie" && kind != "tv") {
		return nil, false, errors.New("invalid TMDb search")
	}
	if d == nil || d.tmdb == nil || d.tmdb.resolveAPIKey(ctx) == "" {
		return nil, false, errors.New("TMDb search unavailable")
	}
	q := url.Values{"api_key": {d.tmdb.resolveAPIKey(ctx)}, "language": {"zh-CN"}, "include_adult": {"false"}, "query": {query}, "page": {strconv.Itoa(page)}}
	var result struct {
		Results    []json.RawMessage `json:"results"`
		TotalPages int               `json:"total_pages"`
	}
	if err := d.tmdb.getJSON(ctx, d.tmdb.resolveBaseURL(ctx)+"/search/"+kind+"?"+q.Encode(), &result); err != nil {
		return nil, false, err
	}
	items := make([]ExternalMediaResult, 0, len(result.Results))
	for _, raw := range result.Results {
		var identity struct {
			MediaType string `json:"media_type"`
			Adult     bool   `json:"adult"`
		}
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, false, err
		}
		if identity.Adult {
			continue
		}
		mediaType := kind
		if kind == "multi" {
			mediaType = identity.MediaType
		}
		var match *Match
		switch mediaType {
		case "movie":
			var row tmdbMovieSearchResult
			if err := json.Unmarshal(raw, &row); err != nil {
				return nil, false, err
			}
			match = d.tmdb.movieSearchResultToMatch(row)
		case "tv":
			var row tmdbTVSearchResult
			if err := json.Unmarshal(raw, &row); err != nil {
				return nil, false, err
			}
			match = d.tmdb.tvSearchResultToMatch(row)
		default:
			continue
		}
		if match.TMDbID <= 0 || strings.TrimSpace(match.Title) == "" {
			continue
		}
		items = append(items, ExternalMediaResult{Source: "tmdb", MediaType: mediaType, TMDbID: match.TMDbID, Title: match.Title, OriginalName: match.OriginalName, Overview: match.Overview, PosterURL: match.PosterURL, BackdropURL: match.BackdropURL, Year: match.Year, ReleaseDate: match.ReleaseDate, Rating: match.Rating})
	}
	return dedupeExternalMedia(items), page < result.TotalPages && page < 500, nil
}

// QueueMissingSearchMetadata 只为尚无 Metadata 的搜索结果登记持久化补齐任务。
func (s *ScraperService) QueueMissingSearchMetadata(ctx context.Context, items []ExternalMediaResult) error {
	for _, item := range items {
		kind, ok := catalogEntityKind(item.MediaType)
		if !ok || !isTMDbCatalogItem(item) {
			continue
		}
		existing, err := s.repo.Metadata.FindByIdentifier(ctx, "tmdb", kind, strconv.Itoa(item.TMDbID))
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if err := s.QueueCatalogHydrationContext(ctx, []ExternalMediaResult{item}); err != nil {
			return err
		}
	}
	return nil
}
