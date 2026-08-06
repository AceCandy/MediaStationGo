package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestWithAuthTokenPropagatesToInternalRedirect(t *testing.T) {
	// <video src=/api/stream/{id}?token=JWT> follows the 302 to the cloud
	// play endpoint, which must stay authenticated.
	r := &http.Request{Header: http.Header{}, URL: &url.URL{RawQuery: "token=jwt123&profile=p"}}
	got := withAuthToken("/api/cloud/play/cloud115?ref=abc", r)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Query().Get("token") != "jwt123" {
		t.Fatalf("token not propagated: %q", got)
	}
	if u.Query().Get("ref") != "abc" {
		t.Fatalf("existing query lost: %q", got)
	}
}

func TestWithAuthTokenNeverLeaksToAbsoluteURL(t *testing.T) {
	// An absolute external direct link (e.g. cloud CDN) must NOT receive the JWT.
	r := &http.Request{Header: http.Header{}, URL: &url.URL{RawQuery: "token=jwt123"}}
	got := withAuthToken("https://cdn.115.example/x.mp4?sig=1", r)
	if strings.Contains(got, "jwt123") {
		t.Fatalf("JWT leaked to external URL: %q", got)
	}
	if got != "https://cdn.115.example/x.mp4?sig=1" {
		t.Fatalf("external URL mutated: %q", got)
	}
}

func TestWithAuthTokenPropagatesToSameOriginAbsoluteInternalURL(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://media.example/Videos/m-1/stream?api_key=jwt123", nil)
	got := withAuthTokenForInternalRedirect("http://media.example/api/cloud/play/openlist?ref=abc", r, "http://media.example")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Query().Get("token") != "jwt123" || u.Query().Get("ref") != "abc" {
		t.Fatalf("same-origin internal URL should keep ref and receive token: %q", got)
	}
}

func TestWithAuthTokenAddsMediaIDToCloudPlaybackRedirect(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://media.example/api/stream/media-1?token=jwt123", nil)
	got := withAuthTokenForInternalRedirect("/api/cloud/play/openlist?ref=abc", req, "")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Query().Get("token") != "jwt123" || u.Query().Get("media_id") != "media-1" {
		t.Fatalf("cloud redirect should carry token and media_id, got %q", got)
	}
}

