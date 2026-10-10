package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestEmbyResponseCacheUsesCurrentRequestToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	user := &model.User{Base: model.Base{ID: "user-1"}, Username: "cache-viewer", Role: "admin", Tier: "plus", IsActive: true}
	if err := repos.User.Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	lib := &model.Library{Name: "Cache", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), lib); err != nil {
		t.Fatal(err)
	}
	media := &model.Media{PermanentBase: model.PermanentBase{ID: "cache-file"}, LibraryID: lib.ID, Title: "Movie", Path: filepath.Join(lib.Path, "movie.strm"), STRMURL: "https://example.invalid/movie.mkv", Container: "mkv"}
	if err := os.WriteFile(filepath.Join(lib.Path, "movie.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nSubtitle\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(media).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.PlaybackHistory{UserID: user.ID, MediaID: media.ID, MetadataID: media.MetadataID, PositionMs: 60000, DurationMs: 120000, WatchedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	cache := service.NewRuntimeCacheService(nil, nil)
	emby := service.NewEmbyService(&config.Config{}, zap.NewNop(), repos).SetRuntimeCache(cache)
	emby.SetSubtitle(service.NewSubtitleService(zap.NewNop(), repos))
	router := gin.New()
	const secret = "cache-test-secret"
	registerEmbyRoutes(router, secret, &service.Container{Repo: repos, Emby: emby})
	tokenizedResponses := 0
	for _, endpoint := range []string{
		"/emby/Users/user-1/Items?IncludeItemTypes=Movie&Recursive=true&Limit=5",
		"/emby/Users/user-1/Items/cache-file",
		"/emby/Users/user-1/Items/Resume?Limit=5",
	} {
		for _, requestID := range []string{"first", "second"} {
			claims := middleware.Claims{UserID: user.ID, Role: "admin", Tier: "plus", RegisteredClaims: jwt.RegisteredClaims{ID: requestID, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			req.Header.Set("X-Emby-Token", token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("%s status=%d", endpoint, w.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			item := body
			if items, list := body["Items"].([]any); list {
				if len(items) != 1 {
					t.Fatalf("%s unexpected item count %d", endpoint, len(items))
				}
				item = items[0].(map[string]any)
			}
			source := item["MediaSources"].([]any)[0].(map[string]any)
			if raw, exists := source["DirectStreamUrl"].(string); exists {
				stream, err := url.Parse(raw)
				if err != nil || stream.Query().Get("api_key") != token {
					t.Fatalf("%s did not use current request credential", endpoint)
				}
				tokenizedResponses++
			}
			if streams, ok := source["MediaStreams"].([]any); ok {
				for _, value := range streams {
					if raw, ok := value.(map[string]any)["DeliveryUrl"].(string); ok {
						delivery, err := url.Parse(raw)
						if err != nil || delivery.Query().Get("api_key") != token {
							t.Fatal("subtitle did not use current request credential")
						}
						tokenizedResponses++
					}
				}
			}
			if source["Path"] != "/Videos/cache-file/stream.mkv" {
				t.Fatal("remote Path exposed source or credential")
			}
		}
	}
	if tokenizedResponses < 2 {
		t.Fatal("did not exercise both cold and warm credential injection")
	}
	item, err := emby.CachedItem(service.WithAuthenticatedUser(t.Context(), user), media.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(item)
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	source := snapshot["MediaSources"].([]any)[0].(map[string]any)
	rawURL, _ := source["DirectStreamUrl"].(string)
	stream, _ := url.Parse(rawURL)
	for _, value := range source["MediaStreams"].([]any) {
		if raw, ok := value.(map[string]any)["DeliveryUrl"].(string); ok {
			delivery, _ := url.Parse(raw)
			if delivery.Query().Has("api_key") {
				t.Fatal("HTTP token mutated subtitle snapshot")
			}
		}
	}
	if stream.Query().Has("api_key") {
		t.Fatal("HTTP token mutated cached snapshot")
	}
}
