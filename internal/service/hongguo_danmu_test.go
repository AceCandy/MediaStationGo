package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type danmuTransport func(*http.Request) (*http.Response, error)

func (f danmuTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newDanmuTestServices(t *testing.T) (*repository.Container, *APIConfigService, *HongGuoDanmuService) {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := NewAPIConfigService(zap.NewNop(), repos, NewCryptoService("test-only", zap.NewNop()))
	svc := NewHongGuoDanmuService(repos.HongGuo, cfg, zap.NewNop())
	t.Cleanup(svc.Close)
	return repos, cfg, svc
}

func TestHongGuoDanmuMergeXML(t *testing.T) {
	var history []model.HongGuoDanmu
	var live []hongguo.Danmu
	for n := 1; n <= 100; n++ {
		history = append(history, model.HongGuoDanmu{SourceID: "123", EpisodeNumber: 1, CommentID: strconv.Itoa(n), Content: "old", OffsetMS: int64(n)})
	}
	for n := 6; n <= 120; n++ {
		live = append(live, hongguo.Danmu{ID: strconv.Itoa(n), Content: "new", OffsetMS: int64(1000 - n)})
	}
	live = append(live, live[0], hongguo.Danmu{ID: "121", OffsetMS: -1, Content: "invalid"})
	merged, added := mergeHongGuoDanmus("123", 1, history, live)
	if len(merged) != 120 || len(added) != 20 {
		t.Fatalf("merge=%d added=%d", len(merged), len(added))
	}
	for n, row := range merged {
		if n > 0 && merged[n-1].OffsetMS > row.OffsetMS {
			t.Fatal("unsorted")
		}
		if row.CommentID == "6" && row.Content != "old" {
			t.Fatal("overwrote history")
		}
	}
	_, again := mergeHongGuoDanmus("123", 1, merged, live)
	if len(again) != 0 {
		t.Fatal("not idempotent")
	}
	xmlBytes, err := marshalHongGuoDanmus([]model.HongGuoDanmu{{CommentID: "7683198247380192281", OffsetMS: 1001, Content: "<&\x00你好", SourceCreatedAt: 1700000000}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Rows []struct {
			P    string `xml:"p,attr"`
			Text string `xml:",chardata"`
		} `xml:"d"`
	}
	if err := xml.Unmarshal(xmlBytes, &doc); err != nil || len(doc.Rows) != 1 || doc.Rows[0].Text != "<&你好" || doc.Rows[0].P != "1.001,1,25,16777215,1700000000,0,0,7683198247380192281,0" {
		t.Fatalf("xml=%s err=%v", xmlBytes, err)
	}
}

func TestHongGuoAPIConfigIsolation(t *testing.T) {
	repos, cfg, _ := newDanmuTestServices(t)
	ctx := t.Context()
	if err := cfg.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	for _, patch := range []APIConfigPatch{{HongGuoApp: &hongguo.DanmuAppConfig{}}, {HongGuoApp: &hongguo.DanmuAppConfig{Cookie: "FAKE_COOKIE", Token: "FAKE_TOKEN", DeviceID: "123456789", Query: map[string]string{"aid": "8662"}}}, {HongGuoApp: &hongguo.DanmuAppConfig{UserAgent: "FAKE_UA"}}} {
		if _, err := cfg.Update(ctx, "hongguo", patch); err != nil {
			t.Fatal(err)
		}
	}
	app, enabled, err := cfg.ResolveHongGuoApp(ctx)
	if err != nil || !enabled || app.Cookie != "FAKE_COOKIE" || app.UserAgent != "FAKE_UA" || app.DeviceID != "123456789" {
		t.Fatal("patch did not preserve parameters")
	}
	var stored model.APIConfig
	if err := repos.DB.Where("provider = ?", "hongguo").Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !cfg.crypto.IsEncrypted(stored.APIKey) || strings.Contains(stored.APIKey, "FAKE") {
		t.Fatal("plaintext storage")
	}
	views, err := cfg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(views)
	if strings.Contains(string(data), "FAKE") || strings.Contains(string(data), "123456789") || strings.Contains(string(data), "enc:v1:") {
		t.Fatal("secret projection")
	}
	view, _ := cfg.Get(ctx, "hongguo")
	if view.MaskedKey != "" || !view.HongGuoApp["cookie"] {
		t.Fatal("unsafe/incomplete status")
	}
	legacy := model.ApiConfig{APIKey: "FAKE_SECRET"}
	data, _ = json.Marshal(legacy)
	if strings.Contains(string(data), "FAKE_SECRET") {
		t.Fatal("legacy leaks")
	}
	bad := []hongguo.DanmuAppConfig{{Cookie: "x\r\ny"}, {Query: map[string]string{"host": "example.invalid"}}, {Query: map[string]string{"x-argus": "secret"}}, {DeviceID: "not-id"}, {Query: map[string]string{"aid": "oops"}}}
	for _, app := range bad {
		if _, err := cfg.Update(ctx, "hongguo", APIConfigPatch{HongGuoApp: &app}); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	old := "fake-old-provider-key"
	base := "https://example.invalid"
	if _, err := cfg.Update(ctx, "tmdb", APIConfigPatch{APIKey: &old, BaseURL: &base}); err != nil {
		t.Fatal(err)
	}
	resolved, _ := cfg.Resolve(ctx, "tmdb")
	if resolved.APIKey != old || resolved.BaseURL != base {
		t.Fatal("old provider changed")
	}
	off := false
	if _, err := cfg.Update(ctx, "hongguo", APIConfigPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	_, enabled, err = cfg.ResolveHongGuoApp(ctx)
	if enabled || err != nil {
		t.Fatal("disable")
	}
	if err := cfg.Delete(ctx, "hongguo"); err != nil {
		t.Fatal(err)
	}
	view, _ = cfg.Get(ctx, "hongguo")
	if view.HasKey || view.HongGuoApp["cookie"] {
		t.Fatal("clear")
	}
	broken := NewAPIConfigService(zap.NewNop(), repos, NewCryptoService("", zap.NewNop()))
	if _, err := broken.Update(ctx, "hongguo", APIConfigPatch{HongGuoApp: &hongguo.DanmuAppConfig{Cookie: "FAKE"}}); err == nil {
		t.Fatal("plaintext fallback")
	}
}

func TestHongGuoDanmuAsyncPersistenceAndClose(t *testing.T) {
	repos, cfg, svc := newDanmuTestServices(t)
	ctx := t.Context()
	var history []model.HongGuoDanmu
	for n := 1; n <= 100; n++ {
		history = append(history, model.HongGuoDanmu{SourceID: "123", EpisodeNumber: 1, CommentID: strconv.Itoa(n), Content: "same", OffsetMS: int64(n)})
	}
	if err := repos.HongGuo.InsertDanmus(ctx, history); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	svc.client = hongguo.NewClient(&http.Client{Transport: danmuTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := `{"code":0,"data":{"video_model":{"video_duration":2}}}`
		if strings.Contains(r.URL.Path, "commentapi") {
			var list []any
			for n := 6; n <= 120; n++ {
				list = append(list, map[string]any{"comment": map[string]any{"comment_id": strconv.Itoa(n), "common": map[string]any{"content": map[string]any{"text": "same"}}, "expand": map[string]any{"offset_time": n}}})
			}
			data, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"data_list": list, "extra": map[string]any{"next_query_danmaku_list_time": 30000}}})
			body = string(data)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	if err := repos.DB.Callback().Create().Before("gorm:create").Register("danmu_block", func(tx *gorm.DB) {
		if tx.Statement.Table == "hongguo_danmus" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer repos.DB.Callback().Create().Remove("danmu_block")
	done := make(chan []byte, 1)
	go func() {
		output, err := svc.Get(ctx, "123", 1, "456")
		if err != nil {
			done <- nil
			return
		}
		done <- output
	}()
	select {
	case output := <-done:
		if strings.Count(string(output), "<d p=") != 120 {
			t.Fatalf("expected120: %s", output)
		}
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("response blocked on save")
	}
	<-entered
	closed := make(chan struct{})
	go func() { svc.Close(); close(closed) }()
	select {
	case <-closed:
		close(release)
		t.Fatal("close did not wait")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("close stuck")
	}
	rows, err := repos.HongGuo.Danmus(ctx, "123", 1)
	if err != nil || len(rows) != 120 {
		t.Fatalf("persisted %d %v", len(rows), err)
	}
	if _, err := svc.Get(ctx, "123", 1, "456"); err == nil {
		t.Fatal("request after close")
	}
	// 唯一键兜住并发重复写，其他源作品及集号不能串数据。
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repos.HongGuo.InsertDanmus(ctx, history); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, key := range []struct {
		source  string
		episode int
	}{{"123", 2}, {"124", 1}} {
		rows, err := repos.HongGuo.Danmus(ctx, key.source, key.episode)
		if err != nil || len(rows) != 0 {
			t.Fatal("coordinate leakage")
		}
	}
	// 关闭实时后，不访问源站，仍返回历史；源 video ID 不属于持久化键。
	other := NewHongGuoDanmuService(repos.HongGuo, cfg, zap.NewNop())
	defer other.Close()
	off := false
	if _, err := cfg.Update(ctx, "hongguo", APIConfigPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	other.client = hongguo.NewClient(&http.Client{Transport: danmuTransport(func(*http.Request) (*http.Response, error) {
		t.Error("disabled upstream called")
		return nil, errors.New("disabled")
	})})
	output, err := other.Get(ctx, "123", 1, "999")
	if err != nil || strings.Count(string(output), "<d p=") != 120 {
		t.Fatal("disabled fallback failed")
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected upstream request count")
	}
}

func TestHongGuoDanmuTargetVisibility(t *testing.T) {
	repos, _, _ := newDanmuTestServices(t)
	for _, value := range []any{
		&model.User{Base: model.Base{ID: "viewer"}, Username: "viewer", Role: "admin", IsActive: true},
		&model.Library{Base: model.Base{ID: "library"}, Name: "红果", Path: "/test", Type: model.LibraryTypeHongGuo},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work"}, SourceID: "123", Kind: "series", Title: "测试"},
		&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: "ep"}, WorkID: "work", Number: 7, SourceVideoID: "456"},
		&model.Media{PermanentBase: model.PermanentBase{ID: "file"}, LibraryID: "library", CatalogSource: "hongguo", LookupCatalogID: "123", Path: "/test/file.strm"},
	} {
		if err := repos.DB.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	ep := "ep"
	if err := repos.DB.Create(&model.HongGuoMediaBinding{MediaID: "file", WorkID: "work", EpisodeID: &ep}).Error; err != nil {
		t.Fatal(err)
	}
	emby := NewEmbyService(&config.Config{}, zap.NewNop(), repos)
	emby.visibilityCache = map[string]embyVisibilityCacheEntry{
		emby.repo.ReadCacheKey() + "locked":        {visibility: MediaVisibility{LibraryRestricted: true}, expiresAt: time.Now().Add(time.Hour)},
		emby.repo.ReadCacheKey() + "hidden":        {visibility: MediaVisibility{HiddenLibraryIDs: []string{"library"}}, expiresAt: time.Now().Add(time.Hour)},
		emby.repo.ReadCacheKey() + "other-library": {visibility: MediaVisibility{AllowedLibraryIDs: []string{"other"}}, expiresAt: time.Now().Add(time.Hour)},
	}
	for _, user := range []string{"locked", "hidden", "other-library"} {
		for _, id := range []string{"file", "hg-episode-ep"} {
			_, _, _, _, err := emby.HongGuoDanmuTarget(t.Context(), user, id)
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("visibility %s %s: %v", user, id, err)
			}
		}
	}
	for _, id := range []string{"file", "hg-episode-ep"} {
		source, num, video, matched, err := emby.HongGuoDanmuTarget(t.Context(), "viewer", id)
		if err != nil || !matched || source != "123" || num != 7 || video != "456" {
			t.Fatalf("target %s: %s %d %s %t %v", id, source, num, video, matched, err)
		}
	}
	for _, id := range []string{"hg-work-work", "hg-group-album", "hg-season-work", "hg-person-person", "hg-episode-missing"} {
		_, _, _, _, err := emby.HongGuoDanmuTarget(t.Context(), "viewer", id)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("container %s: %v", id, err)
		}
	}
	_, _, _, matched, err := emby.HongGuoDanmuTarget(t.Context(), "viewer", "ordinary")
	if matched || err != nil {
		t.Fatal("ordinary changed")
	}
	// 无可见文件时，即使弹幕已存在也不能定位读取。
	if err := repos.DB.Delete(&model.HongGuoMediaBinding{}, "media_id = ?", "file").Error; err != nil {
		t.Fatal(err)
	}
	_, _, _, _, err = emby.HongGuoDanmuTarget(context.Background(), "viewer", "hg-episode-ep")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal(fmt.Sprint("missing file: ", err))
	}
}

func TestHongGuoDanmuSaveFailureAndFallback(t *testing.T) {
	repos, cfg, svc := newDanmuTestServices(t)
	history := []model.HongGuoDanmu{{SourceID: "123", EpisodeNumber: 1, CommentID: "1", OffsetMS: 5000, Content: "history"}}
	if err := repos.HongGuo.InsertDanmus(t.Context(), history); err != nil {
		t.Fatal(err)
	}
	var mode string
	var calls int
	svc.client = hongguo.NewClient(&http.Client{Transport: danmuTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if mode == "failure" {
			return nil, errors.New("simulated network failure")
		}
		body := `{"code":0,"data":{"video_model":{"video_duration":2}}}`
		if strings.Contains(r.URL.Path, "commentapi") {
			body = `{"code":0,"data":{"data_list":[],"extra":{"next_query_danmaku_list_time":30000}}}`
			if mode == "new" {
				body = `{"code":0,"data":{"data_list":[{"comment":{"comment_id":"2","common":{"content":{"text":"live"}},"expand":{"offset_time":1000}}}],"extra":{"next_query_danmaku_list_time":30000}}}`
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	for _, state := range []string{"failure", "empty"} {
		mode = state
		data, err := svc.Get(t.Context(), "123", 1, "456")
		if err != nil || !strings.Contains(string(data), ">history</d>") {
			t.Fatal("history lost on upstream failure/empty")
		}
	}
	off := false
	if _, err := cfg.Update(t.Context(), "hongguo", APIConfigPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err := svc.Get(t.Context(), "123", 1, "456"); err != nil || calls != before {
		t.Fatal("next request did not observe disable")
	}
	on := true
	if _, err := cfg.Update(t.Context(), "hongguo", APIConfigPatch{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	mode = "new"
	if err := repos.DB.Callback().Create().Before("gorm:create").Register("danmu_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "hongguo_danmus" {
			tx.AddError(errors.New("save failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer repos.DB.Callback().Create().Remove("danmu_fail")
	data, err := svc.Get(t.Context(), "123", 1, "456")
	if err != nil || strings.Count(string(data), "<d p=") != 2 {
		t.Fatal("save failure changed response")
	}
	svc.Close()
	rows, err := repos.HongGuo.Danmus(t.Context(), "123", 1)
	if err != nil || len(rows) != 1 {
		t.Fatal("save failure changed history")
	}
}
