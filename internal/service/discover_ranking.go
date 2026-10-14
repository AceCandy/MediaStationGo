package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// RankingPage 共用手动、定时刷新与普通读取；失败返回旧快照和错误，不推进成功时间。
func (d *DiscoverService) RankingPage(ctx context.Context, key string, page int, refresh bool, load func(context.Context) ([]ExternalMediaResult, error)) ([]ExternalMediaResult, time.Time, error) {
	provider := key
	for _, section := range DiscoverSections {
		if section.Key == key {
			provider = section.Provider
			break
		}
	}
	if tmdbDiscoverPath(key) != "" {
		provider = "tmdb"
	}
	value, _ := d.rankLocks.LoadOrStore(provider, make(chan struct{}, 1))
	gate := value.(chan struct{})
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return nil, time.Time{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	items, at, err := d.savedRankingPage(ctx, key, page)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !refresh && len(items) > 0 && time.Since(at) < 6*time.Hour {
		return items, at, nil
	}
	fresh, err := load(WithTMDbFresh(WithDoubanFresh(ctx)))
	if err != nil {
		return items, at, err
	}
	if len(fresh) == 0 {
		if page == 1 {
			return items, at, errors.New("榜单首页返回空数据")
		}
		return fresh, time.Time{}, nil
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if d.rankingRepo != nil {
		payload, err := json.Marshal(fresh)
		if err != nil {
			return items, at, err
		}
		if err := d.rankingRepo.Save(ctx, model.DiscoverRankingPage{Key: key, Page: page, Payload: string(payload), FetchedAt: now}); err != nil {
			return items, at, err
		}
	}
	d.sectionCache.mu.Lock()
	if page == 1 {
		for cachedKey := range d.sectionCache.entries {
			if strings.HasPrefix(cachedKey, key+":") {
				delete(d.sectionCache.entries, cachedKey)
			}
		}
	}
	d.sectionCache.entries[discoverSectionCacheKey(key, page)] = discoverSectionCacheEntry{items: cloneExternalMediaResults(fresh), storedAt: now}
	d.sectionCache.mu.Unlock()
	return fresh, now, nil
}

func (d *DiscoverService) savedRankingPage(ctx context.Context, key string, page int) ([]ExternalMediaResult, time.Time, error) {
	if d.rankingRepo != nil {
		row, err := d.rankingRepo.Load(ctx, key, page)
		if err != nil {
			return nil, time.Time{}, err
		}
		if row == nil {
			return nil, time.Time{}, nil
		}
		var items []ExternalMediaResult
		err = json.Unmarshal([]byte(row.Payload), &items)
		return items, row.FetchedAt, err
	}
	d.sectionCache.mu.RLock()
	entry := d.sectionCache.entries[discoverSectionCacheKey(key, page)]
	d.sectionCache.mu.RUnlock()
	return cloneExternalMediaResults(entry.items), entry.storedAt, nil
}
