package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const streamTestJWTSecret = "stream-test-secret"

func TestInternalStreamRedirectUsesMediaScopedToken(t *testing.T) {
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), nil)
	accountToken := signStreamTestToken(t, Claims{UserID: "user-1", Role: "user", Tier: "basic"})
	r := httptest.NewRequest(http.MethodGet, "http://media.example/Videos/wrapper-1/stream?"+url.Values{
		"token": {accountToken},
	}.Encode(), nil)
	got := svc.withExternalPlaybackTokenForInternalRedirect(
		"/api/stream/source-1?quality=source&token=stored-token&api_key=stored-key",
		r,
		"",
		"wrapper-1",
	)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("api_key") != "" || len(q["token"]) != 1 || q.Get("token") == accountToken || q.Get("token") == "stored-token" {
		t.Fatalf("internal redirect should contain only a fresh scoped token: %q", got)
	}
	claims := parseStreamTestToken(t, q.Get("token"))
	if claims.UserID != "user-1" || claims.Purpose != ExternalPlaybackTokenPurpose || claims.MediaID != "source-1" {
		t.Fatalf("redirect claims = %#v, want user-1 scoped to source-1", claims)
	}
	if q.Get("quality") != "source" {
		t.Fatalf("existing query lost: %q", got)
	}
}

func TestExternalRedirectNeverReceivesPlaybackToken(t *testing.T) {
	r := &http.Request{Header: http.Header{}, URL: &url.URL{RawQuery: "token=jwt123"}}
	got := (&StreamService{}).withExternalPlaybackTokenForInternalRedirect(
		"https://cdn.example.test/x.mp4?quality=source", r, "", "wrapper-1",
	)
	if strings.Contains(got, "jwt123") {
		t.Fatalf("JWT leaked to external URL: %q", got)
	}
	if got != "https://cdn.example.test/x.mp4?quality=source" {
		t.Fatalf("external URL mutated: %q", got)
	}
}

func TestSameOriginAbsoluteInternalURLUsesMediaScopedToken(t *testing.T) {
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), nil)
	accountToken := signStreamTestToken(t, Claims{UserID: "user-1", Role: "user"})
	r := httptest.NewRequest(http.MethodGet, "http://media.example/Videos/m-1/stream?api_key="+accountToken, nil)
	got := svc.withExternalPlaybackTokenForInternalRedirect(
		"http://media.example/api/stream/source-1?quality=source", r, "http://media.example", "m-1",
	)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	claims := parseStreamTestToken(t, u.Query().Get("token"))
	if claims.MediaID != "source-1" || claims.Purpose != ExternalPlaybackTokenPurpose || u.Query().Get("quality") != "source" {
		t.Fatalf("same-origin internal URL should keep query and receive scoped token: %q", got)
	}
}

func TestServeFileRedirectsInternalSTRMAsAbsoluteURLWithToken(t *testing.T) {
	repos := newStreamTestRepo(t)
	rows := []model.Media{
		{Base: model.Base{ID: "internal-strm"}, Title: "Internal STRM", Path: "/media/Internal.strm", Container: "strm", STRMURL: "/api/stream/source-1?quality=source&token=stored-token&api_key=stored-key"},
		{Base: model.Base{ID: "source-1"}, Path: "/media/Source.mkv", DurationSec: 2 * 60 * 60},
	}
	for i := range rows {
		if err := repos.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), repos)
	accountToken := signStreamTestToken(t, Claims{UserID: "user-1", Role: "user", Tier: "basic"})
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/internal-strm?api_key="+accountToken, nil)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "internal-strm"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	location, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	if location.Scheme != "http" || location.Host != "nas.local:18080" || location.Path != "/api/stream/source-1" ||
		location.Query().Get("quality") != "source" || location.Query().Get("api_key") != "" {
		t.Fatalf("redirect Location should be absolute and tokenized, got %q", loc)
	}
	claims := parseStreamTestToken(t, location.Query().Get("token"))
	if claims.UserID != "user-1" || claims.Purpose != ExternalPlaybackTokenPurpose || claims.MediaID != "source-1" || location.Query().Get("token") == accountToken {
		t.Fatalf("redirect claims = %#v, want a new user-1 token scoped to source-1", claims)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("redirect Cache-Control = %q, want no-store", got)
	}
}

