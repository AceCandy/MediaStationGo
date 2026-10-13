package handler

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestExtensionRoutesAuthCacheAndMatching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	create := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	update := func(v any, values map[string]any) {
		t.Helper()
		if err := db.Model(v).Updates(values).Error; err != nil {
			t.Fatal(err)
		}
	}
	user := model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "admin", Tier: "plus", IsActive: true}
	create(&user)
	lib := model.Library{Base: model.Base{ID: "library"}, Name: "Movies", Path: "/test", Enabled: true}
	create(&lib)
	work := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "movie"}, Kind: "movie", Title: "Movie", Source: "tmdb"}
	create(&work)
	create(&model.MetadataIdentifier{MetadataID: work.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: "101"})
	create(&model.Media{PermanentBase: model.PermanentBase{ID: "file"}, LibraryID: lib.ID, MetadataID: work.ID, Path: "/test/movie"})
	profile := model.PlayProfile{Base: model.Base{ID: "locked-profile"}, UserID: user.ID, Name: "PIN", IsDefault: true, RequirePIN: true, AllowedLibraryIDs: `["library"]`}
	create(&profile)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/trending/movie/day" {
			http.Error(w, "unexpected upstream", 500)
			return
		}
		if r.URL.Query().Get("page") == "2" {
			http.Error(w, "private-upstream-detail", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":101,"title":"First","poster_path":"/first.jpg"},{"id":202,"title":"Second"}]}`))
	}))
	defer upstream.Close()
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}, Secrets: config.SecretsConfig{JWTSecret: "test-secret", TMDbAPIKey: "test-key", TMDbAPIProxy: upstream.URL}}
	repos := repository.New(db)
	svc := service.New(cfg, zap.NewNop(), repos)
	t.Cleanup(svc.Close)
	// 预置真实持久周历，HTTP 只读测试不访问 Bangumi。
	calendarItems := []service.CalendarEntry{{ExternalMediaResult: service.ExternalMediaResult{Source: "bangumi", MediaType: "anime", BangumiID: 101, Title: "Monday"}, Weekday: 1, Timezone: "Asia/Tokyo"}, {ExternalMediaResult: service.ExternalMediaResult{Source: "bangumi", MediaType: "anime", BangumiID: 102, Title: "Tuesday"}, Weekday: 2, Timezone: "Asia/Tokyo"}}
	raw, _ := json.Marshal(calendarItems)
	if err := repos.BangumiCalendar.Save(t.Context(), model.BangumiCalendarSnapshot{Payload: string(raw), FetchedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", svc)
	token := signedTestToken(t, "test-secret")
	request := func(method, path, body, carrier string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/emby-ext/v1"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		switch carrier {
		case "header":
			req.Header.Set("X-Emby-Token", token)
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+token)
		case "media":
			req.Header.Set("Authorization", `MediaBrowser Token="`+token+`"`)
		case "query":
			q := req.URL.Query()
			q.Set("api_key", token)
			req.URL.RawQuery = q.Encode()
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	check := func(out *httptest.ResponseRecorder, status int) {
		t.Helper()
		if out.Code != status {
			t.Fatalf("status=%d want=%d body=%s", out.Code, status, out.Body.String())
		}
	}
	check(request("GET", "/capabilities", "", "", nil), 401)
	for _, carrier := range []string{"header", "bearer", "media", "query"} {
		check(request("GET", "/capabilities", "", carrier, nil), 200)
	}
	for _, path := range []string{"/rankings", "/calendar", "/calendar?weekday=1"} {
		check(request("GET", path, "", "header", nil), 200)
	}
	cal := request("GET", "/calendar?weekday=1", "", "header", nil)
	if !strings.Contains(cal.Body.String(), "Monday") || strings.Contains(cal.Body.String(), "Tuesday") || !strings.Contains(cal.Body.String(), `"episode":null`) {
		t.Fatal(cal.Body.String())
	}
	for _, path := range []string{"/calendar?weekday=0", "/calendar?weekday=8", "/rankings/tmdb_trending_day/items?page=0", "/rankings/tmdb_trending_day/items?page=501"} {
		check(request("GET", path, "", "header", nil), 400)
	}
	check(request("GET", "/rankings/unknown/items", "", "header", nil), 404)
	for i := 0; i < 2; i++ {
		out := request("GET", "/rankings/tmdb_trending_day/items", "", "header", nil)
		check(out, 200)
		var result struct {
			Items []service.ExternalMediaResult `json:"items"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Items) != 2 || result.Items[0].TMDbID != 101 || result.Items[1].TMDbID != 202 || result.Items[0].PosterURL != "/emby-ext/v1/images/tmdb/movie/101/poster" {
			t.Fatalf("bad ranking %s", out.Body.String())
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("ranking did not reuse center: %d", requests.Load())
	}
	fail := request("GET", "/rankings/tmdb_trending_day/items?page=2", "", "header", nil)
	check(fail, 502)
	if strings.Contains(fail.Body.String(), "private-upstream-detail") {
		t.Fatal("upstream details leaked")
	}
	check(request("GET", "/images/tmdb/movie/101/poster", "", "", nil), 401)
	check(request("GET", "/images/tmdb/movie/0/poster", "", "header", nil), 400)

	// 使用资料中心已存图片，验证播放器鉴权和图片直出，无需外网。
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	key := "catalogs/tmdb/artwork/aa/bb/cc/aabbcc" + strings.Repeat("0", 58) + ".png"
	path := filepath.Join(cfg.App.DataDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pixels.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "aabbcc" + strings.Repeat("0", 58), StorageKey: key, MimeType: "image/png", Width: 2, Height: 3, SizeBytes: int64(pixels.Len())}
	create(&asset)
	subject, err := repos.TMDb.SubjectIdentity(t.Context(), "movie", 101)
	if err != nil {
		t.Fatal(err)
	}
	artwork, err := repos.TMDb.Artwork(t.Context(), subject.ID, "poster")
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.TMDb.SaveArtwork(t.Context(), artwork, &asset.ID, false); err != nil {
		t.Fatal(err)
	}
	imageResponse := request("GET", "/images/tmdb/movie/101/poster", "", "header", nil)
	check(imageResponse, 200)
	if !strings.HasPrefix(imageResponse.Header().Get("Content-Type"), "image/") || imageResponse.Body.Len() == 0 {
		t.Fatal("missing image bytes")
	}
	head := request("HEAD", "/images/tmdb/movie/101/poster", "", "header", nil)
	check(head, 200)
	if head.Body.Len() != 0 {
		t.Fatal("HEAD returned image body")
	}

	body := `{"items":[{"key":"movie","source":"tmdb","external_id":"101","media_type":"movie"},{"key":"missing","source":"tmdb","external_id":"999","media_type":"movie"}]}`
	locked := request("POST", "/library/matches", body, "header", nil)
	check(locked, 200)
	if strings.Contains(locked.Body.String(), "item_id") {
		t.Fatal("PIN-locked match leaked")
	}
	unlocked := request("POST", "/library/matches", body, "header", map[string]string{"X-Play-Profile-PIN-Token": signPlayProfilePINToken(svc, user.ID, profile.ID, time.Now().Add(time.Hour))})
	check(unlocked, 200)
	if !strings.Contains(unlocked.Body.String(), `"item_id":"movie"`) || strings.Contains(unlocked.Body.String(), "/test/") {
		t.Fatal(unlocked.Body.String())
	}
	for _, body := range []string{`{"items":[]}`, `{"items":[{"key":"x","source":"tmdb","external_id":"1","media_type":"movie"},{"key":"x","source":"tmdb","external_id":"2","media_type":"movie"}]}`, `{"items":[{"key":"x","source":"tmdb","external_id":"1","media_type":"tv","episode":1}]}`, strings.Repeat("x", 65*1024), body + strings.Repeat(" ", 65*1024)} {
		check(request("POST", "/library/matches", body, "header", nil), 400)
	}
	update(&profile, map[string]any{"allowed_library_ids": "[]"})
	emptyAllowed := request("POST", "/library/matches", body, "header", map[string]string{"X-Play-Profile-PIN-Token": signPlayProfilePINToken(svc, user.ID, profile.ID, time.Now().Add(time.Hour))})
	check(emptyAllowed, 200)
	if strings.Contains(emptyAllowed.Body.String(), "item_id") {
		t.Fatal("empty allowed list treated as unrestricted")
	}
	discover := svc.Discover
	svc.Discover = nil
	check(request("GET", "/rankings/tmdb_trending_day/items", "", "header", nil), 503)
	svc.Discover = discover
	// 已签发的 admin claim 不能越过当前角色、权限与撤销状态。
	update(&user, map[string]any{"role": "user"})
	limited := request("GET", "/capabilities", "", "header", nil)
	check(limited, 200)
	if !strings.Contains(limited.Body.String(), `"rankings":false`) || !strings.Contains(limited.Body.String(), `"format":"bilibili_xml"`) {
		t.Fatal(limited.Body.String())
	}
	check(request("GET", "/rankings", "", "header", nil), 403)
	if err := svc.Permissions.Save(t.Context(), user.ID, &model.UserPermission{CanViewDiscover: true}); err != nil {
		t.Fatal(err)
	}
	check(request("GET", "/capabilities", "", "header", nil), 200)
	update(&user, map[string]any{"token_version": 1})
	check(request("GET", "/capabilities", "", "header", nil), 401)
	update(&user, map[string]any{"token_version": 0, "is_active": false})
	check(request("GET", "/capabilities", "", "header", nil), 403)
}
