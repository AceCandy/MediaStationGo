package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestEmbyDanmuRawRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		&model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "admin", Tier: "plus", IsActive: true},
		&model.Library{Base: model.Base{ID: "library"}, Name: "红果", Path: "/test", Type: model.LibraryTypeHongGuo},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work"}, SourceID: "123", Kind: "series", Title: "测试"},
		&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: "ep"}, WorkID: "work", Number: 1},
		&model.Media{PermanentBase: model.PermanentBase{ID: "file"}, LibraryID: "library", CatalogSource: "hongguo", LookupCatalogID: "123", Path: "/test/file.strm"},
		&model.HongGuoDanmu{SourceID: "123", EpisodeNumber: 1, CommentID: "9999999999999999999", OffsetMS: 1234, Content: "测试<&"},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	ep := "ep"
	if err := db.Create(&model.HongGuoMediaBinding{MediaID: "file", WorkID: "work", EpisodeID: &ep}).Error; err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := service.NewAPIConfigService(zap.NewNop(), repos, service.NewCryptoService("test-secret", zap.NewNop()))
	off := false
	if _, err := cfg.Update(t.Context(), "hongguo", service.APIConfigPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	danmu := service.NewHongGuoDanmuService(repos.HongGuo, cfg, zap.NewNop())
	defer danmu.Close()
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{Repo: repos, Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos), HongGuoDanmu: danmu})
	reads := 0
	if err := db.Callback().Query().Before("gorm:query").Register("count_danmu", func(tx *gorm.DB) {
		if tx.Statement.Table == "hongguo_danmus" {
			reads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("count_danmu")
	for _, prefix := range []string{"", "/emby"} {
		for _, test := range []struct {
			id     string
			auth   bool
			status int
			xml    bool
		}{
			{"hg-episode-ep", true, 200, true}, {"file", true, 200, true}, {"hg-episode-ep", false, 401, false}, {"hg-episode-missing", true, 404, false}, {"hg-group-album", true, 404, false}, {"media-demo", true, 200, false},
		} {
			before := reads
			req := httptest.NewRequest(http.MethodGet, prefix+"/api/danmu/"+test.id+"/raw", nil)
			if test.auth {
				req.Header.Set("X-Emby-Token", signedTestToken(t, "test-secret"))
			}
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != test.status {
				t.Fatalf("%s: %d %s", test.id, out.Code, out.Body.String())
			}
			if test.xml {
				if out.Header().Get("Cache-Control") != "no-store" || !strings.Contains(out.Header().Get("Content-Type"), "application/xml") || !strings.Contains(out.Body.String(), "1.234,1,25,16777215,0,0,0,9999999999999999999,0") {
					t.Fatalf("xml contract: %s", out.Body.String())
				}
			} else if reads != before {
				t.Fatal("read danmu before permission / for non-hongguo")
			}
			if test.id == "media-demo" && out.Body.Len() != 0 {
				t.Fatal("non-hongguo changed")
			}
		}
	}
}
