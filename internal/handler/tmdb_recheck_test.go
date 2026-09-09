package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestTMDbRecheckListAccessAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Media{}, &model.TMDbRecheckJob{}, &model.TMDbRecheckChange{}, &model.TMDbRecheckAssetChange{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TMDbRecheckJob{MetadataID: "test-job", Status: "done", LeaseToken: "private-lease"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{Repo: repository.New(db)}
	for _, tc := range []struct {
		role, path string
		want       int
	}{
		{"", "tmdb_episode_metadata_recheck/pending", 401},
		{"user", "tmdb_episode_metadata_recheck/pending", 403},
		{"admin", "unknown/pending", 404},
		{"admin", "tmdb_episode_metadata_recheck/pending?page=no", 400},
		{"admin", "tmdb_episode_metadata_recheck/pending?page=0", 400},
		{"admin", "tmdb_episode_metadata_recheck/pending?page_size=101", 400},
		{"admin", "tmdb_episode_metadata_recheck/pending?status=invalid", 400},
		{"admin", "tmdb_episode_metadata_recheck/pending?status=done", 200},
		{"admin", "tmdb_episode_metadata_recheck/pending?status=done&page=2", 200},
		{"admin", "tmdb_episode_metadata_recheck/pending?status=not_found", 200},
		{"", "tmdb_episode_metadata_recheck/pending/job/files", 401},
		{"user", "tmdb_episode_metadata_recheck/pending/job/files", 403},
		{"admin", "unknown/pending/job/files", 404},
		{"admin", "tmdb_episode_metadata_recheck/pending/job/files?page=0", 400},
		{"admin", "tmdb_episode_metadata_recheck/pending/job/files?page_size=101", 400},
		{"admin", "tmdb_episode_metadata_recheck/pending/job/files", 200},
	} {
		t.Run(tc.role+tc.path, func(t *testing.T) {
			router := gin.New()
			group := router.Group("/api", middleware.AuthRequired("recheck-test-secret"))
			registerAuthedStatsDiscoveryAndAIRoutes(group, svc)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/tasks/definitions/"+tc.path, nil)
			if tc.role != "" {
				req.Header.Set("Authorization", "Bearer "+signedProbeRoleToken(t, "recheck-test-secret", tc.role))
			}
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private-lease") || strings.Contains(rec.Body.String(), "lease_token") {
				t.Fatal("lease exposed")
			}
			if tc.want == 200 && strings.Contains(tc.path, "page=2") && !strings.Contains(rec.Body.String(), `"items":[]`) {
				t.Fatalf("pagination=%s", rec.Body.String())
			}
		})
	}
}

func TestTMDbRecheckFilesVersionsAndPagination(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Media{}, &model.TMDbRecheckJob{}); err != nil {
		t.Fatal(err)
	}
	series := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "series"}, Kind: "series"}
	season := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "season"}, Kind: "season", ParentID: &series.ID}
	episode := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "episode"}, ParentID: &season.ID, Kind: "episode", EpisodeNum: 1}
	wrongKind := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "wrong-kind"}, ParentID: &season.ID, Kind: "season"}
	for _, row := range []*model.MetadataItem{&series, &season, &episode, &wrongKind} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []model.Media{
		{PermanentBase: model.PermanentBase{ID: "a"}, MetadataID: season.ID, Path: "/local/season.strm"},
		{PermanentBase: model.PermanentBase{ID: "b"}, MetadataID: episode.ID, Path: "/local/episode-v1.strm"},
		{PermanentBase: model.PermanentBase{ID: "c"}, MetadataID: episode.ID, Path: "/local/episode-v2.mkv"},
		{PermanentBase: model.PermanentBase{ID: "d"}, MetadataID: episode.ID, Path: "https://secret.invalid/video?token=hidden"},
		{PermanentBase: model.PermanentBase{ID: "e"}, MetadataID: wrongKind.ID, Path: "/local/not-child.strm"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{season.ID, episode.ID} {
		if err := db.Create(&model.TMDbRecheckJob{MetadataID: id, Status: "not_found"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	registerAuthedStatsDiscoveryAndAIRoutes(router.Group("/api", middleware.AuthRequired("files-test")), &service.Container{Repo: repository.New(db)})
	get := func(id, page string) repository.TMDbRecheckFilesPage {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/tasks/definitions/tmdb_episode_metadata_recheck/pending/"+id+"/files?page_size=2&page="+page, nil)
		req.Header.Set("Authorization", "Bearer "+signedProbeRoleToken(t, "files-test", "admin"))
		router.ServeHTTP(rec, req)
		if rec.Code != 200 || strings.Contains(rec.Body.String(), "hidden") || strings.Contains(rec.Body.String(), "secret.invalid") {
			t.Fatalf("unsafe response %d %s", rec.Code, rec.Body.String())
		}
		var out repository.TMDbRecheckFilesPage
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first, second := get(season.ID, "1"), get(season.ID, "2")
	if len(first.Items) != 2 || !first.HasMore || first.Items[0].MediaID != "a" || first.Items[1].MediaID != "b" || !first.Items[1].CanPreview || len(second.Items) != 2 || second.HasMore || second.Items[0].CanPreview || second.Items[1].CanPreview {
		t.Fatalf("season pages %+v %+v", first, second)
	}
	if ep := get(episode.ID, "1"); len(ep.Items) != 2 || ep.Items[0].MediaID != "b" || !ep.HasMore {
		t.Fatalf("episode versions %+v", ep)
	}
	if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id=?", episode.ID).Update("status", "pending").Error; err != nil {
		t.Fatal(err)
	}
	if ep := get(episode.ID, "1"); len(ep.Items) != 0 {
		t.Fatalf("non-404 files %+v", ep)
	}
}
