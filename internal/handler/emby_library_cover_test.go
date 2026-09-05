package handler

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestEmbyLibraryCover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	media := service.NewMediaService(cfg, zap.NewNop(), repos)
	emby := service.NewEmbyService(cfg, zap.NewNop(), repos)
	lib := model.Library{Name: "电影", Type: "movie", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo: repos, Media: media, Emby: emby, ImageProxy: service.NewImageProxy(cfg, zap.NewNop()),
	})
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 16, 9))); err != nil {
		t.Fatal(err)
	}
	checkTag := func(hasCover bool) string {
		t.Helper()
		views, err := emby.Views(t.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		items := views["Items"].([]map[string]any)
		if len(items) != 1 {
			t.Fatalf("views count = %d", len(items))
		}
		tag := items[0]["ImageTags"].(map[string]string)["Primary"]
		if (tag != "") != hasCover {
			t.Fatalf("tag = %q, hasCover = %v", tag, hasCover)
		}
		item, err := emby.Item(t.Context(), lib.ID, "")
		if err != nil || item == nil {
			t.Fatalf("library detail: %v", err)
		}
		if item["ImageTags"].(map[string]string)["Primary"] != tag {
			t.Fatal("detail and views image tags differ")
		}
		return tag
	}
	checkImage := func(want []byte) {
		t.Helper()
		for _, path := range []string{
			"/Items/" + lib.ID + "/Images/Primary",
			"/items/" + lib.ID + "/images/primary",
			"/emby/Items/" + lib.ID + "/Images/Primary",
			"/emby/items/" + lib.ID + "/images/primary",
		} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
				if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Location") != "" {
					t.Fatalf("%s %s: status=%d headers=%v", method, path, rec.Code, rec.Header())
				}
				if method == http.MethodGet && !bytes.Equal(rec.Body.Bytes(), want) {
					t.Fatalf("%s: unexpected image", path)
				}
				if method == http.MethodHead && rec.Body.Len() != 0 {
					t.Fatalf("HEAD %s returned a body", path)
				}
			}
		}
	}
	checkTag(false)
	checkImage(embyPlaceholderPNG)
	if _, err := media.SaveLibraryCover(t.Context(), lib.ID, bytes.NewReader(data.Bytes())); err != nil {
		t.Fatal(err)
	}
	firstTag := checkTag(true)
	if checkTag(true) != firstTag {
		t.Fatal("unchanged cover tag is unstable")
	}
	checkImage(data.Bytes())
	if raw, err := emby.ImageURL(t.Context(), lib.ID, "Backdrop"); err != nil || raw != "" {
		t.Fatalf("library backdrop = %q, %v", raw, err)
	}
	data.Reset()
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 32, 18))); err != nil {
		t.Fatal(err)
	}
	if _, err := media.SaveLibraryCover(t.Context(), lib.ID, bytes.NewReader(data.Bytes())); err != nil {
		t.Fatal(err)
	}
	if checkTag(true) == firstTag {
		t.Fatal("replacing cover did not change tag")
	}
	checkImage(data.Bytes())
	if _, err := media.ClearLibraryCover(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	checkTag(false)
	checkImage(embyPlaceholderPNG)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data.Bytes())
	}))
	defer upstream.Close()
	if err := media.UpdateLibraryCover(t.Context(), lib.ID, strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)+"/cover.png"); err != nil {
		t.Fatal(err)
	}
	checkTag(true)
	checkImage(data.Bytes())
}
