package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestEmbyLowercasePlaybackInfoRouteReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		PermanentBase: model.PermanentBase{ID: "media-1"},
		LibraryID:     lib.ID,
		Title:         "Lowercase Playback",
		Path:          filepath.Join(lib.Path, "lowercase-playback.mp4"),
		Container:     "mp4",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/users/user-1/items/media-1/playbackinfo", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode playback info: %v", err)
	}
	if _, ok := body["MediaSources"]; !ok {
		t.Fatalf("missing MediaSources: %#v", body)
	}
	sources, ok := body["MediaSources"].([]any)
	if !ok || len(sources) == 0 {
		t.Fatalf("unexpected MediaSources: %#v", body["MediaSources"])
	}
	source, ok := sources[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected MediaSource: %#v", sources[0])
	}
	directURL, _ := source["DirectStreamUrl"].(string)
	if !strings.Contains(directURL, "api_key=") {
		t.Fatalf("DirectStreamUrl should carry api_key for clients that do not repeat auth headers: %#v", source)
	}
	if _, ok := source["TranscodingUrl"]; ok {
		t.Fatalf("direct-only PlaybackInfo must omit TranscodingUrl: %#v", source)
	}
}

func TestEmbyPlaybackInfoDoesNotExposeTokenInRemotePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "远程电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		PermanentBase: model.PermanentBase{ID: "remote-1"},
		LibraryID:     lib.ID,
		Title:         "Remote Movie",
		Path:          "https://example.invalid/Movies/Movie.mkv",
		STRMURL:       "https://example.invalid/Movies/Movie.mkv",
		Container:     "mkv",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/users/user-1/items/remote-1/playbackinfo", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode playback info: %v", err)
	}
	source := body["MediaSources"].([]any)[0].(map[string]any)
	pathURL, _ := source["Path"].(string)
	if pathURL != "/Videos/remote-1/stream.mkv" {
		t.Fatalf("remote Path should stay as non-tokenized stream URL, got %#v", source)
	}
	if strings.Contains(pathURL, "api_key=") || strings.Contains(pathURL, "token=") {
		t.Fatalf("remote Path must not expose auth key/token: %#v", source)
	}
	directURL, _ := source["DirectStreamUrl"].(string)
	if !strings.HasPrefix(directURL, "/Videos/remote-1/stream.mkv") || !strings.Contains(directURL, "api_key=") {
		t.Fatalf("DirectStreamUrl should stay tokenized: %#v", source)
	}
	if source["SupportsDirectPlay"] != true {
		t.Fatalf("remote media should advertise DirectPlay when tokenized Path is playable: %#v", source)
	}
	if source["SupportsTranscoding"] != false {
		t.Fatalf("remote media should not advertise host transcoding: %#v", source)
	}
}

func TestEmbyItemsDoNotExposeTokenInEmbeddedRemotePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "远程电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		PermanentBase: model.PermanentBase{ID: "remote-1"},
		LibraryID:     lib.ID,
		Title:         "Remote Movie",
		Path:          "https://example.invalid/Movies/Movie.mkv",
		STRMURL:       "https://example.invalid/Movies/Movie.mkv",
		Container:     "mkv",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	token := signedTestToken(t, secret)
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/emby/Users/user-1/Items?IncludeItemTypes=Movie&Recursive=true&Limit=5&X-Emby-Token="+token, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	items := body["Items"].([]any)
	if len(items) != 1 {
		t.Fatalf("unexpected items: %#v", body["Items"])
	}
	source := items[0].(map[string]any)["MediaSources"].([]any)[0].(map[string]any)
	pathURL, _ := source["Path"].(string)
	if pathURL != "/Videos/remote-1/stream.mkv" {
		t.Fatalf("embedded remote Path should stay as non-tokenized stream URL, got %#v", source)
	}
	if strings.Contains(pathURL, "api_key=") || strings.Contains(pathURL, "token=") {
		t.Fatalf("embedded remote Path must not expose auth key/token: %#v", source)
	}
}

