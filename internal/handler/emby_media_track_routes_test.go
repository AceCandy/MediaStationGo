package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestEmbyPlaybackInfoRoutesParseGETAndPOSTSelections(t *testing.T) {
	router, mediaID, token, _, _ := newEmbyTrackRouteTest(t)

	get := httptest.NewRequest(http.MethodGet, "/emby/Items/"+mediaID+"/PlaybackInfo?AudioStreamIndex=0&SubtitleStreamIndex=-1", nil)
	get.Header.Set("X-Emby-Token", token)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, get)
	assertPlaybackSelectionURL(t, getResponse, 0, -1)

	post := httptest.NewRequest(http.MethodPost, "/items/"+mediaID+"/playbackinfo", strings.NewReader(`{"MediaSourceId":"`+mediaID+`","AudioStreamIndex":2,"SubtitleStreamIndex":3}`))
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-Emby-Token", token)
	postResponse := httptest.NewRecorder()
	router.ServeHTTP(postResponse, post)
	assertPlaybackSelectionURL(t, postResponse, 2, 3)

	invalid := httptest.NewRequest(http.MethodGet, "/Items/"+mediaID+"/PlaybackInfo?AudioStreamIndex=9", nil)
	invalid.Header.Set("X-Emby-Token", token)
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid audio status = %d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}

	for name, body := range map[string]string{
		"negative audio":    `{"AudioStreamIndex":-2}`,
		"negative subtitle": `{"SubtitleStreamIndex":-2}`,
		"unknown subtitle":  `{"SubtitleStreamIndex":9}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/Items/"+mediaID+"/PlaybackInfo", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Emby-Token", token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestEmbySubtitleDeliveryRoutesRediscoverSidecar(t *testing.T) {
	router, mediaID, token, subtitlePath, _ := newEmbyTrackRouteTest(t)
	for _, path := range []string{
		"/emby/Videos/" + mediaID + "/Subtitles/3/Stream.vtt",
		"/videos/" + mediaID + "/subtitles/3/stream.vtt",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Emby-Token", token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "WEBVTT") {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}

	if err := os.Remove(subtitlePath); err != nil {
		t.Fatal(err)
	}
	stale := httptest.NewRequest(http.MethodGet, "/emby/Videos/"+mediaID+"/Subtitles/3/Stream.vtt", nil)
	stale.Header.Set("X-Emby-Token", token)
	staleResponse := httptest.NewRecorder()
	router.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusNotFound {
		t.Fatalf("stale subtitle status = %d body=%s", staleResponse.Code, staleResponse.Body.String())
	}
}

func TestEmbyProgressRoutesUseProbeDurationAndIgnoreUnknownDuration(t *testing.T) {
	router, mediaID, token, _, repos := newEmbyTrackRouteTest(t)
	paths := []string{"/Sessions/Playing", "/Sessions/Playing/Progress", "/Sessions/Playing/Stopped"}
	request := `{"ItemId":"` + mediaID + `","MediaSourceId":"` + mediaID + `","PositionTicks":300000000}`

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(request))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Emby-Token", token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusNoContent {
			t.Fatalf("unknown duration %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	var count int64
	if err := repos.DB.Model(&model.PlaybackHistory{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unknown duration history count=%d err=%v", count, err)
	}
	if err := repos.DB.Model(&model.MediaProbeMetadata{}).Where("media_id = ?", mediaID).Update("duration_ms", 120_000).Error; err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(request))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Emby-Token", token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusNoContent {
			t.Fatalf("probe duration %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	var history model.PlaybackHistory
	if err := repos.DB.First(&history).Error; err != nil || history.DurationMs != 120_000 {
		t.Fatalf("probe duration history=%#v err=%v", history, err)
	}
}

func newEmbyTrackRouteTest(t *testing.T) (*gin.Engine, string, string, string, *repository.Container) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base: model.Base{ID: "user-1"}, Username: "tester", PasswordHash: "x", Role: "admin", Tier: "plus", IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}
	library := model.Library{Name: "Movies", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Track Movie", Source: "local"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(library.Path, "track-movie.mkv")
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	subtitlePath := filepath.Join(library.Path, "track-movie.zh.vtt")
	if err := os.WriteFile(subtitlePath, []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n你好\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, MetadataID: metadata.ID, Title: "Track Movie", Path: mediaPath, Container: "matroska"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	doc := &service.ProbeDocument{SchemaVersion: service.ProbeDocumentSchemaVersion, Streams: []service.ProbeStream{
		{Index: 0, CodecType: "audio", CodecName: "aac"},
		{Index: 1, CodecType: "video", CodecName: "hevc", Width: 3840, Height: 2160},
		{Index: 2, CodecType: "audio", CodecName: "eac3", Disposition: service.ProbeDisposition{Default: true}},
	}}
	probeJSON, err := service.MarshalProbeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MediaProbeMetadata{
		MediaID: media.ID, ProbeJSON: probeJSON, SchemaVersion: service.ProbeDocumentSchemaVersion, ProbedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	mediaProbe := service.NewMediaProbeService(repos, nil)
	subtitles := service.NewSubtitleService(zap.NewNop(), repos)
	subtitles.SetMediaProbe(mediaProbe)
	emby := service.NewEmbyService(&config.Config{}, zap.NewNop(), repos)
	emby.SetMediaProbe(mediaProbe)
	emby.SetSubtitle(subtitles)
	container := &service.Container{Log: zap.NewNop(), Repo: repos, Emby: emby, Subtitle: subtitles}
	const secret = "track-route-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, container)
	return router, media.ID, signedTestToken(t, secret), subtitlePath, repos
}

func assertPlaybackSelectionURL(t *testing.T, response *httptest.ResponseRecorder, audioIndex, subtitleIndex int) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		MediaSources []struct {
			DirectStreamURL string `json:"DirectStreamUrl"`
		} `json:"MediaSources"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || len(payload.MediaSources) != 1 {
		t.Fatalf("decode playback info: payload=%#v err=%v", payload, err)
	}
	parsed, err := url.Parse(payload.MediaSources[0].DirectStreamURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("AudioStreamIndex") != strconv.Itoa(audioIndex) || parsed.Query().Get("SubtitleStreamIndex") != strconv.Itoa(subtitleIndex) {
		t.Fatalf("selection query = %q", parsed.RawQuery)
	}
}