func TestServeFileRedirectsInternalSTRMAsAbsoluteURLWithToken(t *testing.T) {
	repos := newStreamTestRepo(t)
	if err := repos.DB.Create(&model.Media{
		Base:    model.Base{ID: "cloud-1"},
		Title:   "Cloud",
		Path:    "cloud://openlist/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=movie",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/cloud-1?api_key=jwt123", nil)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "cloud-1"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "http://nas.local:18080/api/cloud/play/openlist?") ||
		!strings.Contains(loc, "ref=movie") ||
		!strings.Contains(loc, "token=jwt123") {
		t.Fatalf("redirect Location should be absolute and tokenized, got %q", loc)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("cloud redirect Cache-Control = %q, want no-store", got)
	}
}

func TestServeFileRedirectUsesForwardedTunnelHost(t *testing.T) {
	repos := newStreamTestRepo(t)
	if err := repos.DB.Create(&model.Media{
		Base:    model.Base{ID: "cloud-1"},
		Title:   "Cloud",
		Path:    "cloud://openlist/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=movie",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/stream/cloud-1?api_key=jwt123", nil)
	req.Header.Set("X-Forwarded-Host", "media.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "cloud-1"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://media.example.com/api/cloud/play/openlist?") ||
		!strings.Contains(loc, "ref=movie") ||
		!strings.Contains(loc, "token=jwt123") {
		t.Fatalf("redirect Location should use forwarded tunnel host and token, got %q", loc)
	}
}

func TestServeFileRedirectsCloudMediaForVideoStreamMode(t *testing.T) {
	repos := newStreamTestRepo(t)
	if err := repos.Setting.Set(t.Context(), CloudPlaybackModeSettingKey, CloudPlaybackModeRedirectProxy); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		Base:    model.Base{ID: "cloud-1"},
		Title:   "Cloud",
		Path:    "cloud://openlist/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=movie",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/cloud-1?api_key=jwt123", nil)
	w := httptest.NewRecorder()

	err := svc.ServeFile(w, req, "cloud-1")
	if err != nil {
		t.Fatalf("video stream mode should still reach cloud playback endpoint: %v", err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/api/cloud/play/openlist?") || !strings.Contains(loc, "token=jwt123") {
		t.Fatalf("redirect Location should target tokenized cloud play endpoint, got %q", loc)
	}
}

func TestServeFileRedirectsCloudMediaExternalHTTPSTRMURL(t *testing.T) {
	repos := newStreamTestRepo(t)
	target := "https://cdn.example.test/%E5%AF%92%E6%88%98.mkv?sign=direct"
	if err := repos.DB.Create(&model.Media{
		Base:    model.Base{ID: "cloud-http"},
		Title:   "Cloud HTTP",
		Path:    "cloud://openlist/Movie.mkv",
		STRMURL: target,
	}).Error; err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/cloud-http?token=jwt123", nil)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "cloud-http"); err != nil {
		t.Fatalf("external HTTP STRM target should redirect: %v", err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != target {
		t.Fatalf("Location = %q, want %q", loc, target)
	}
	if strings.Contains(loc, "jwt123") || strings.Contains(loc, "media_id=") {
		t.Fatalf("external direct link must not receive internal auth query, got %q", loc)
	}
	entries := observed.FilterMessage("media playback redirect").All()
	if len(entries) != 1 {
		t.Fatalf("redirect log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["playback_source"] != "remote_redirect" || fields["resolve_source"] != "configured" ||
		fields["target_scheme"] != "https" || fields["target_host"] != "cdn.example.test" || fields["target_path"] != "/寒战.mkv" ||
		fmt.Sprint(fields["target_query_keys"]) != "[sign]" {
		t.Fatalf("unexpected redirect log fields: %#v", fields)
	}
	loggedTarget := fmt.Sprint(fields["target_scheme"], fields["target_host"], fields["target_path"], fields["target_query_keys"])
	if strings.Contains(loggedTarget, "direct") || fields["target_hash"] == "" {
		t.Fatalf("redirect log should hide query values and include target hash: %#v", fields)
	}
}

func TestServeFileLogsLocalFilePath(t *testing.T) {
	repos := newStreamTestRepo(t)
	target := filepath.Join(t.TempDir(), "Movie.mkv")
	if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{Base: model.Base{ID: "local-file"}, Path: target}).Error; err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/stream/local-file", nil)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "local-file"); err != nil {
		t.Fatal(err)
	}
	entries := observed.FilterMessage("media playback local").All()
	if len(entries) != 1 {
		t.Fatalf("local playback log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["playback_source"] != "local_file" || fields["path"] != target {
		t.Fatalf("unexpected local playback log fields: %#v", fields)
	}
}

func TestServeFileRedirectsMappedLocalPathUsingLongestPrefix(t *testing.T) {
	repos := newStreamTestRepo(t)
	localPath := "/mnt/media/new115/电影/测试 影片 (2026).mkv"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.Header.Get("Range") != "bytes=0-0" {
			t.Errorf("Range = %q, want bytes=0-0", r.Header.Get("Range"))
		}
		if r.URL.EscapedPath() != "/d/new115/%E7%94%B5%E5%BD%B1/%E6%B5%8B%E8%AF%95%20%E5%BD%B1%E7%89%87%20%282026%29.mkv" {
			t.Errorf("escaped path = %q", r.URL.EscapedPath())
		}
		http.Redirect(w, r, "https://cdn.example.test/movie.mkv?t=temporary", http.StatusFound)
	}))
	defer upstream.Close()
	mappings := strings.Join([]string{
		"/mnt/media => https://fallback.example.test/d/all/",
		"/mnt/media/new115/ => " + upstream.URL + "/d/new115/",
	}, "\n")
	if err := repos.Setting.Set(t.Context(), PlaybackPathMappingsSettingKey, mappings); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{Base: model.Base{ID: "mapped-local"}, Path: localPath}).Error; err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos, nil)
	svc.SetStorageConfig(NewStorageConfigService(zap.NewNop(), nil, nil))
	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/stream/mapped-local", nil)
	req.Header.Set("User-Agent", "mapped-player")
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "mapped-local"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Scheme != "https" || location.Host != "cdn.example.test" ||
		location.Path != "/movie.mkv" || location.Query().Get("t") != "temporary" {
		t.Fatalf("unexpected mapped redirect: %q", location.String())
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("mapped redirect Cache-Control = %q, want no-store", got)
	}
	w = httptest.NewRecorder()
	if err := svc.ServeFile(w, req, "mapped-local"); err != nil {
		t.Fatal(err)
	}
	if upstreamCalls != 1 || w.Header().Get("Location") != location.String() {
		t.Fatalf("cached redirect calls/location = %d/%q", upstreamCalls, w.Header().Get("Location"))
	}
	entries := observed.FilterMessage("media playback redirect").All()
	if len(entries) != 2 {
		t.Fatalf("redirect log entries = %d, want 2", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["playback_source"] != "remote_redirect" || fields["resolve_source"] != "path_mapping" ||
		fields["path"] != localPath || fields["target_host"] != "cdn.example.test" ||
		fields["target_path"] != "/movie.mkv" || fields["cache_hit"] != false {
		t.Fatalf("unexpected mapped redirect log fields: %#v", fields)
	}
	if fields := entries[1].ContextMap(); fields["cache_hit"] != true {
		t.Fatalf("cached redirect log fields: %#v", fields)
	}
}

func TestServeFileRedirectsMappedLocalSTRMTarget(t *testing.T) {
	repos := newStreamTestRepo(t)
	localTarget := "/mnt/media/new115/电影/测试影片 (2026).mp4"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://cdn.example.test/zhongkui.mp4?t=temporary", http.StatusFound)
	}))
	defer upstream.Close()
	if err := repos.Setting.Set(t.Context(), PlaybackPathMappingsSettingKey,
		"/mnt/media/new115/ => "+upstream.URL+"/d/new115/"); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		Base:      model.Base{ID: "mapped-local-strm"},
		Path:      "/data/strm/测试影片 (2026).strm",
		Container: "strm",
		STRMURL:   localTarget,
	}).Error; err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos, nil)
	svc.SetStorageConfig(NewStorageConfigService(zap.NewNop(), nil, nil))
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, httptest.NewRequest(http.MethodGet, "/api/stream/mapped-local-strm", nil), "mapped-local-strm"); err != nil {
		t.Fatal(err)
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound || location.Host != "cdn.example.test" ||
		location.Path != "/zhongkui.mp4" {
		t.Fatalf("unexpected local STRM redirect: status=%d location=%q", w.Code, location.String())
	}
	entries := observed.FilterMessage("media playback redirect").All()
	if len(entries) != 1 {
		t.Fatalf("redirect log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["resolve_source"] != "path_mapping" || fields["path"] != localTarget {
		t.Fatalf("unexpected local STRM redirect log fields: %#v", fields)
	}
}

func TestServeFileIgnoresInvalidOrNonMatchingPathMappings(t *testing.T) {
	repos := newStreamTestRepo(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "media-other", "Movie.mkv")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	mappings := strings.Join([]string{
		"# comments are ignored",
		filepath.Join(dir, "media") + " => https://cdn.example.test/d/media/",
		filepath.Join(dir, "media-other") + " => ftp://cdn.example.test/d/media-other/",
		"invalid line",
	}, "\n")
	if err := repos.Setting.Set(t.Context(), PlaybackPathMappingsSettingKey, mappings); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{Base: model.Base{ID: "local-fallback"}, Path: target}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, httptest.NewRequest(http.MethodGet, "/api/stream/local-fallback", nil), "local-fallback"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || w.Body.String() != "video" || w.Header().Get("Location") != "" {
		t.Fatalf("status/body/location = %d/%q/%q, want local 200", w.Code, w.Body.String(), w.Header().Get("Location"))
	}
}

func TestServeFileRedirectsLocalSTRMFileTargetByDefault(t *testing.T) {
	repos := newStreamTestRepo(t)
	target := "https://cdn.example.test/LocalMovie.mkv?sign=direct"
	if err := repos.DB.Create(&model.Media{
		Base:      model.Base{ID: "local-strm"},
		Title:     "Local STRM",
		Path:      "D:/media/LocalMovie.strm",
		Container: "strm",
		STRMURL:   target,
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/local-strm?token=jwt123", nil)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "local-strm"); err != nil {
		t.Fatalf("local .strm media should redirect to its target: %v", err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != target {
		t.Fatalf("Location = %q, want %q", loc, target)
	}
}

func TestServeFileReadsLocalPathFromLegacySTRMRecord(t *testing.T) {
	repos := newStreamTestRepo(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "LocalMovie.mkv")
	if err := os.WriteFile(target, []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(dir, "LocalMovie.strm")
	if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		Base:      model.Base{ID: "local-path-strm"},
		Title:     "Local STRM",
		Path:      strmPath,
		Container: "strm",
	}).Error; err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/stream/local-path-strm", nil)
	req.Header.Set("Range", "bytes=1-3")
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "local-path-strm"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusPartialContent || w.Body.String() != "bcd" {
		t.Fatalf("status/body = %d/%q, want 206/%q", w.Code, w.Body.String(), "bcd")
	}
	entries := observed.FilterMessage("media playback local").All()
	if len(entries) != 1 {
		t.Fatalf("local playback log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["playback_source"] != "local_strm" || fields["path"] != target {
		t.Fatalf("unexpected local playback log fields: %#v", fields)
	}
}

func TestStreamProbeUsesLocalSTRMTarget(t *testing.T) {
	repos := newStreamTestRepo(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "LocalMovie.mkv")
	if err := os.WriteFile(target, []byte("target-video"), 0o644); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(dir, "LocalMovie.strm")
	if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	media := model.Media{Base: model.Base{ID: "local-probe-strm"}, Path: strmPath, Container: "strm"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	prober := &fakeCloudPlaybackProber{probe: &ProbeResult{
		DurationSec: 120, Width: 1920, Height: 1080, VideoCodec: "h264", AudioCodec: "aac", Container: "matroska,webm",
	}}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)

	if err := svc.Probe(t.Context(), media.ID, prober); err != nil {
		t.Fatal(err)
	}
	if prober.path != target {
		t.Fatalf("probed path = %q, want STRM target %q", prober.path, target)
	}
	var persisted model.Media
	if err := repos.DB.First(&persisted, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.DurationSec != 120 || persisted.SizeBytes != int64(len("target-video")) {
		t.Fatalf("probe metadata not persisted: %#v", persisted)
	}
}

func TestCloudPlaybackModeUsesExplicitModeBeforeLegacySTRMFlag(t *testing.T) {
	repos := newStreamTestRepo(t)
	if got := CloudPlaybackMode(t.Context(), repos); got != CloudPlaybackModeRedirectProxy {
		t.Fatalf("default mode = %q, want %q", got, CloudPlaybackModeRedirectProxy)
	}
	if err := repos.Setting.Set(t.Context(), STRMEnabledSettingKey, "true"); err != nil {
		t.Fatal(err)
	}
	if got := CloudPlaybackMode(t.Context(), repos); got != CloudPlaybackModeSTRM {
		t.Fatalf("legacy strm.enabled=true mode = %q, want %q", got, CloudPlaybackModeSTRM)
	}
	if err := repos.Setting.Set(t.Context(), CloudPlaybackModeSettingKey, CloudPlaybackModeRedirectProxy); err != nil {
		t.Fatal(err)
	}
	if got := CloudPlaybackMode(t.Context(), repos); got != CloudPlaybackModeRedirectProxy {
		t.Fatalf("explicit mode should override legacy flag, got %q", got)
	}
	if err := repos.Setting.Set(t.Context(), CloudPlaybackModeSettingKey, CloudPlaybackModeSTRM); err != nil {
		t.Fatal(err)
	}
	if got := CloudPlaybackMode(t.Context(), repos); got != CloudPlaybackModeSTRM {
		t.Fatalf("explicit strm mode = %q, want %q", got, CloudPlaybackModeSTRM)
	}
	if err := repos.Setting.Set(t.Context(), CloudPlaybackSTRMEnabledSettingKey, "false"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), CloudPlaybackRedirectEnabledSettingKey, "false"); err != nil {
		t.Fatal(err)
	}
	if got := CloudPlaybackMode(t.Context(), repos); got != "" {
		t.Fatalf("both disabled mode = %q, want empty", got)
	}
}

func TestServeFileRejectsCloudMediaWhenSelectedModeDisabled(t *testing.T) {
	repos := newStreamTestRepo(t)
	if err := repos.Setting.Set(t.Context(), CloudPlaybackSTRMEnabledSettingKey, "false"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), CloudPlaybackRedirectEnabledSettingKey, "false"); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		Base:    model.Base{ID: "cloud-1"},
		Title:   "Cloud",
		Path:    "cloud://openlist/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=movie",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/cloud-1?api_key=jwt123", nil)
	w := httptest.NewRecorder()

	err := svc.ServeFileWithCloudMode(w, req, "cloud-1", CloudPlaybackModeSTRM)
	if !errors.Is(err, ErrCloudPlaybackDisabled) {
		t.Fatalf("error = %v, want ErrCloudPlaybackDisabled", err)
	}
}

func newStreamTestRepo(t *testing.T) *repository.Container {
	t.Helper()
	db := newServiceTestDB(t, &model.Media{}, &model.Setting{})
	return repository.New(db)
}

func TestRequestTokenFromBearerHeader(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer hdrtok")
	r := &http.Request{Header: h, URL: &url.URL{}}
	if got := requestToken(r); got != "hdrtok" {
		t.Fatalf("bearer token not extracted: %q", got)
	}
}

func TestRequestTokenFromMediaBrowserAuthorizationHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-MediaBrowser-Authorization", `MediaBrowser Client="Infuse", Device="PC", Token="mbtok"`)
	r := &http.Request{Header: h, URL: &url.URL{}}
	if got := requestToken(r); got != "mbtok" {
		t.Fatalf("MediaBrowser token not extracted: %q", got)
	}
}

func TestAppendQueryToHLSSegments(t *testing.T) {
	in := "#EXTM3U\n#EXTINF:4.0,\nseg_00000.ts\n#EXTINF:4.0,\nseg_00001.ts?old=1\n"
	got := appendQueryToHLSSegments(in, "token=abc")
	if !strings.Contains(got, "seg_00000.ts?token=abc") {
		t.Fatalf("missing tokenized segment: %q", got)
	}
	if !strings.Contains(got, "seg_00001.ts?old=1") {
		t.Fatalf("existing query should be preserved: %q", got)
	}
}

func TestAppendQueryToHLSSegmentsDropsUnknownQuery(t *testing.T) {
	got := appendQueryToHLSSegments("#EXTM3U\nseg_00000.ts\n", "api_key=abc&AudioStreamIndex=3&redirect=https%3A%2F%2Fevil.invalid")
	if !strings.Contains(got, "api_key=abc") || !strings.Contains(got, "AudioStreamIndex=3") {
		t.Fatalf("allowed query missing: %q", got)
	}
	if strings.Contains(got, "redirect") || strings.Contains(got, "evil.invalid") {
		t.Fatalf("unknown query leaked: %q", got)
	}
}

func TestResolvedHLSQueryPinsValidatedAudioSelection(t *testing.T) {
	got := resolvedHLSQuery("token=abc&audioStreamIndex=99&redirect=https%3A%2F%2Fevil.invalid", TranscodeKey{MediaID: "media", AudioStreamIndex: 3})
	values, err := url.ParseQuery(got)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("token") != "abc" || values.Get("AudioStreamIndex") != "3" || values.Get("audioStreamIndex") != "" {
		t.Fatalf("resolved query = %q", got)
	}
	if values.Get("redirect") != "" || values.Get("_hls_audio_fallback") != "" {
		t.Fatalf("unexpected query fields = %q", got)
	}

	fallback := resolvedHLSQuery("token=abc", TranscodeKey{MediaID: "media", AudioStreamIndex: -1})
	values, err = url.ParseQuery(fallback)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("AudioStreamIndex") != "-1" || values.Get("_hls_audio_fallback") != "1" {
		t.Fatalf("fallback query = %q", fallback)
	}
}

func TestHLSKeyKeepsFallbackAfterProbeArrives(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.MediaProbeMetadata{})
	repos := repository.New(db)
	metadata := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"})
	media := model.Media{MetadataID: metadata.ID, LibraryID: "library", Title: "Movie", Path: "/movie.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	doc := &ProbeDocument{SchemaVersion: ProbeDocumentSchemaVersion, Streams: []ProbeStream{
		{Index: 2, CodecType: "audio"},
		{Index: 3, CodecType: "audio", Disposition: ProbeDisposition{Default: true}},
	}}
	probeJSON, err := MarshalProbeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: probeJSON, SchemaVersion: ProbeDocumentSchemaVersion}).Error; err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/?AudioStreamIndex=-1&_hls_audio_fallback=1", nil)
	stream := &StreamService{mediaProbe: NewMediaProbeService(repos, nil)}
	key, err := stream.hlsKey(request, media.ID)
	if err != nil || key.AudioStreamIndex != -1 {
		t.Fatalf("key = %#v, err=%v", key, err)
	}
}
