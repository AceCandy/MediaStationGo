package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// DiscoverDetail 只包含电影/整剧资料；时长来自资料源而非媒体探测。
type DiscoverDetail struct {
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
	work, err := s.repo.Metadata.FindByIdentifier(ctx, "tmdb", id.Kind(), strconv.Itoa(id.TMDbID))
	if err != nil {
		return nil, err
	}
	if work != nil {
		view, err := s.repo.MediaView.FindDiscoverPresentation(ctx, work.ID)
		if err != nil {
			return nil, err
		}
		if view != nil {
			identifiers, err := s.repo.Metadata.ListIdentifiers(ctx, work.ID)
			if err != nil {
				return nil, err
			}
			view.DoubanID, _ = uniqueIdentifier(identifiers, "douban", work.Kind)
			if err := s.attachMediaProviderDetails(ctx, view); err != nil {
				return nil, err
			}
			credits, err := s.ListMetadataCredits(ctx, work.ID)
			if err != nil {
				return nil, err
			}
			detail := &DiscoverDetail{Match: &Match{TMDbID: id.TMDbID, MediaType: id.MediaType, Title: view.Title, OriginalName: view.OriginalName, Overview: view.Overview, Rating: view.Rating, Year: view.Year, ReleaseDate: view.ReleaseDate, PosterURL: view.PosterURL, BackdropURL: view.BackdropURL, DoubanID: view.DoubanID, Genres: splitCSV(view.Genres), Languages: splitCSV(view.Languages), Countries: splitCSV(view.Countries)}, Credits: credits, RuntimeMinutes: []int{}, LocalMetadata: true, TMDbStatus: view.TMDbStatus, DoubanStatus: view.DoubanStatus}
			// 整剧只使用来源声明的单集时长，不把任一播放文件的时长当成全剧时长。
			snapshot, err := s.repo.Metadata.FindProviderSnapshot(ctx, work.ID, "tmdb")
			if err != nil {
				return nil, err
			}
			if snapshot != nil {
				detail.RuntimeMinutes = discoverRuntimeMinutes([]byte(snapshot.Payload), id.MediaType)
			}
			if id.MediaType == "movie" && work.RuntimeSec > 0 {
				detail.RuntimeMinutes = []int{(work.RuntimeSec + 59) / 60}
			}
			return detail, nil
		}
	}
	if s.tmdb == nil {
		return nil, errors.New("TMDb 未配置")
	}
	var match *Match
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
	detail := &DiscoverDetail{Match: match, Credits: []MediaCredit{}, RuntimeMinutes: discoverRuntimeMinutes(match.RawJSON, id.MediaType)}
	for _, credit := range match.Credits {
		detail.Credits = append(detail.Credits, MediaCredit{PersonID: "tmdb-" + credit.ExternalID, Name: credit.Name, Role: credit.OriginalRole, Type: credit.Type, ProfileURL: credit.ProfileURL})
	}
	// 本地无对应元数据时才请求远端预览。
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
