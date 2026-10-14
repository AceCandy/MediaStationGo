package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

func TestEmbyResponseCacheKeyPreservesQuery(t *testing.T) {
	e := &EmbyService{}
	base := ItemsParams{UserID: "a", IDs: []string{"b", "a", "b"}, Limit: 2}
	variants := []ItemsParams{base, base, base, base, base}
	variants[0].IDs = []string{"a", "b", "b"}
	variants[1].IDs = []string{"b", "a"}
	variants[2].IDs = []string{"b,a,b"}
	variants[3].UserID = "b"
	variants[4].StartIndex = 1
	for _, p := range variants {
		if e.embyItemsCacheKey("items", base) == e.embyItemsCacheKey("items", p) {
			t.Fatalf("different query shares key: %#v", p)
		}
	}
	if e.embyItemsCacheKey("items", base) == e.embyItemsCacheKey("resume", base) {
		t.Fatal("Items and Resume share key")
	}
}

func TestEmbyResponseCacheSnapshotAndLimits(t *testing.T) {
	e := &EmbyService{repo: repository.New(nil), cache: NewRuntimeCacheService(nil, nil)}
	ctx := t.Context()
	state := e.responseCacheState("a")
	out := map[string]any{"Items": []map[string]any{{"Id": "item", "Ticks": int64(9007199254740993), "Nested": []map[string]any{{"Path": "/stream"}}}}, "TotalRecordCount": int64(1), "StartIndex": 0}
	key := embyUserCachePrefix("a") + "snapshot"
	before := time.Now()
	e.writeResponseCache(ctx, key, state, "a", out)
	expires := e.cache.memory[e.cache.key(key)].expiresAt
	if expires.Before(before.Add(embyResponseCacheTTL)) || expires.After(time.Now().Add(embyResponseCacheTTL)) {
		t.Fatalf("unexpected TTL: %v", expires.Sub(before))
	}
	out["Items"].([]map[string]any)[0]["Id"] = "mutated-cold"
	warm, ok := e.readResponseCache(ctx, key, state, "a", true)
	if !ok || warm["TotalRecordCount"] != int64(1) || warm["StartIndex"] != 0 {
		t.Fatalf("bad envelope: %#v, %v", warm, ok)
	}
	item := warm["Items"].([]map[string]any)[0]
	if item["Id"] != "item" || item["Ticks"] != json.Number("9007199254740993") {
		t.Fatalf("snapshot changed or integer rounded: %#v", item)
	}
	item["Nested"].([]any)[0].(map[string]any)["Path"] = "mutated-warm"
	again, ok := e.readResponseCache(ctx, key, state, "a", true)
	if !ok || again["Items"].([]map[string]any)[0]["Nested"].([]any)[0].(map[string]any)["Path"] != "/stream" {
		t.Fatal("warm objects share state")
	}
	for name, value := range map[string]map[string]any{
		"nil": nil, "large": {"Overview": strings.Repeat("x", maxEmbyResponseCacheBytes)}, "invalid": {"Bad": make(chan int)},
	} {
		e.writeResponseCache(ctx, name, state, "a", value)
		if _, found := e.cache.getMemory(e.cache.key(name)); found {
			t.Fatalf("cached %s response", name)
		}
	}
	empty := emptyItemsEnvelope(0)
	e.writeResponseCache(ctx, "empty", state, "a", empty)
	if _, ok := e.readResponseCache(ctx, "empty", state, "a", true); !ok {
		t.Fatal("valid empty page not cached")
	}
	e.cache.SetJSON(ctx, "broken", embyResponseSnapshot{Body: json.RawMessage(`{"Items":[]}`), ExpiresAt: time.Now().Add(time.Minute)}, time.Minute)
	if _, ok := e.readResponseCache(ctx, "broken", state, "a", true); ok {
		t.Fatal("incomplete envelope accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	e.writeResponseCache(cancelled, "cancelled", state, "a", empty)
	if _, ok := e.readResponseCache(cancelled, key, state, "a", true); ok {
		t.Fatal("cancelled request hit cache")
	}
	if _, found := e.cache.getMemory(e.cache.key("cancelled")); found {
		t.Fatal("cancelled response cached")
	}
	// Redis 命中可能短暂提升到 L1，但不得延长响应自身的五分钟期限。
	emptyJSON, _ := json.Marshal(empty)
	e.cache.SetJSON(ctx, "promoted-expired", embyResponseSnapshot{Body: emptyJSON, ExpiresAt: time.Now().Add(-time.Second)}, time.Minute)
	if _, ok := e.readResponseCache(ctx, "promoted-expired", state, "a", true); ok {
		t.Fatal("L1 promotion extended response lifetime")
	}
	e.cache.mu.Lock()
	entry := e.cache.memory[e.cache.key(key)]
	entry.expiresAt = time.Now().Add(-time.Second)
	e.cache.memory[e.cache.key(key)] = entry
	e.cache.mu.Unlock()
	if _, ok := e.readResponseCache(ctx, key, state, "a", true); ok {
		t.Fatal("expired response accepted")
	}
}

func TestEmbyResponseCacheInvalidationAndOldFill(t *testing.T) {
	e := &EmbyService{repo: repository.New(nil), cache: NewRuntimeCacheService(nil, nil)}
	ctx := t.Context()
	oldA, oldB := e.responseCacheState("a"), e.responseCacheState("b")
	keyA, keyB := embyUserCachePrefix("a")+"old", embyUserCachePrefix("b")+"old"
	out := emptyItemsEnvelope(0)
	e.writeResponseCache(ctx, keyA, oldA, "a", out)
	e.writeResponseCache(ctx, keyB, oldB, "b", out)
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		close(started)
		<-finish
		e.writeResponseCache(ctx, keyA, oldA, "a", out)
		close(done)
	}()
	<-started
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	e.cache.InvalidateEmbyUser(cancelled, "a")
	close(finish)
	<-done
	if _, ok := e.readResponseCache(ctx, keyA, oldA, "a", true); ok {
		t.Fatal("old user response survived invalidation")
	}
	if _, found := e.cache.getMemory(e.cache.key(keyA)); found {
		t.Fatal("in-flight old query refilled cache")
	}
	if _, ok := e.readResponseCache(ctx, keyB, oldB, "b", true); !ok || oldB != e.responseCacheState("b") {
		t.Fatal("user A invalidated user B")
	}
	e.cache.DeletePrefix(cancelled, "media:")
	if _, ok := e.readResponseCache(ctx, keyB, oldB, "b", true); ok {
		t.Fatal("global change retained old response")
	}
	current := e.responseCacheState("a")
	e.repo.InvalidateReadCache()
	e.writeResponseCache(ctx, "old-permissions", current, "a", out)
	if _, found := e.cache.getMemory(e.cache.key("old-permissions")); found {
		t.Fatal("permission change allowed old fill")
	}
	var nilCache *RuntimeCacheService
	nilCache.InvalidateEmbyUser(ctx, "a")
	nilCache.DeletePrefix(ctx, "media:")
}

func newResponseCacheFixture(t *testing.T) (*EmbyService, string, []string, *atomic.Int64) {
	t.Helper()
	e := newTestEmbyService(t).SetRuntimeCache(NewRuntimeCacheService(nil, nil))
	if err := e.repo.DB.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Cache Movies", Path: "/fixture/cache", Type: "movie", Enabled: true}
	if err := e.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 0; i < 3; i++ {
		metadata := createServiceSourceTestMetadata(t, e.repo.DB, model.MetadataItem{Kind: "movie", Title: fmt.Sprintf("Movie %d", i), Source: "local"})
		media := model.Media{LibraryID: lib.ID, MetadataID: metadata.ID, Path: fmt.Sprintf("/fixture/cache/%d.mkv", i)}
		if err := e.repo.DB.Create(&media).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, metadata.ID)
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{}
	for _, user := range []string{"a", "b"} {
		e.visibilityCache[e.repo.ReadCacheKey()+user] = embyVisibilityCacheEntry{visibility: MediaVisibility{IncludeNSFW: true, AllowedLibraryIDs: []string{lib.ID}}, expiresAt: time.Now().Add(time.Hour)}
	}
	queries := &atomic.Int64{}
	count := func(*gorm.DB) { queries.Add(1) }
	for _, register := range []func(string, func(*gorm.DB)) error{
		e.repo.DB.Callback().Query().Before("gorm:query").Register,
		e.repo.DB.Callback().Row().Before("gorm:row").Register,
		e.repo.DB.Callback().Raw().Before("gorm:raw").Register,
	} {
		if err := register("test:response-cache-count", count); err != nil {
			t.Fatal(err)
		}
	}
	return e, lib.ID, ids, queries
}

func assertResponseCacheWarm(t *testing.T, queries *atomic.Int64, read func() (map[string]any, error)) map[string]any {
	t.Helper()
	cold, err := read()
	if err != nil {
		t.Fatal(err)
	}
	queries.Store(0)
	warm, err := read()
	if err != nil || queries.Load() != 0 {
		t.Fatalf("warm response queried database: queries=%d err=%v", queries.Load(), err)
	}
	coldJSON, _ := json.Marshal(cold)
	warmJSON, _ := json.Marshal(warm)
	if string(coldJSON) != string(warmJSON) {
		t.Fatalf("cold/warm JSON differ:\n%s\n%s", coldJSON, warmJSON)
	}
	return warm
}

func TestEmbyResponseCacheFinalPages(t *testing.T) {
	e, lib, ids, queries := newResponseCacheFixture(t)
	for _, p := range []ItemsParams{
		{UserID: "a", ParentID: lib, IncludeItemTypes: []string{"Movie"}, Limit: 2, Recursive: true, SkipTotalRecordCount: true},
		{UserID: "a", ParentID: lib, IncludeItemTypes: []string{"Movie"}, Limit: 500, Recursive: true},
		{UserID: "a", IDs: []string{ids[1], ids[0], ids[1]}, Limit: 1},
	} {
		page := assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.Items(t.Context(), p) })
		if page["TotalRecordCount"] != int64(3) {
			t.Fatalf("wrong final total: %#v", page)
		}
		if p.SkipTotalRecordCount && len(page["Items"].([]map[string]any)) != 2 {
			t.Fatal("cached lookahead row leaked")
		}
		if len(p.IDs) > 0 {
			items := page["Items"].([]map[string]any)
			for i, id := range p.IDs {
				if items[i]["Id"] != id {
					t.Fatal("ID order or duplicate changed")
				}
			}
		}
	}
	if err := e.SetFavorite(t.Context(), "a", ids[0], true); err != nil {
		t.Fatal(err)
	}
	p := ItemsParams{UserID: "a", ParentID: lib, IncludeItemTypes: []string{"Movie"}, Filters: []string{"IsFavorite"}, Limit: 2, Recursive: true}
	page := assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.Items(t.Context(), p) })
	items := page["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["LibraryIds"].([]any)[0] != lib {
		t.Fatalf("favorite memberships not cached: %#v", items)
	}
}

