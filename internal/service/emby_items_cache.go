package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const embyItemsCachePrefix = "media:emby:"
const embyResponseCacheTTL = 5 * time.Minute
const maxEmbyResponseCacheBytes = 1 << 20

// embyResponseSnapshot 保存注入本次请求凭据前的独立 JSON 快照。
type embyResponseSnapshot struct {
	Body json.RawMessage `json:"body"`
	// ExpiresAt 防止 Redis 命中提升到 L1 时延长响应的五分钟期限。
	ExpiresAt time.Time `json:"expires_at"`
}

type embyResponseCacheState struct {
	Repository    string
	Content, User uint64
}

func embyUserCachePrefix(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return embyItemsCachePrefix + "v2:" + hex.EncodeToString(sum[:]) + ":"
}

func (e *EmbyService) embyItemsCacheKey(kind string, p ItemsParams) string {
	raw, _ := json.Marshal(struct {
		Kind   string
		Params ItemsParams
	}{kind, p})
	sum := sha256.Sum256(raw)
	return embyUserCachePrefix(p.UserID) + hex.EncodeToString(sum[:])
}

// responseCacheIdentity 捕获开始时的版本及认证可见范围，旧读取只允许填回旧键。
func (e *EmbyService) responseCacheIdentity(ctx context.Context, kind string, p ItemsParams) (string, embyResponseCacheState) {
	state := e.responseCacheState(p.UserID)
	auth, authenticated := ctx.Value(authenticatedUserKey{}).(authenticatedUserVisibility)
	raw, _ := json.Marshal(struct {
		Query         string
		State         embyResponseCacheState
		Visibility    MediaVisibility
		Authenticated bool
		AuthID        string
		HideAdult     bool
	}{e.embyItemsCacheKey(kind, p), state, e.mediaVisibility(ctx, p.UserID), authenticated, auth.id, auth.hideAdult})
	sum := sha256.Sum256(raw)
	return embyUserCachePrefix(p.UserID) + hex.EncodeToString(sum[:]), state
}

func (e *EmbyService) responseCacheState(userID string) embyResponseCacheState {
	content, user := e.cache.embyCacheVersions(userID)
	return embyResponseCacheState{e.repo.ReadCacheKey(), content, user}
}

// readResponseCache 保留列表顶层 Go 类型及嵌套整数精度，每次读取返回新对象。
func (e *EmbyService) readResponseCache(ctx context.Context, key string, state embyResponseCacheState, userID string, list bool) (map[string]any, bool) {
	var snapshot embyResponseSnapshot
	if !e.cache.GetJSON(ctx, key, &snapshot) || len(snapshot.Body) == 0 || !snapshot.ExpiresAt.After(time.Now()) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Body))
	decoder.UseNumber()
	var out map[string]any
	if list {
		var page struct {
			Items            []map[string]any
			TotalRecordCount *int64
			StartIndex       *int
		}
		if decoder.Decode(&page) != nil || page.Items == nil || page.TotalRecordCount == nil || page.StartIndex == nil {
			return nil, false
		}
		out = map[string]any{"Items": page.Items, "TotalRecordCount": *page.TotalRecordCount, "StartIndex": *page.StartIndex}
	} else if decoder.Decode(&out) != nil || out == nil {
		return nil, false
	}
	if ctx.Err() != nil || state != e.responseCacheState(userID) {
		return nil, false
	}
	return out, true
}

func (e *EmbyService) writeResponseCache(ctx context.Context, key string, state embyResponseCacheState, userID string, out map[string]any) {
	if key == "" || out == nil || ctx.Err() != nil {
		return
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > maxEmbyResponseCacheBytes || state != e.responseCacheState(userID) {
		return
	}
	e.cache.SetJSON(ctx, key, embyResponseSnapshot{Body: raw, ExpiresAt: time.Now().Add(embyResponseCacheTTL)}, embyResponseCacheTTL)
}

// CachedItem 只缓存 HTTP 浏览详情，内部写后读取仍使用 Item。
func (e *EmbyService) CachedItem(ctx context.Context, itemID, userID string) (map[string]any, error) {
	if e.cache == nil {
		return e.Item(ctx, itemID, userID)
	}
	p := ItemsParams{UserID: userID, IDs: []string{itemID}}
	key, state := e.responseCacheIdentity(ctx, "detail", p)
	if out, ok := e.readResponseCache(ctx, key, state, userID, false); ok {
		return out, nil
	}
	out, err := e.Item(ctx, itemID, userID)
	if err == nil {
		e.writeResponseCache(ctx, key, state, userID, out)
	}
	return out, err
}

// CachedResumeItemsPage 缓存续播最终页，不影响内部续播算法或精确进度入口。
func (e *EmbyService) CachedResumeItemsPage(ctx context.Context, p ItemsParams) (map[string]any, error) {
	if p.Limit <= 0 || p.Limit > 100 {
		p.Limit = 20
	}
	if p.StartIndex < 0 || p.StartIndex > int(^uint(0)>>1)-p.Limit {
		p.StartIndex = 0
	}
	key := ""
	var state embyResponseCacheState
	if e.cache != nil && !embyRandomSort(p) {
		key, state = e.responseCacheIdentity(ctx, "resume", p)
		if out, ok := e.readResponseCache(ctx, key, state, p.UserID, true); ok {
			return out, nil
		}
	}
	out, err := e.ResumeItemsPage(ctx, p)
	if err == nil {
		e.writeResponseCache(ctx, key, state, p.UserID, out)
	}
	return out, err
}
