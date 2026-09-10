package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type tmdbSeasonBatchKey struct{}

var errTMDbEpisodeMissingFromSeason = errors.New("TMDb 整季清单中没有该集")

type tmdbSeasonBatch struct {
	mu       sync.Mutex
	entries  map[string]*tmdbSeasonEntry
	bytes    int
	requests atomic.Int64
}

type tmdbSeasonEntry struct {
	done     chan struct{}
	details  *TMDbSeasonDetails
	err      error
	canceled bool
}

// withTMDbSeasonBatch 只在一次后台执行内复用整季数据，不影响手动单集刷新。
func withTMDbSeasonBatch(ctx context.Context) context.Context {
	if tmdbSeasonBatchFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, tmdbSeasonBatchKey{}, &tmdbSeasonBatch{entries: make(map[string]*tmdbSeasonEntry)})
}

func tmdbSeasonBatchFromContext(ctx context.Context) *tmdbSeasonBatch {
	b, _ := ctx.Value(tmdbSeasonBatchKey{}).(*tmdbSeasonBatch)
	return b
}

func (b *tmdbSeasonBatch) season(ctx context.Context, t *TMDbProvider, series, season int) (*TMDbSeasonDetails, error) {
	key := fmt.Sprintf("%s\x00%s\x00%d/%d", t.resolveBaseURL(ctx), t.resolveAPIKey(ctx), series, season)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b.mu.Lock()
		if entry := b.entries[key]; entry != nil {
			b.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-entry.done:
				if entry.canceled {
					continue
				}
				return entry.details, entry.err
			}
		}
		entry := &tmdbSeasonEntry{done: make(chan struct{})}
		b.entries[key] = entry
		b.mu.Unlock()
		entry.details, entry.err = t.getTVSeasonDetails(ctx, series, season, "zh-CN")
		if entry.err == nil && entry.details != nil {
			entry.details, entry.err = t.completeSeasonOriginalLanguage(ctx, series, season, entry.details)
		}
		entry.canceled = ctx.Err() != nil
		if entry.canceled {
			entry.details, entry.err = nil, ctx.Err()
		}
		b.mu.Lock()
		// ponytail: 执行内 32 季/16 MiB 整批淘汰；只有命中率不足时才引入 LRU。
		if len(b.entries) > 32 || (entry.details != nil && b.bytes+len(entry.details.RawJSON) > 16<<20) {
			for k, cached := range b.entries {
				select {
				case <-cached.done:
					b.bytes -= len(cached.details.RawJSON)
					delete(b.entries, k)
				default:
				}
			}
		}
		if entry.err != nil || entry.details == nil || len(entry.details.RawJSON) > 16<<20 {
			delete(b.entries, key)
		} else {
			b.bytes += len(entry.details.RawJSON)
		}
		close(entry.done)
		b.mu.Unlock()
		return entry.details, entry.err
	}
}

func (t *TMDbProvider) episodeFromSeason(raw []byte, season, episode int) (*TMDbEpisodeDetails, error) {
	var inventory struct {
		ID       int               `json:"id"`
		Season   int               `json:"season_number"`
		Episodes []json.RawMessage `json:"episodes"`
	}
	if err := json.Unmarshal(raw, &inventory); err != nil {
		return nil, err
	}
	if inventory.ID <= 0 || inventory.Season != season {
		return nil, ErrTMDbRefreshIdentity
	}
	if inventory.Episodes == nil {
		return nil, errors.New("TMDb 整季响应缺少集清单")
	}
	for _, item := range inventory.Episodes {
		var identity struct {
			ID      int `json:"id"`
			Episode int `json:"episode_number"`
			Season  int `json:"season_number"`
		}
		if err := json.Unmarshal(item, &identity); err != nil {
			return nil, err
		}
		if identity.Episode == episode {
			if identity.ID <= 0 || identity.Season != season {
				return nil, ErrTMDbRefreshIdentity
			}
			return t.parseTVEpisodeDetails(item, episode)
		}
	}
	return nil, errTMDbEpisodeMissingFromSeason
}

// completeSeasonOriginalLanguage 只用剧的原始语言补缺，不遍历其他语言或请求单集。
func (t *TMDbProvider) completeSeasonOriginalLanguage(ctx context.Context, series, season int, details *TMDbSeasonDetails) (*TMDbSeasonDetails, error) {
	if details.ID <= 0 || details.SeasonNumber != season {
		return nil, ErrTMDbRefreshIdentity
	}
	needsText := false
	for _, episode := range details.Episodes {
		if tmdbEpisodeTitleNeedsOriginal(episode.Name) || metadataTitleNeedsChineseLocalization(&Match{Title: episode.Overview}) {
			needsText = true
			break
		}
	}
	if !needsText {
		return details, nil
	}
	q := url.Values{"api_key": {t.resolveAPIKey(ctx)}}
	var show struct {
		ID       int    `json:"id"`
		Language string `json:"original_language"`
	}
	if err := t.getJSON(ctx, fmt.Sprintf("%s/tv/%d?%s", t.resolveBaseURL(ctx), series, q.Encode()), &show); err != nil {
		return nil, err
	}
	language := strings.TrimSpace(show.Language)
	if show.ID != series {
		return nil, ErrTMDbRefreshIdentity
	}
	if language == "" {
		return details, nil
	}
	original, err := t.getTVSeasonDetails(ctx, series, season, language)
	if err != nil {
		return nil, err
	}
	if original == nil || original.ID != details.ID || original.SeasonNumber != season {
		return nil, ErrTMDbRefreshIdentity
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(details.RawJSON, &root); err != nil {
		return nil, err
	}
	var episodes []map[string]json.RawMessage
	if err := json.Unmarshal(root["episodes"], &episodes); err != nil {
		return nil, err
	}
	for _, fields := range episodes {
		var number, id int
		_ = json.Unmarshal(fields["episode_number"], &number)
		_ = json.Unmarshal(fields["id"], &id)
		fallback, err := t.episodeFromSeason(original.RawJSON, season, number)
		if errors.Is(err, errTMDbEpisodeMissingFromSeason) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if fallback == nil || fallback.ID != id {
			continue
		}
		var name, overview string
		_ = json.Unmarshal(fields["name"], &name)
		_ = json.Unmarshal(fields["overview"], &overview)
		if tmdbEpisodeTitleNeedsOriginal(name) {
			fields["name"], _ = json.Marshal(fallback.Name)
		}
		if metadataTitleNeedsChineseLocalization(&Match{Title: overview}) && fallback.Overview != "" {
			fields["overview"], _ = json.Marshal(fallback.Overview)
		}
	}
	root["episodes"], _ = json.Marshal(episodes)
	raw, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	return t.parseTVSeasonDetails(raw)
}

func tmdbEpisodeTitleNeedsOriginal(name string) bool {
	return tmdbEntityTitleIsGenerated(name, model.MetadataKindEpisode) || metadataTitleNeedsChineseLocalization(&Match{Title: name})
}