func TestEmbyResponseCacheDetailsResumeAndState(t *testing.T) {
	e, lib, ids, queries := newResponseCacheFixture(t)
	ctx := t.Context()
	for _, user := range []string{"a", "b"} {
		assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.CachedItem(ctx, ids[0], user) })
		assertResponseCacheWarm(t, queries, func() (map[string]any, error) {
			return e.CachedResumeItemsPage(ctx, ItemsParams{UserID: user, Limit: 20})
		})
	}
	playback := NewPlaybackService(e.log, e.repo).SetRuntimeCache(e.cache)
	before := e.responseCacheState("a")
	if err := playback.RecordProgress(ctx, "a", "ignored", "", 1, 120000, MediaVisibility{}); err != nil || e.responseCacheState("a") != before {
		t.Fatal("ignored short progress invalidated cache")
	}
	var media model.Media
	if err := e.repo.DB.Where("metadata_id = ?", ids[0]).Take(&media).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DB.Exec(`CREATE UNIQUE INDEX cache_test_history_identity ON playback_histories (user_id, metadata_id) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatal(err)
	}
	if err := playback.RecordProgress(ctx, "a", media.ID, "", 60000, 120000, MediaVisibility{IncludeNSFW: true, AllowedLibraryIDs: []string{lib}}); err != nil {
		t.Fatal(err)
	}
	if e.responseCacheState("a") == before {
		t.Fatal("Web progress did not invalidate Emby")
	}
	queries.Store(0)
	if _, err := e.CachedItem(ctx, ids[0], "b"); err != nil || queries.Load() != 0 {
		t.Fatal("user A progress evicted user B detail")
	}
	page := assertResponseCacheWarm(t, queries, func() (map[string]any, error) {
		return e.CachedResumeItemsPage(ctx, ItemsParams{UserID: "a", Limit: 20})
	})
	if page["TotalRecordCount"] != int64(1) {
		t.Fatalf("resume did not refresh: %#v", page)
	}
	assertResponseCacheWarm(t, queries, func() (map[string]any, error) {
		return e.Items(ctx, ItemsParams{UserID: "a", Recursive: true, Filters: []string{"IsResumable"}, Limit: 20})
	})
	before = e.responseCacheState("a")
	if n, err := playback.DeleteHistoryForMedia(ctx, "a", media.ID, nil); err != nil || n != 1 || e.responseCacheState("a") == before {
		t.Fatalf("history deletion did not invalidate: %d %v", n, err)
	}
	before = e.responseCacheState("a")
	if n, err := playback.DeleteHistoryForMedia(ctx, "a", media.ID, nil); err != nil || n != 0 || e.responseCacheState("a") != before {
		t.Fatal("zero-row delete invalidated cache")
	}
	page = assertResponseCacheWarm(t, queries, func() (map[string]any, error) {
		return e.CachedResumeItemsPage(ctx, ItemsParams{UserID: "a", Limit: 20})
	})
	if page["TotalRecordCount"] != int64(0) {
		t.Fatal("deleted history remained resumable")
	}
}

func TestEmbyResponseCacheBypassAndPermissions(t *testing.T) {
	e, lib, ids, queries := newResponseCacheFixture(t)
	ctx := t.Context()
	p := ItemsParams{UserID: "a", ParentID: lib, IncludeItemTypes: []string{"Movie"}, Recursive: true, Limit: 2, SortBy: "Random"}
	for i := 0; i < 2; i++ {
		queries.Store(0)
		if _, err := e.Items(ctx, p); err != nil || queries.Load() == 0 {
			t.Fatalf("random cached: %v", err)
		}
	}
	p.SortBy = "SortName"
	p.SearchTerm = "Movie"
	for i := 0; i < 2; i++ {
		queries.Store(0)
		if _, err := e.SearchHints(ctx, p); err != nil || queries.Load() == 0 {
			t.Fatalf("independent SearchHints cached: %v", err)
		}
	}
	key, _ := e.responseCacheIdentity(ctx, "items", p)
	auth := WithAuthenticatedUser(ctx, &model.User{Base: model.Base{ID: "a"}})
	hidden := WithAuthenticatedUser(ctx, &model.User{Base: model.Base{ID: "a"}, HideAdult: true})
	keyAuth, _ := e.responseCacheIdentity(auth, "items", p)
	keyHidden, _ := e.responseCacheIdentity(hidden, "items", p)
	if key == keyAuth || keyAuth == keyHidden {
		t.Fatal("authentication visibility snapshots share key")
	}
	assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.CachedItem(ctx, ids[0], "a") })
	if err := e.repo.Library.UpdateFields(ctx, lib, map[string]any{"cover_url": "/fixture/new-cover.jpg"}); err != nil {
		t.Fatal(err)
	}
	queries.Store(0)
	if _, err := e.CachedItem(ctx, ids[0], "a"); err != nil || queries.Load() == 0 {
		t.Fatal("repository permission version did not bypass old response")
	}
	for i := 0; i < 2; i++ {
		queries.Store(0)
		out, err := e.CachedItem(ctx, "missing-detail", "a")
		if err != nil || out != nil || queries.Load() == 0 {
			t.Fatal("nil detail cached")
		}
	}
}

func TestEmbyResponseCacheProgressRollback(t *testing.T) {
	e, lib, ids, queries := newResponseCacheFixture(t)
	ctx := t.Context()
	assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.CachedItem(ctx, ids[0], "a") })
	var media model.Media
	if err := e.repo.DB.Where("metadata_id = ?", ids[0]).Take(&media).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DB.Exec(`CREATE UNIQUE INDEX rollback_cache_history_identity ON playback_histories (user_id, metadata_id) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DB.Callback().Create().Before("gorm:create").Register("test:fail-cache-event", func(tx *gorm.DB) {
		if tx.Statement.Table == "playback_events" {
			tx.AddError(errors.New("event write failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	before := e.responseCacheState("a")
	p := NewPlaybackService(e.log, e.repo).SetRuntimeCache(e.cache)
	err := p.RecordProgress(ctx, "a", media.ID, "session", 60000, 120000, MediaVisibility{IncludeNSFW: true, AllowedLibraryIDs: []string{lib}})
	if err == nil || e.responseCacheState("a") != before {
		t.Fatal("rolled-back transaction invalidated cache")
	}
	var count int64
	if err := e.repo.DB.Model(&model.PlaybackHistory{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed transaction retained history")
	}
	queries.Store(0)
	if _, err := e.CachedItem(ctx, ids[0], "a"); err != nil || queries.Load() != 0 {
		t.Fatal("rollback evicted valid detail")
	}
}

func TestEmbyResponseCacheNFOHierarchy(t *testing.T) {
	e := nfoBrowseFixture(t, 2, 2).SetRuntimeCache(NewRuntimeCacheService(nil, nil))
	queries := &atomic.Int64{}
	for _, register := range []func(string, func(*gorm.DB)) error{
		e.repo.DB.Callback().Query().Before("gorm:query").Register,
		e.repo.DB.Callback().Row().Before("gorm:row").Register,
	} {
		if err := register("test:cache-nfo-count", func(*gorm.DB) { queries.Add(1) }); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []ItemsParams{
		{UserID: "viewer", ParentID: "library-nfo", Limit: 1, SkipTotalRecordCount: true},
		{UserID: "viewer", ParentID: "nfo-show-1", Limit: 10},
		{UserID: "viewer", ParentID: "nfo-season-1", IncludeItemTypes: []string{"Episode"}, Limit: 10},
	} {
		assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.Items(t.Context(), p) })
	}
	assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.CachedItem(t.Context(), "nfo-show-1", "viewer") })
}

func TestEmbyResponseCachePublicPeopleChanges(t *testing.T) {
	e, _, ids, queries := newResponseCacheFixture(t)
	ctx := t.Context()
	scraper := &ScraperService{repo: e.repo, cache: e.cache}
	person := model.Person{Name: "Tony Leung", OriginalName: "Tony Leung", NormalizedName: "tony leung"}
	if err := e.repo.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	lookup := newTranslationCacheLookup("person_name", person.ID, person.OriginalName)
	if err := e.repo.DB.Create(&model.TranslationCache{Kind: lookup.Kind, ContextKey: lookup.ContextKey, SourceText: lookup.SourceText, TargetLanguage: lookup.TargetLanguage, PromptVersion: lookup.PromptVersion, TranslatedText: "梁朝伟"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"a", "b"} {
		assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.CachedItem(ctx, person.ID, user) })
	}
	before := e.responseCacheState("a")
	group := &pendingPeopleTranslation{lookup: lookup, targets: []repository.TranslationTarget{{Kind: "person_name", ID: person.ID, OriginalText: person.OriginalName}}}
	if applied, _, err := scraper.translatePeopleWindow(ctx, []*pendingPeopleTranslation{group}); err != nil || applied != 1 || e.responseCacheState("a") == before {
		t.Fatalf("translation did not invalidate: applied=%d err=%v", applied, err)
	}
	for _, user := range []string{"a", "b"} {
		item := assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.CachedItem(ctx, person.ID, user) })
		if item["Name"] != "梁朝伟" {
			t.Fatal("public translation left stale person response")
		}
	}
	// 人物资产已提交而补全标记失败时，也不能保留旧响应。
	if err := e.repo.DB.Callback().Update().Before("gorm:update").Register("test:fail-cache-hydration", func(tx *gorm.DB) {
		if tx.Statement.Table == "metadata_items" {
			tx.AddError(errors.New("hydration marker failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	before = e.responseCacheState("a")
	err := scraper.saveCreditInputs(ctx, ids[0], []string{model.CreditTypeActor}, []repository.CreditInput{{Provider: "tmdb", ExternalID: "140", Name: "Actor", Type: model.CreditTypeActor}})
	if err == nil || e.responseCacheState("a") == before {
		t.Fatal("partial credit commit failed to invalidate")
	}
	var count int64
	if err := e.repo.DB.Model(&model.PersonIdentifier{}).Where("provider='tmdb' AND external_id='140'").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("expected committed person before marker failure")
	}
}

func TestEmbyResponseCacheSourcePages(t *testing.T) {
	e, _, _, queries := newResponseCacheFixture(t)
	if err := e.repo.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for _, source := range []string{model.TaskSystemHongGuo, model.TaskSystemHuangGuoAI} {
		lib := model.Library{Name: source, Type: source, Path: "/fixture/" + source, Enabled: true}
		if err := e.repo.Library.Create(ctx, &lib); err != nil {
			t.Fatal(err)
		}
		id := "99001"
		if source == model.TaskSystemHongGuo {
			if _, err := e.repo.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: id, Title: "Source show", EpisodeCount: 1, Snapshot: []byte(`{}`)}); err != nil {
				t.Fatal(err)
			}
			if err := e.repo.HongGuo.SaveAlbum(ctx, id, hongguo.Album{ID: id, Season: 1}); err != nil {
				t.Fatal(err)
			}
		} else {
			input := huangguoai.Work{Summary: huangguoai.Summary{SourceID: id, Category: "ai-mogai", Title: "Source movie"}, Episodes: []huangguoai.Episode{{Number: 1, PagePath: "/video/99001/"}}}
			if err := e.repo.HuangGuoAI.RegisterSummaries(ctx, []huangguoai.Summary{input.Summary}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := e.repo.HuangGuoAI.SaveDetail(ctx, input); err != nil {
				t.Fatal(err)
			}
		}
		media := model.Media{LibraryID: lib.ID, CatalogSource: source, LookupCatalogID: id, SeasonNum: 1, EpisodeNum: 1, Path: "/fixture/" + source + "/1.strm"}
		if err := e.repo.Media.Upsert(ctx, &media); err != nil {
			t.Fatal(err)
		}
		e.visibilityCache[e.repo.ReadCacheKey()+"a"] = embyVisibilityCacheEntry{visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}
		p := ItemsParams{UserID: "a", ParentID: lib.ID, Limit: 2, SkipTotalRecordCount: true}
		page := assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.Items(ctx, p) })
		if page["TotalRecordCount"] != int64(1) {
			t.Fatalf("wrong %s source final page: %#v", source, page)
		}
		itemID := page["Items"].([]map[string]any)[0]["Id"].(string)
		assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.CachedItem(ctx, itemID, "a") })
		if err := e.SetFavorite(ctx, "a", itemID, true); err != nil {
			t.Fatal(err)
		}
		page = assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.Items(ctx, p) })
		if page["Items"].([]map[string]any)[0]["UserData"].(map[string]any)["IsFavorite"] != true {
			t.Fatal("source favorite retained stale page")
		}
	}
}

