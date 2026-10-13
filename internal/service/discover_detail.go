package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// DiscoverDetail 只包含电影/整剧资料；时长来自资料源而非媒体探测。
type DiscoverDetail struct {
	CenterID       string   `json:"center_id,omitempty"`
	MissingScopes  []string `json:"missing_scopes,omitempty"`
	SourceError    bool     `json:"source_error,omitempty"`
	RefreshPending bool     `json:"refresh_pending"`
	*Match
	RuntimeMinutes []int         `json:"runtime_minutes"`
	Credits        []MediaCredit `json:"credits"`
	LocalMetadata  bool          `json:"local_metadata"`
	TMDbStatus     string        `json:"tmdb_status,omitempty"`
	DoubanStatus   string        `json:"douban_status,omitempty"`
}

// DiscoverTMDbDetail 按点击读取完整资料，不触发入库或季集抓取。
func (s *MediaService) DiscoverTMDbDetail(ctx context.Context, id repository.DiscoverIdentity) (*DiscoverDetail, error) {
	if !id.Valid() {
		return nil, errors.New("作品身份无效")
	}
	if s.tmdb == nil {
		return nil, errors.New("TMDb 未配置")
	}
	if s.tmdb.catalog != nil {
		return s.tmdb.catalog.discoverDetail(ctx, id.MediaType, id.TMDbID)
	}
	var match *Match
	var err error
	if id.MediaType == "movie" {
		match, err = s.tmdb.GetMovieMatch(ctx, id.TMDbID)
	} else {
		match, err = s.tmdb.GetTVMatch(ctx, id.TMDbID)
	}
	if err != nil {
		return nil, err
	}
	if match == nil || match.TMDbID != id.TMDbID {
		return nil, errors.New("TMDb 未返回对应作品详情")
	}
	if s.tmdb.catalog != nil {
		match.PosterURL = tmdbCatalogSubjectImage(id.MediaType, id.TMDbID, "poster")
		match.BackdropURL = tmdbCatalogSubjectImage(id.MediaType, id.TMDbID, "backdrop")
	}
	detail := &DiscoverDetail{Match: match, Credits: []MediaCredit{}, RuntimeMinutes: discoverRuntimeMinutes(match.RawJSON, id.MediaType)}
	for _, credit := range match.Credits {
		if s.tmdb.catalog != nil {
			credit.ProfileURL = "/api/catalogs/tmdb/images/person/" + credit.ExternalID + "/profile"
		}
		detail.Credits = append(detail.Credits, MediaCredit{PersonID: "tmdb-" + credit.ExternalID, Name: credit.Name, Role: credit.OriginalRole, Type: credit.Type, ProfileURL: credit.ProfileURL})
	}
	return detail, nil
}

// discoverRuntimeMinutes 同时读取本地快照和远端资料，缺失或无效时长不推算。
func discoverRuntimeMinutes(payload []byte, mediaType string) []int {
	minutes := []int{}
	var runtime struct {
		Movie    int   `json:"runtime"`
		Episodes []int `json:"episode_run_time"`
	}
	if json.Unmarshal(payload, &runtime) != nil {
		return minutes
	}
	values := runtime.Episodes
	if mediaType == "movie" {
		values = []int{runtime.Movie}
	}
	seen := map[int]bool{}
	for _, value := range values {
		if value > 0 && !seen[value] {
			minutes = append(minutes, value)
			seen[value] = true
		}
	}
	return minutes
}