func TestEmbyPlaybackInfoPrefetchesNextRedirect(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			db, err := testdb.OpenPostgres(t, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			create := func(value any) {
				t.Helper()
				if err := db.Create(value).Error; err != nil {
					t.Fatal(err)
				}
			}
			create(&model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "admin", Tier: "plus", IsActive: true})
			create(&model.Library{Base: model.Base{ID: "library"}, Name: "Show", Path: t.TempDir(), Type: "tv"})
			series, season := "series", "season"
			create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: series}, Kind: "series", Title: "Show", Source: "test"})
			create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: season}, Kind: "season", ParentID: &series, SeasonNum: 1, Source: "test"})
			started, release := make(chan struct{}, 4), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.UserAgent() != "Player/1" {
					t.Error("player UA was not preserved")
				}
				if r.URL.Path == "/direct" {
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write([]byte("v"))
					return
				}
				calls.Add(1)
				started <- struct{}{}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Location", "/direct")
				w.WriteHeader(http.StatusFound)
			}))
			defer upstream.Close()
			defer unblock()
			for i, id := range []string{"first", "next"} {
				create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: id}, Kind: "episode", ParentID: &season, EpisodeNum: i + 1, Source: "test"})
				create(&model.Media{PermanentBase: model.PermanentBase{ID: id + "-file"}, LibraryID: "library", MetadataID: id, Path: "/test/" + id + ".strm", STRMURL: upstream.URL + "/origin", SeasonNum: 1, EpisodeNum: i + 1})
			}
			probeJSON, err := service.MarshalProbeDocument(&service.ProbeDocument{SchemaVersion: service.ProbeDocumentSchemaVersion, Streams: []service.ProbeStream{{Index: 0, CodecType: "video", CodecName: "h264"}}})
			if err != nil {
				t.Fatal(err)
			}
			create(&model.MediaProbeMetadata{MediaID: "first-file", ProbeJSON: probeJSON, SchemaVersion: service.ProbeDocumentSchemaVersion})
			repos := repository.New(db)
			if err := repos.Setting.Set(t.Context(), service.PlaybackRedirectResolvePrefixesSettingKey, upstream.URL+"/origin"); err != nil {
				t.Fatal(err)
			}
			svc := &service.Container{Repo: repos, Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos), Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos), Stream: service.NewStreamService(&config.Config{}, zap.NewNop(), repos)}
			svc.Emby.SetMediaProbe(service.NewMediaProbeService(repos, nil))
			router := gin.New()
			registerEmbyRoutes(router, "test-secret", svc)
			token := signedTestToken(t, "test-secret")
			request := func(method, path, body string, want int) {
				t.Helper()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
				r.Header.Set("User-Agent", "Player/1")
				r.Header.Set("X-Emby-Token", token)
				w := httptest.NewRecorder()
				done := make(chan struct{})
				go func() { router.ServeHTTP(w, r); close(done) }()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("PlaybackInfo blocked on upstream")
				}
				if w.Code != want {
					t.Fatalf("status=%d want=%d", w.Code, want)
				}
			}
			request(http.MethodGet, "/Items/first/PlaybackInfo?MediaSourceId=next-file", "", 400)
			request(http.MethodPost, "/Items/first/PlaybackInfo", "{", 400)
			request(http.MethodGet, "/Items/missing/PlaybackInfo", "", 404)
			if calls.Load() != 0 {
				t.Fatal("rejected request prefetched")
			}
			request(method, "/items/first/playbackinfo?MediaSourceId=first-file", "", 200)
			select {
			case <-started:
				t.Fatal("HTTP request prefetched before next episode track metadata")
			case <-time.After(1200 * time.Millisecond):
			}
			create(&model.MediaProbeMetadata{MediaID: "next-file", ProbeJSON: probeJSON, SchemaVersion: service.ProbeDocumentSchemaVersion})
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("HTTP request did not prefetch next episode")
			}
			unblock()
			r := httptest.NewRequest(http.MethodGet, "/Videos/next-file/stream.mkv", nil)
			r.Header.Set("User-Agent", "Player/1")
			r.Header.Set("X-Emby-Token", token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != 302 || w.Header().Get("Location") != upstream.URL+"/direct" {
				t.Fatalf("prefetched playback status=%d", w.Code)
			}
			if calls.Load() != 1 {
				t.Fatalf("upstream source requests=%d want 1", calls.Load())
			}

		})
	}
}
