package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestEmbyLibraryDisplayPersistenceAndEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Setting{}, &model.User{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	cfg := &config.Config{}
	svc := &service.Container{Repo: repo, Cfg: cfg, Emby: service.NewEmbyService(cfg, zap.NewNop(), repo)}
	for _, id := range []string{"a", "b", "c"} {
		lib := model.Library{Name: id, Path: "/test/" + id, Type: "movie", Enabled: true}
		lib.ID = id
		if err := db.Create(&lib).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.CtxUserRole, c.GetHeader("Test-Role"))
		c.Set(middleware.CtxUserID, c.GetHeader("Test-User"))
		c.Next()
	})
	router.PUT("/settings", middleware.AdminRequired(), updateSettingHandler(svc))
	router.GET("/folders", embyVirtualFoldersHandler(svc))
	router.GET("/views", embyViewsHandler(svc))
	write := func(value, role string, wantStatus int) {
		t.Helper()
		body, _ := json.Marshal(settingReq{Key: service.EmbyLibraryDisplaySettingKey, Value: value})
		req := httptest.NewRequest(http.MethodPut, "/settings", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Test-Role", role)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != wantStatus {
			t.Fatalf("setting status %d: %s", res.Code, res.Body.String())
		}
	}
	value := `[{"id":"b","hidden":false},{"id":"a","hidden":true}]`
	write(value, "user", http.StatusForbidden)
	write(`[{"id":"missing","hidden":false}]`, "admin", http.StatusBadRequest)
	write(`[{"id":"a","hidden":false},{"id":"a","hidden":true}]`, "admin", http.StatusBadRequest)
	write(value, "admin", http.StatusNoContent)
	stored, err := repo.Setting.Get(t.Context(), service.EmbyLibraryDisplaySettingKey)
	if err != nil || stored != value {
		t.Fatalf("setting not persisted: %q, %v", stored, err)
	}
	// 新实例必须从数据库读取，而非依赖旧实例的运行时状态。
	svc.Emby = service.NewEmbyService(cfg, zap.NewNop(), repository.New(db))
	check := func(user string, want []string) {
		t.Helper()
		for _, path := range []string{"/folders", "/views"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Test-User", user)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, res.Code, res.Body.String())
			}
			var items []map[string]any
			if path == "/views" {
				var envelope struct {
					Items            []map[string]any
					TotalRecordCount int
				}
				if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				items = envelope.Items
				if envelope.TotalRecordCount != len(want) {
					t.Fatal("incorrect count")
				}
			} else if err := json.Unmarshal(res.Body.Bytes(), &items); err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(items))
			for _, item := range items {
				got = append(got, item["Id"].(string))
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s got %v, want %v", path, got, want)
			}
		}
		root, err := svc.Emby.Items(t.Context(), service.ItemsParams{UserID: user})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0)
		for _, item := range root["Items"].([]map[string]any) {
			got = append(got, item["Id"].(string))
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("root got %v, want %v", got, want)
		}
	}
	check("", []string{"b", "c"})
	user := model.User{Username: "viewer", PasswordHash: "test", Role: "user"}
	user.ID = "viewer"
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&user).Update("hide_adult", false).Error; err != nil {
		t.Fatal(err)
	}
	profile := model.PlayProfile{UserID: user.ID, Name: "default", IsDefault: true, AllowAdult: true, AllowedLibraryIDs: `["c"]`}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	check(user.ID, []string{"c"})
	write(`[{"id":"a","hidden":true},{"id":"b","hidden":true},{"id":"c","hidden":true}]`, "admin", http.StatusNoContent)
	check("", []string{})
	write("", "admin", http.StatusNoContent)
	check("", []string{"a", "b", "c"})
}
