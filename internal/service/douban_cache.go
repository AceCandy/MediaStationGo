package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type doubanFreshKey struct{}

// WithDoubanFresh 让人工刷新绕过有效期和失败退避，并要求本次获取成功。
func WithDoubanFresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, doubanFreshKey{}, true)
}

func (d *DoubanProvider) setDataRepository(repo *repository.Container, cache *RuntimeCacheService) {
	d.dataRepo, d.cache = repo, cache
}

func (d *DoubanProvider) detailTTL(ctx context.Context) time.Duration {
	if d.dataRepo != nil && d.dataRepo.Setting != nil {
		value, err := d.dataRepo.Setting.Get(ctx, "metadata.douban_detail_ttl_seconds")
		if seconds, parseErr := strconv.ParseInt(value, 10, 64); err == nil && parseErr == nil && seconds >= 3600 && seconds <= 30*86400 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 7 * 24 * time.Hour
}

// enterDataRequest 串行准入后重新检查缓存，合并同时到达的请求；等待可取消。
func (d *DoubanProvider) enterDataRequest(ctx context.Context) (func(), error) {
	select {
	case d.dataGate <- struct{}{}:
		return func() { <-d.dataGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *DoubanProvider) waitDataRequest(ctx context.Context) error {
	if delay := time.Until(d.dataNext); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	d.dataNext = time.Now().Add(d.dataDelay)
	return ctx.Err()
}

// cachedDetail 将资料获取与本地字段、图片处理分开，成功 JSON 独立落库。
func (d *DoubanProvider) cachedDetail(ctx context.Context, id, kind string, purpose string, fetch func() ([]byte, bool, error)) ([]byte, bool, time.Time, error) {
	if d.dataRepo == nil {
		raw, degraded, err := fetch()
		return raw, degraded, time.Now().UTC(), err
	}
	started := time.Now().UTC()
	release, err := d.enterDataRequest(ctx)
	if err != nil {
		return nil, false, time.Time{}, err
	}
	defer release()
	forced, _ := ctx.Value(doubanFreshKey{}).(bool)
	stored, err := d.dataRepo.Douban.Find(ctx, id, kind)
	if err != nil {
		return nil, false, time.Time{}, err
	}
	if stored != nil {
		ttl := d.detailTTL(ctx)
		if stored.Quality == model.DoubanQualityDegraded && ttl > 24*time.Hour {
			ttl = 24 * time.Hour
		}
		usable := purpose != "detail" || stored.Quality != model.DoubanQualityPartial
		if purpose == "regular" && stored.Quality == model.DoubanQualityPartial && stored.Payload != nil {
			// 普通刮削兼容旧摘要，但绑定搜索的 subject 不能替代详情。
			usable = model.DoubanPayloadQuality([]byte(*stored.Payload), false) == model.DoubanQualityPartial
		}
		if purpose == "binding" && stored.Payload != nil {
			_, identityErr := doubanBindingPayloadKind([]byte(*stored.Payload), id)
			usable = identityErr == nil
		}
		if stored.Payload != nil && stored.FetchedAt != nil && usable &&
			time.Since(*stored.FetchedAt) < ttl && (!forced || stored.FetchedAt.After(started)) {
			if _, err := doubanMatchFromRawJSON(id, []byte(*stored.Payload)); err == nil && model.DoubanPayloadMatchesIdentity([]byte(*stored.Payload), id, stored.EntityKind) {
				return []byte(*stored.Payload), stored.Quality == model.DoubanQualityDegraded, *stored.FetchedAt, nil
			}
		}
		if !forced && stored.NextRetryAt != nil && time.Now().Before(*stored.NextRetryAt) {
			return nil, false, time.Time{}, fmt.Errorf("%w: retry is deferred", ErrDoubanTemporarilyUnavailable)
		}
	}
	unknownKind := kind == ""
	if unknownKind {
		kind = model.MetadataKindMovie
		if stored != nil {
			kind = stored.EntityKind
		}
	}
	raw, degraded, err := fetch()
	if unknownKind && err == nil {
		identity := doubanCacheSubject(raw)
		if identity["type"] == "tv" || identity["is_tv"] == true {
			kind = model.MetadataKindSeries
		}
	}
	if err == nil {
		_, err = doubanMatchFromRawJSON(id, raw)
	}
	if err == nil {
		err = validateDoubanCacheIdentity(raw, id, kind)
	}
	at := time.Now().UTC()
	if err != nil {
		if ctx.Err() == nil {
			if saveErr := d.dataRepo.Douban.RecordFailure(ctx, id, kind, at, at.Add(doubanFailureDelay(err))); saveErr != nil {
				return nil, false, time.Time{}, saveErr
			}
		}
		return nil, false, time.Time{}, err
	}
	quality := model.DoubanPayloadQuality(raw, degraded)
	if purpose == "binding" {
		quality = model.DoubanQualityPartial
	}
	if err := d.dataRepo.Douban.Save(ctx, id, kind, raw, quality, at); err != nil {
		return nil, false, time.Time{}, err
	}
	if degraded && stored != nil && stored.Quality == model.DoubanQualityComplete {
		if err := d.dataRepo.Douban.RecordFailure(ctx, id, kind, at, at.Add(24*time.Hour)); err != nil {
			return nil, false, time.Time{}, err
		}
	}
	if d.cache != nil {
		d.cache.DeletePrefix(ctx, "media:")
		d.cache.DeletePrefix(ctx, embyItemsCachePrefix)
	}
	return raw, degraded, at, nil
}

func validateDoubanCacheIdentity(raw []byte, id, kind string) error {
	if !model.DoubanPayloadMatchesIdentity(raw, id, kind) {
		return fmt.Errorf("%w: subject identity or type mismatch", ErrDoubanTemporarilyUnavailable)
	}
	return nil
}

func doubanCacheSubject(raw []byte) map[string]any {
	var subject map[string]any
	_ = json.Unmarshal(raw, &subject)
	for _, key := range []string{"subject", "data"} {
		if nested, ok := subject[key].(map[string]any); ok {
			return nested
		}
	}
	return subject
}

type doubanRequestStatsKey struct{}

func withDoubanRequestStats(ctx context.Context) (context.Context, *atomic.Int64) {
	count := &atomic.Int64{}
	return context.WithValue(ctx, doubanRequestStatsKey{}, count), count
}

// cachedQuery 保存搜索/榜单响应；详情由 cachedDetail 校验和持久化。
func (d *DoubanProvider) cachedQuery(ctx context.Context, requestURL, referer string) ([]byte, int, error) {
	u, err := url.Parse(requestURL)
	if err != nil || d.dataRepo == nil || strings.Contains(u.Path, "/rexxar/api/v2/") || strings.Contains(u.Path, "subject_abstract") {
		return d.requestJSONRemote(ctx, requestURL, referer)
	}
	release, err := d.enterDataRequest(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer release()
	u.RawQuery = u.Query().Encode()
	u.Fragment = ""
	sum := sha256.Sum256([]byte(u.String()))
	key := hex.EncodeToString(sum[:])
	forced, _ := ctx.Value(doubanFreshKey{}).(bool)
	if !forced {
		raw, err := d.dataRepo.Douban.FindQuery(ctx, key, time.Now().UTC())
		if err != nil || raw != nil {
			return raw, 200, err
		}
	}
	raw, status, err := d.requestJSONRemote(ctx, requestURL, referer)
	if err != nil || status != 200 {
		return raw, status, err
	}
	valid := false
	switch {
	case strings.Contains(u.Path, "subject_suggest"):
		var results []map[string]json.RawMessage
		valid = json.Unmarshal(raw, &results) == nil && results != nil
	case strings.Contains(u.Path, "search_subjects"):
		var results map[string]json.RawMessage
		var subjects []map[string]json.RawMessage
		valid = json.Unmarshal(raw, &results) == nil && json.Unmarshal(results["subjects"], &subjects) == nil && subjects != nil
	}
	if u.Hostname() == "search.douban.com" {
		_, parseErr := parseDoubanBindingSearch(raw)
		valid = parseErr == nil
	}
	if !valid {
		return nil, status, fmt.Errorf("%w: invalid query payload", ErrDoubanTemporarilyUnavailable)
	}
	ttl := 5 * time.Minute
	if strings.Contains(u.Path, "search_subjects") {
		ttl = 6 * time.Hour
	}
	if err := d.dataRepo.Douban.SaveQuery(ctx, key, raw, time.Now().UTC(), ttl); err != nil {
		return nil, status, err
	}
	return raw, status, nil
}

func doubanFailureDelay(err error) time.Duration {
	if errors.Is(err, ErrDoubanSubjectNotFound) {
		return 24 * time.Hour
	}
	return 5 * time.Minute
}

func doubanMatchFetchedAt(match *Match) time.Time {
	if !match.DoubanFetchedAt.IsZero() {
		return match.DoubanFetchedAt
	}
	return time.Now().UTC()
}