func TestServeFileRedirectUsesForwardedTunnelHost(t *testing.T) {
	repos := newStreamTestRepo(t)
	rows := []model.Media{
		{Base: model.Base{ID: "internal-strm"}, Title: "Internal STRM", Path: "/media/Internal.strm", Container: "strm", STRMURL: "/api/stream/source-1?quality=source"},
		{Base: model.Base{ID: "source-1"}, Path: "/media/Source.mkv"},
	}
	for i := range rows {
		if err := repos.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), repos)
	accountToken := signStreamTestToken(t, Claims{UserID: "user-1", Role: "user"})
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/stream/internal-strm?api_key="+accountToken, nil)
	req.Header.Set("X-Forwarded-Host", "media.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "internal-strm"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	location, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	if location.Scheme != "https" || location.Host != "media.example.com" || location.Path != "/api/stream/source-1" ||
		location.Query().Get("quality") != "source" || location.Query().Get("token") == accountToken {
		t.Fatalf("redirect Location should use forwarded tunnel host and token, got %q", loc)
	}
	if claims := parseStreamTestToken(t, location.Query().Get("token")); claims.MediaID != "source-1" {
		t.Fatalf("redirect claims = %#v, want source-1 scope", claims)
	}
}

func TestInternalStreamRedirectDoesNotPropagateInvalidToken(t *testing.T) {
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), nil)
	r := httptest.NewRequest(http.MethodGet, "http://media.example/api/stream/wrapper-1?api_key=invalid-token", nil)
	got := svc.withExternalPlaybackTokenForInternalRedirect(
		"/api/stream/source-1?quality=source&token=stored-token&api_key=stored-key", r, "", "wrapper-1",
	)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("token") != "" || u.Query().Get("api_key") != "" || u.Query().Get("quality") != "source" {
		t.Fatalf("invalid credentials must not reach internal redirect: %q", got)
	}
}

func TestInternalStreamRedirectRescopesExistingPlaybackToken(t *testing.T) {
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), nil)
	sourceToken := signStreamTestToken(t, Claims{
		UserID: "user-1", Role: "user", Purpose: ExternalPlaybackTokenPurpose, MediaID: "wrapper-1",
	})
	r := httptest.NewRequest(http.MethodGet, "http://media.example/api/stream/wrapper-1?token="+sourceToken, nil)
	got := svc.withExternalPlaybackTokenForInternalRedirect("/api/stream/source-1", r, "", "wrapper-1")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	targetToken := u.Query().Get("token")
	claims := parseStreamTestToken(t, targetToken)
	if targetToken == sourceToken || claims.Purpose != ExternalPlaybackTokenPurpose || claims.MediaID != "source-1" {
		t.Fatalf("redirect claims = %#v, want a new token scoped to source-1", claims)
	}
}

func TestInternalStreamRedirectRejectsUnusableScopedTokens(t *testing.T) {
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), nil)
	tests := []struct {
		name   string
		claims Claims
	}{
		{
			name: "different source media",
			claims: Claims{
				UserID: "user-1", Role: "user", Purpose: ExternalPlaybackTokenPurpose, MediaID: "other-wrapper",
			},
		},
		{
			name:   "unknown purpose",
			claims: Claims{UserID: "user-1", Role: "user", Purpose: "other", MediaID: "wrapper-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := signStreamTestToken(t, tt.claims)
			r := httptest.NewRequest(http.MethodGet, "http://media.example/api/stream/wrapper-1?token="+token, nil)
			got := svc.withExternalPlaybackTokenForInternalRedirect(
				"/api/stream/source-1?quality=source&token=stored-token&api_key=stored-key", r, "", "wrapper-1",
			)
			u, err := url.Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			if u.Query().Get("token") != "" || u.Query().Get("api_key") != "" || u.Query().Get("quality") != "source" {
				t.Fatal("unusable scoped credentials reached internal redirect")
			}
		})
	}
}

func TestInternalStreamRedirectRequiresExactPath(t *testing.T) {
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), nil)
	accountToken := signStreamTestToken(t, Claims{UserID: "user-1", Role: "user"})
	r := httptest.NewRequest(http.MethodGet, "http://media.example/api/stream/wrapper-1?token="+accountToken, nil)
	for _, target := range []string{
		"/api/stream/source-1/?quality=source",
		"/api/stream/source-1/extra?quality=source",
	} {
		t.Run(target, func(t *testing.T) {
			if got := svc.withExternalPlaybackTokenForInternalRedirect(target, r, "", "wrapper-1"); got != target {
				t.Fatal("non-exact stream target changed")
			}
		})
	}
}

func TestCrossSchemeAbsoluteInternalURLDoesNotReceivePlaybackToken(t *testing.T) {
	cfg := &config.Config{Secrets: config.SecretsConfig{JWTSecret: streamTestJWTSecret}}
	svc := NewStreamService(cfg, zap.NewNop(), nil)
	accountToken := signStreamTestToken(t, Claims{UserID: "user-1", Role: "user"})
	r := httptest.NewRequest(http.MethodGet, "https://media.example/Videos/wrapper-1/stream?token="+accountToken, nil)
	target := "http://media.example/api/stream/source-1?quality=source"
	if got := svc.withExternalPlaybackTokenForInternalRedirect(target, r, "", "wrapper-1"); got != target {
		t.Fatal("cross-scheme target changed")
	}
}