func TestEmbyResponseCacheRedisLayers(t *testing.T) {
	redisURL := os.Getenv("MEDIASTATION_TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("MEDIASTATION_TEST_REDIS_URL is not configured")
	}
	cfg := &config.Config{}
	cfg.Cache.RedisURL = redisURL
	cfg.Cache.RedisPrefix = fmt.Sprintf("emby-cache-test-%d", time.Now().UnixNano())
	cache := NewRuntimeCacheService(cfg, nil)
	if cache.client == nil {
		t.Fatal("test Redis unavailable; fallback is not a successful L2 test")
	}
	t.Cleanup(func() {
		cache.DeletePrefix(context.Background(), "media:")
		_ = cache.Close()
	})
	e := &EmbyService{repo: repository.New(nil), cache: cache}
	ctx := t.Context()
	state := e.responseCacheState("a")
	key := embyUserCachePrefix("a") + "redis"
	e.writeResponseCache(ctx, key, state, "a", emptyItemsEnvelope(0))
	if ttl, err := cache.client.PTTL(ctx, cache.key(key)).Result(); err != nil || ttl > embyResponseCacheTTL || ttl < embyResponseCacheTTL-time.Second {
		t.Fatalf("Redis TTL=%v err=%v", ttl, err)
	}
	cache.mu.Lock()
	cache.memory = map[string]runtimeCacheItem{}
	cache.mu.Unlock()
	if _, ok := e.readResponseCache(ctx, key, state, "a", true); !ok {
		t.Fatal("Redis L2 response did not restore after L1 clear")
	}
	restarted := &EmbyService{repo: repository.New(nil), cache: cache}
	if _, ok := restarted.readResponseCache(ctx, key, state, "a", true); ok {
		t.Fatal("new repository instance accepted old Redis response")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	cache.InvalidateEmbyUser(cancelled, "a")
	if exists, err := cache.client.Exists(ctx, cache.key(key)).Result(); err != nil || exists != 1 {
		t.Fatalf("did not exercise failed Redis deletion: exists=%d err=%v", exists, err)
	}
	if _, ok := e.readResponseCache(ctx, key, state, "a", true); ok {
		t.Fatal("failed Redis deletion retained stale user response")
	}
	e.writeResponseCache(ctx, key, state, "a", emptyItemsEnvelope(0))
	newState := e.responseCacheState("a")
	newKey := embyUserCachePrefix("a") + "new"
	e.writeResponseCache(ctx, newKey, newState, "a", emptyItemsEnvelope(0))
	cache.mu.Lock()
	cache.memory = map[string]runtimeCacheItem{}
	cache.mu.Unlock()
	if _, ok := e.readResponseCache(ctx, newKey, newState, "a", true); !ok {
		t.Fatal("new version did not reach Redis L2")
	}
}

func TestEmbyResponseCacheMaximumLookahead(t *testing.T) {
	e, lib, _, queries := newResponseCacheFixture(t)
	if err := e.repo.DB.Exec(testdb.MetadataFixtureSQL("id,kind,title,source", `SELECT 'cache-limit-'||n,'movie','Movie '||n,'local' FROM generate_series(4,501) n`)).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DB.Exec(`INSERT INTO media (id,metadata_id,library_id,path)
SELECT id||'-file',id,?,'/fixture/cache/'||id||'.mkv' FROM metadata_items WHERE id LIKE 'cache-limit-%'`, lib).Error; err != nil {
		t.Fatal(err)
	}
	p := ItemsParams{UserID: "a", ParentID: lib, IncludeItemTypes: []string{"Movie"}, Fields: []string{"BasicSyncInfo"}, Recursive: true, Limit: 500, SkipTotalRecordCount: true}
	page := assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.Items(t.Context(), p) })
	if len(page["Items"].([]map[string]any)) != 500 || page["TotalRecordCount"] != int64(501) {
		t.Fatal("maximum final page lost 501st lookahead or leaked it")
	}
	p.StartIndex = 500
	page = assertResponseCacheWarm(t, queries, func() (map[string]any, error) { return e.Items(t.Context(), p) })
	if len(page["Items"].([]map[string]any)) != 1 || page["TotalRecordCount"] != int64(501) {
		t.Fatal("maximum page changed final tail")
	}
}
