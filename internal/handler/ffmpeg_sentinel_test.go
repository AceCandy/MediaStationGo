package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestRetainedPlaybackWorkflowsNeverStartFFmpeg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell sentinel is POSIX-only")
	}

	binDir := t.TempDir()
	marker := filepath.Join(binDir, "ffmpeg-started")
	ffprobePath := filepath.Join(binDir, "ffprobe")
	ffmpegPath := filepath.Join(binDir, "ffmpeg")
	probeOutput := `{"format":{"format_name":"matroska,webm","duration":"120.5","size":"16"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080},{"index":1,"codec_type":"audio","codec_name":"aac","channels":2},{"index":2,"codec_type":"subtitle","codec_name":"ass","tags":{"language":"chi","title":"Embedded"}}]}`
	if err := os.WriteFile(ffprobePath, []byte("#!/bin/sh\ncat <<'JSON'\n"+probeOutput+"\nJSON\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ffmpegPath, []byte(fmt.Sprintf("#!/bin/sh\n: > %q\nexit 97\n", marker)), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	const secret = "ffmpeg-sentinel-secret"
	if err := repos.User.Create(t.Context(), &model.User{
		Base: model.Base{ID: "user-1"}, Username: "sentinel", PasswordHash: "x",
		Role: "admin", Tier: "plus", IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}

	mediaDir := t.TempDir()
	library := model.Library{Name: "Sentinel", Path: mediaDir, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Sentinel Movie", Source: "local"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(mediaDir, "sentinel.mkv")
	if err := os.WriteFile(mediaPath, []byte("fake-video-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaDir, "sentinel.zh.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nsubtitle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		PermanentBase: model.PermanentBase{ID: "sentinel-media"}, LibraryID: library.ID, MetadataID: metadata.ID,
		Title: "Sentinel Movie", Path: mediaPath, Container: "matroska",
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		App: config.AppConfig{
			DataDir: mediaDir, FFprobePath: ffprobePath, FFprobeMaxConcurrent: 1,
		},
		Cache:   config.CacheConfig{CacheDir: t.TempDir()},
		Secrets: config.SecretsConfig{JWTSecret: secret},
	}
	container := service.New(cfg, zap.NewNop(), repos)
	closed := false
	t.Cleanup(func() {
		if !closed {
			container.Close()
		}
	})
	detail := httptest.NewRecorder()
	detailContext, _ := gin.CreateTestContext(detail)
	detailContext.Set(middleware.CtxUserID, "user-1")
	detailContext.Set(middleware.CtxUserRole, "admin")
	detailContext.Params = gin.Params{{Key: "id", Value: media.ID}}
	detailContext.Request = httptest.NewRequest(http.MethodGet, "/api/media/"+media.ID, nil)
	getMediaHandler(container)(detailContext)
	if detail.Code != http.StatusOK {
		t.Fatalf("media detail status = %d body=%s", detail.Code, detail.Body.String())
	}
	if _, ok := container.MediaProbe.Load(t.Context(), media.ID); ok {
		t.Fatal("media detail blocked on media probe")
	}
	container.Boot()
	assertFFmpegSentinelNotStarted(t, marker, "service boot")

	probe := httptest.NewRecorder()
	probeContext, _ := gin.CreateTestContext(probe)
	probeContext.Set(middleware.CtxUserID, "user-1")
	probeContext.Set(middleware.CtxUserRole, "user")
	probeContext.Params = gin.Params{{Key: "id", Value: media.ID}}
	probeContext.Request = httptest.NewRequest(http.MethodPost, "/api/media/"+media.ID+"/probe/ensure", nil)
	ensureMediaProbeHandler(container)(probeContext)
	if probe.Code != http.StatusOK || !strings.Contains(probe.Body.String(), `"tracks":[`) {
		t.Fatalf("ensure media probe status = %d body=%s", probe.Code, probe.Body.String())
	}
	assertFFmpegSentinelNotStarted(t, marker, "probe")

	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerEmbyRoutes(router, secret, container)
	token := signedTestToken(t, secret)

	playback := serveEmbySentinelRequest(t, router, token, "/Items/"+media.ID+"/PlaybackInfo")
	if playback.Code != http.StatusOK {
		t.Fatalf("PlaybackInfo status = %d body=%s", playback.Code, playback.Body.String())
	}
	assertSentinelSubtitleFacts(t, playback)
	assertFFmpegSentinelNotStarted(t, marker, "PlaybackInfo")

	original := serveEmbySentinelRequest(t, router, token, "/Videos/"+media.ID+"/original.mkv")
	if original.Code != http.StatusOK || original.Body.String() != "fake-video-bytes" {
		t.Fatalf("original status = %d body=%q", original.Code, original.Body.String())
	}
	assertFFmpegSentinelNotStarted(t, marker, "original playback")

	embedded := serveEmbySentinelRequest(t, router, token, "/Videos/"+media.ID+"/Subtitles/2/Stream.vtt")
	if embedded.Code != http.StatusNotFound {
		t.Fatalf("embedded subtitle status = %d body=%s", embedded.Code, embedded.Body.String())
	}
	assertFFmpegSentinelNotStarted(t, marker, "embedded subtitle")

	external := serveEmbySentinelRequest(t, router, token, "/Videos/"+media.ID+"/Subtitles/3/Stream.vtt")
	if external.Code != http.StatusOK || !strings.Contains(external.Body.String(), "WEBVTT") {
		t.Fatalf("external subtitle status = %d body=%s", external.Code, external.Body.String())
	}
	assertFFmpegSentinelNotStarted(t, marker, "external subtitle")

	container.Close()
	closed = true
	assertFFmpegSentinelNotStarted(t, marker, "service close")
}

func serveEmbySentinelRequest(t *testing.T, router http.Handler, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Emby-Token", token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func assertSentinelSubtitleFacts(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	var payload struct {
		MediaSources []struct {
			MediaStreams []struct {
				Index       int    `json:"Index"`
				Type        string `json:"Type"`
				IsExternal  bool   `json:"IsExternal"`
				DeliveryURL string `json:"DeliveryUrl"`
			} `json:"MediaStreams"`
		} `json:"MediaSources"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || len(payload.MediaSources) != 1 {
		t.Fatalf("decode PlaybackInfo: payload=%#v err=%v", payload, err)
	}
	var embedded, external bool
	for _, stream := range payload.MediaSources[0].MediaStreams {
		if stream.Type != "Subtitle" {
			continue
		}
		if stream.Index == 2 && !stream.IsExternal && stream.DeliveryURL == "" {
			embedded = true
		}
		if stream.Index == 3 && stream.IsExternal && stream.DeliveryURL != "" {
			external = true
		}
	}
	if !embedded || !external {
		t.Fatalf("subtitle facts: embedded=%t external=%t payload=%#v", embedded, external, payload)
	}
}

func assertFFmpegSentinelNotStarted(t *testing.T, marker, workflow string) {
	t.Helper()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ffmpeg sentinel started during %s: %v", workflow, err)
	}
}