func TestServeFileRedirectsExternalHTTPSTRMURLUnchanged(t *testing.T) {
	repos := newStreamTestRepo(t)
	target := "https://cdn.example.test/%E5%AF%92%E6%88%98.mkv?quality=source"
	if err := repos.DB.Create(&model.Media{
		Base:      model.Base{ID: "remote-http"},
		Title:     "Remote HTTP",
		Path:      "/media/寒战.strm",
		Container: "strm",
		STRMURL:   target,
	}).Error; err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/remote-http?token=jwt123", nil)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "remote-http"); err != nil {
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
	if fields["playback_source"] != "remote_redirect" ||
		fields["target_scheme"] != "https" || fields["target_host"] != "cdn.example.test" || fields["target_path"] != "/寒战.mkv" ||
		fmt.Sprint(fields["target_query_keys"]) != "[quality]" {
		t.Fatalf("unexpected redirect log fields: %#v", fields)
	}
	loggedTarget := fmt.Sprint(fields["target_scheme"], fields["target_host"], fields["target_path"], fields["target_query_keys"])
	if strings.Contains(loggedTarget, "source") || fields["target_hash"] == "" {
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
	svc := NewStreamService(&config.Config{}, zap.New(core), repos)
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
	localPath := "/mnt/media/archive/电影/测试 影片 (2026).mkv"
	mappings := strings.Join([]string{
		"/mnt/media => https://fallback.example.test/files/",
		"/mnt/media/archive/ => https://cdn.example.test/media/",
	}, "\n")
	if err := repos.Setting.Set(t.Context(), PlaybackPathMappingsSettingKey, mappings); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{Base: model.Base{ID: "mapped-local"}, Path: localPath}).Error; err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.InfoLevel)
	svc := NewStreamService(&config.Config{}, zap.New(core), repos)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/stream/mapped-local", nil)
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
		location.Path != "/media/电影/测试 影片 (2026).mkv" {
		t.Fatalf("unexpected mapped redirect: %q", location.String())
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("mapped redirect Cache-Control = %q, want no-store", got)
	}
	entries := observed.FilterMessage("media playback redirect").All()
	if len(entries) != 1 {
		t.Fatalf("redirect log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["playback_source"] != "remote_redirect" || fields["resolve_source"] != "path_mapping" ||
		fields["path"] != localPath || fields["target_host"] != "cdn.example.test" ||
		fields["target_path"] != "/media/电影/测试 影片 (2026).mkv" {
		t.Fatalf("unexpected mapped redirect log fields: %#v", fields)
	}
}

func TestServeFileRedirectsMappedLocalSTRMTarget(t *testing.T) {
	repos := newStreamTestRepo(t)
	localTarget := "/mnt/media/archive/电影/测试影片 (2026).mp4"
	if err := repos.Setting.Set(t.Context(), PlaybackPathMappingsSettingKey,
		"/mnt/media/archive/ => https://cdn.example.test/media/"); err != nil {
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
	svc := NewStreamService(&config.Config{}, zap.New(core), repos)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, httptest.NewRequest(http.MethodGet, "/api/stream/mapped-local-strm", nil), "mapped-local-strm"); err != nil {
		t.Fatal(err)
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound || location.Host != "cdn.example.test" ||
		location.Path != "/media/电影/测试影片 (2026).mp4" {
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
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos)
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
	target := "https://cdn.example.test/LocalMovie.mkv?quality=source"
	if err := repos.DB.Create(&model.Media{
		Base:      model.Base{ID: "local-strm"},
		Title:     "Local STRM",
		Path:      "D:/media/LocalMovie.strm",
		Container: "strm",
		STRMURL:   target,
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos)
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
	svc := NewStreamService(&config.Config{}, zap.New(core), repos)
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
	prober := &recordingLocalPlaybackProber{probe: &ProbeResult{
		DurationSec: 120, Width: 1920, Height: 1080, VideoCodec: "h264", AudioCodec: "aac", Container: "matroska,webm",
	}}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos)

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

func newStreamTestRepo(t *testing.T) *repository.Container {
	t.Helper()
	db := newServiceTestDB(t, &model.Media{}, &model.Setting{})
	return repository.New(db)
}

func signStreamTestToken(t *testing.T, claims Claims) string {
	t.Helper()
	now := time.Now()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		Issuer:    "mediastationgo",
		Subject:   claims.UserID,
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(streamTestJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func parseStreamTestToken(t *testing.T, raw string) *Claims {
	t.Helper()
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (interface{}, error) {
		return []byte(streamTestJWTSecret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("parse playback token: %v", err)
	}
	return claims
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
