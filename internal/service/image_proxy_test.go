package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestImageProxyServesLocalImagePath(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "episode-thumb.png")
	if err := os.WriteFile(imagePath, transparent1x1PNG, 0o644); err != nil {
		t.Fatal(err)
	}

	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(dir, "cache")}}, zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/api/img", nil)
	rec := httptest.NewRecorder()

	if err := proxy.Serve(t.Context(), rec, req, imagePath); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got == "" {
		t.Fatal("missing content-type")
	}
	if rec.Body.Len() != len(transparent1x1PNG) {
		t.Fatalf("body length = %d, want %d", rec.Body.Len(), len(transparent1x1PNG))
	}
}
