package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoHTTPAccessAndStateIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	ctx := t.Context()
	library := model.Library{Name: "来源测试", Path: t.TempDir(), Type: model.LibraryTypeHongGuo}
	profile := model.PlayProfile{UserID: "user-1", Name: "无媒体库权限", AllowedLibraryIDs: `[]`, RequirePIN: true}
	for _, row := range []any{&library, &profile} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.HongGuo.SaveDiscoveryPage(ctx, []hongguo.Work{{SourceID: "9000000000000000001", Title: "HTTP 测试"}}, model.HongGuoSyncState{Category: "real-drama", NextPage: 1}); err != nil {
		t.Fatal(err)
	}
	work, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "9000000000000000001", Title: "HTTP 测试", Tags: []string{"都市"}, EpisodeCount: 1, Completed: true, TotalEpisodes: 1, CoverURL: "https://example.com/private-image?signature=fixture", Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.HongGuo.ReplaceRank(ctx, "hot-drama", "", []hongguo.Work{{SourceID: work.SourceID, Title: work.Title}}); err != nil {
		t.Fatal(err)
	}
	m := model.Media{LibraryID: library.ID, Path: filepath.Join(library.Path, "movie.mp4"), CatalogSource: model.TaskSystemHongGuo, LookupCatalogID: work.SourceID}
	if err := os.WriteFile(m.Path, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.Upsert(ctx, &m); err != nil {
		t.Fatal(err)
	}
	if err := repos.HongGuo.SetFavorite(ctx, "other-user", work.SourceID, true); err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{Repo: repos, Cfg: &config.Config{}}
	svc.Permissions = service.NewPermissionService(zap.NewNop(), repos)
	if err := db.Create(&model.User{Base: model.Base{ID: "user-1"}, Username: "hongguo-viewer", Role: "user"}).Error; err != nil {
		t.Fatal(err)
	}
	svc.Media = service.NewMediaService(svc.Cfg, zap.NewNop(), repos)
	svc.Stream = service.NewStreamService(svc.Cfg, zap.NewNop(), repos)
	svc.HongGuo = service.NewHongGuoService(repos, service.NewTaskTrackerService(zap.NewNop(), nil), nil, t.TempDir())
	t.Cleanup(svc.HongGuo.Wait)
	router := gin.New()
	registerAdminRoutes(router.Group("/api"), &config.Config{Secrets: config.SecretsConfig{JWTSecret: "hongguo-http-test"}}, svc)
	registerHongGuoRoutes(router.Group("/api", middleware.AuthRequired("hongguo-http-test")), svc)
	router.GET("/api/stream/:id", middleware.AuthRequired("hongguo-http-test"), streamHandler(svc))
	streamRequest := func(profileID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/stream/"+m.ID, nil)
		req.Header.Set("Authorization", "Bearer "+signedProbeRoleToken(t, "hongguo-http-test", "user"))
		req.Header.Set("Range", "bytes=2-5")
		if profileID != "" {
			req.Header.Set("X-Play-Profile-ID", profileID)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := streamRequest(""); rec.Code != 206 || rec.Body.String() != "2345" {
		t.Fatalf("source local range: %d %q", rec.Code, rec.Body.String())
	}
	if rec := streamRequest(profile.ID); rec.Code != 404 {
		t.Fatalf("locked source stream: %d", rec.Code)
	}
	if err := db.Model(&m).Update("strm_url", "https://example.com/local-download.mp4").Error; err != nil {
		t.Fatal(err)
	}
	if rec := streamRequest(""); rec.Code != 302 || rec.Header().Get("Location") != "https://example.com/local-download.mp4" {
		t.Fatalf("source STRM redirect: %d", rec.Code)
	}
	request := func(method, path, role, body, profileID string) *httptest.ResponseRecorder {
		if role != "" {
			if err := repos.User.UpdateFields(ctx, "user-1", map[string]any{"role": role}); err != nil {
				t.Fatal(err)
			}
		}
		url := "/api/catalogs/hongguo" + path
		if strings.HasPrefix(path, "/admin/") {
			url = "/api" + path
		}
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		if role != "" {
			req.Header.Set("Authorization", "Bearer "+signedProbeRoleToken(t, "hongguo-http-test", role))
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if profileID != "" {
			req.Header.Set("X-Play-Profile-ID", profileID)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "signature") || strings.Contains(rec.Body.String(), "source_url") || strings.Contains(rec.Body.String(), "snapshot") {
			t.Fatal("private source fields exposed")
		}
		return rec
	}
	for _, path := range []string{"/works", "/search?keyword=test", "/works/" + work.SourceID, "/works/" + work.SourceID + "/episodes", "/groups/unknown"} {
		if rec := request("GET", path, "user", "", ""); rec.Code != 403 {
			t.Fatalf("discover permission bypass: %s %d", path, rec.Code)
		}
	}
	if rec := request("GET", "/works", "admin", "", ""); rec.Code != 200 {
		t.Fatalf("admin catalog access: %d", rec.Code)
	}
	if rec := request("GET", "/works?source_category=real-drama&category=都市", "admin", "", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"total":1`) {
		t.Fatalf("category catalog: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request("GET", "/works?source_category=real-drama", "admin", "", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"total":1`) {
		t.Fatalf("source category catalog: %d %s", rec.Code, rec.Body.String())
	}
	for _, target := range []any{&model.HongGuoDiscovery{}, &model.HongGuoWork{}} {
		if err := db.Model(target).Where("source_id = ?", work.SourceID).Update("source_category", "").Error; err != nil {
			t.Fatal(err)
		}
	}
	if rec := request("GET", "/works?source_category=other", "admin", "", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"total":1`) {
		t.Fatalf("other category catalog: %d %s", rec.Code, rec.Body.String())
	}
	if rec := request("PUT", "/works/"+work.SourceID+"/category", "user", `{"source_category":"real-drama"}`, ""); rec.Code != 403 {
		t.Fatalf("viewer changed category: %d", rec.Code)
	}
	for _, tc := range []struct {
		id, body string
		status   int
	}{
		{"invalid", `{"source_category":"real-drama"}`, 400},
		{work.SourceID, `{"source_category":"other"}`, 400},
		{"99999", `{"source_category":"ai-drama"}`, 404},
		{work.SourceID, `{"source_category":"real-drama"}`, 204},
	} {
		if rec := request("PUT", "/works/"+tc.id+"/category", "admin", tc.body, ""); rec.Code != tc.status {
			t.Fatalf("set category %s: %d %s", tc.id, rec.Code, rec.Body.String())
		}
	}
	var categoryRows int64
	if err := db.Model(&model.HongGuoDiscovery{}).Where("source_id = ? AND source_category = ?", work.SourceID, "real-drama").Count(&categoryRows).Error; err != nil || categoryRows != 1 {
		t.Fatalf("discovery category not updated: count=%d err=%v", categoryRows, err)
	}
	if rec := request("GET", "/works?rank=hot-drama", "admin", "", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"total":1`) {
		t.Fatalf("official rank catalog: %d %s", rec.Code, rec.Body.String())
	}
	if err := db.Model(&model.UserPermission{}).Where("user_id = ?", "user-1").Update("can_view_discover", true).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, role, body string
		status                   int
	}{
		{"GET", "/works", "", "", 401},
		{"GET", "/search?keyword=test", "", "", 401},
		{"GET", "/search", "user", "", 400},
		{"GET", "/search?keyword=..", "user", "", 400},
		{"GET", "/admin/playback-stats?system=hongguo", "", "", 401},
		{"GET", "/admin/playback-stats?system=hongguo", "user", "", 403},
		{"GET", "/admin/playback-stats?system=hongguo", "admin", "", 200},
		{"GET", "/admin/playback-stats?system=nfo", "admin", "", 200},
		{"GET", "/admin/playback-stats?system=all", "admin", "", 200},
		{"GET", "/admin/playback-stats?system=all", "user", "", 403},
		{"GET", "/admin/playback-stats?system=nfo", "", "", 401},
		{"GET", "/admin/playback-stats?system=other", "admin", "", 400},
		{"GET", "/works?page=0", "user", "", 400},
		{"GET", "/works?sort=unknown", "user", "", 400},
		{"GET", "/works?rank=unknown", "user", "", 400},
		{"GET", "/works?rank=hot-drama&source_category=real-drama", "user", "", 400},
		{"GET", "/works?source_category=unknown", "user", "", 400},
		{"GET", "/works?source_category=other", "user", "", 200},
		{"GET", "/works?source_category=comic", "user", "", 400},
		{"GET", "/works?category=都市", "user", "", 400},
		{"GET", "/works?source_category=comic-drama&category=都市", "user", "", 400},
		{"GET", "/works?source_category=real-drama&category=脑洞", "user", "", 400},
		{"GET", "/works?source_category=comic-drama&category=脑洞", "user", "", 200},
		{"GET", "/works?source_category=ai-drama&category=脑洞", "user", "", 200},
		{"GET", "/works?category=未知", "user", "", 400},
		{"GET", "/works/invalid", "user", "", 400},
		{"GET", "/works/99999", "user", "", 404},
		{"GET", "/works/" + work.SourceID, "user", "", 200},
		{"GET", "/works/" + work.SourceID + "/episodes?page=1", "user", "", 200},
		{"POST", "/works/" + work.SourceID + "/refresh", "user", "", 403},
		{"POST", "/works/invalid/refresh", "admin", "", 400},
		{"POST", "/groups", "user", `{}`, 404},
		{"POST", "/groups", "admin", `{}`, 404},
		{"PUT", "/groups/99999", "admin", `{}`, 404},
		{"DELETE", "/groups/99999", "admin", "", 404},
		{"GET", "/groups/99999", "user", "", 404},
		{"GET", "/groups/not-an-id", "user", "", 400},
		{"PUT", "/status", "user", `{"enabled":false}`, 403},
		{"PUT", "/status", "admin", `{}`, 400},
		{"POST", "/cancel", "user", "", 403},
		{"POST", "/cancel", "admin", "", 204},
		{"GET", "/pending", "user", "", 403},
		{"GET", "/pending", "admin", "", 200},
		{"GET", "/pending?page=0", "admin", "", 400},
		{"PUT", "/media/" + m.ID + "/played", "user", `{"played":true,"user_id":"other-user"}`, 204},
		{"PUT", "/media/" + m.ID + "/played", "user", `{}`, 400},
		{"GET", "/libraries/" + library.ID, "user", "", 200},
		{"PUT", "/works/" + work.SourceID + "/favorite", "user", `{"favorite":true,"user_id":"other-user"}`, 204},
	} {
		t.Run(tc.method+tc.path+tc.role, func(t *testing.T) {
			if rec := request(tc.method, tc.path, tc.role, tc.body, ""); rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	for _, path := range []string{"/works/" + work.SourceID + "/media", "/me?tab=favourites"} {
		rec := request(http.MethodGet, path, "user", "", profile.ID)
		var result struct {
			Items []json.RawMessage `json:"items"`
			Total int               `json:"total"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || len(result.Items) != 0 || result.Total != 0 {
			t.Fatalf("locked profile leaked files: %s", rec.Body.String())
		}
	}
	if rec := request(http.MethodGet, "/libraries/"+library.ID, "user", "", profile.ID); rec.Code != 404 {
		t.Fatalf("locked library: %d", rec.Code)
	}
	if rec := request(http.MethodPut, "/media/"+m.ID+"/played", "user", `{"played":false}`, profile.ID); rec.Code != 404 {
		t.Fatalf("locked profile changed progress: %d", rec.Code)
	}
	state, err := repos.HongGuo.UserState(ctx, "user-1", work.SourceID, 1)
	if err != nil || !state.Completed {
		t.Fatal("locked mutation changed watched state")
	}
	if rec := request(http.MethodPut, "/works/"+work.SourceID+"/favorite", "user", `{"favorite":false,"user_id":"other-user"}`, ""); rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	other, err := repos.HongGuo.UserState(ctx, "other-user", work.SourceID, 0)
	if err != nil || !other.Favorite {
		t.Fatal("request user_id changed another user's favorite")
	}
	if _, err := repos.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "92001", Title: "官方系列", EpisodeCount: 2, Snapshot: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := repos.HongGuo.SaveAlbum(ctx, "92001", hongguo.Album{ID: "99999", Season: 3}); err != nil {
		t.Fatal(err)
	}
	rec := request(http.MethodGet, "/groups/99999", "user", "", "")
	var album repository.HongGuoGroupDetail
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &album) != nil || album.ID != "99999" || len(album.Members) != 1 || album.Members[0].SeasonNumber != 3 {
		t.Fatalf("official album GET: %d %s", rec.Code, rec.Body.String())
	}
}
