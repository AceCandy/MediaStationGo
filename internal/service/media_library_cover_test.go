package service

import (
	"bytes"
	"errors"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm/logger"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestLibraryCoverUploadServeAndClear(t *testing.T) {
	dataDir := t.TempDir()
	db := newServiceTestDB(t, &model.Library{})
	repos := repository.New(db)
	svc := NewMediaService(&config.Config{App: config.AppConfig{DataDir: dataDir}}, zap.NewNop(), repos)
	lib := model.Library{Name: "电影", Type: "movie", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SaveLibraryCover(t.Context(), lib.ID, strings.NewReader("not an image")); !errors.Is(err, ErrInvalidLibraryCover) {
		t.Fatalf("invalid upload error = %v", err)
	}
	if _, err := svc.SaveLibraryCover(t.Context(), lib.ID, bytes.NewReader(make([]byte, maxArtworkBytes+1))); !errors.Is(err, ErrInvalidLibraryCover) {
		t.Fatalf("oversized upload error = %v", err)
	}
	updated, err := svc.SaveLibraryCover(t.Context(), lib.ID, bytes.NewReader(testArtworkPNG(t, 16, 9)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(updated.CoverURL, libraryCoverURL(lib.ID)+"?v=") {
		t.Fatalf("cover URL = %q", updated.CoverURL)
	}
	version, ok := libraryCoverVersion(updated.CoverURL, lib.ID)
	if !ok {
		t.Fatalf("invalid stored cover URL = %q", updated.CoverURL)
	}
	path, err := svc.libraryCoverPath(lib.ID, version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stored cover: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", libraryCoverURL(lib.ID), nil)
	var queries bytes.Buffer
	originalLogger := db.Logger
	db.Logger = logger.New(log.New(&queries, "", 0), logger.Config{LogLevel: logger.Info})
	if err := svc.ServeLibraryCover(t.Context(), recorder, request, lib.ID); err != nil {
		t.Fatal(err)
	}
	db.Logger = originalLogger
	if sql := queries.String(); strings.Count(sql, "SELECT") != 1 || strings.Contains(sql, "library_roots") || !strings.Contains(sql, "cover_url") {
		t.Fatalf("cover should only select its URL: %s", sql)
	}
	if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("served cover: status=%d type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	cleared, err := svc.ClearLibraryCover(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.CoverURL != "" {
		t.Fatalf("cleared cover URL = %q", cleared.CoverURL)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleared cover file error = %v", err)
	}
	for _, id := range []string{lib.ID, "00000000-0000-0000-0000-000000000000"} {
		if err := svc.ServeLibraryCover(t.Context(), httptest.NewRecorder(), request, id); !errors.Is(err, ErrLibraryCoverNotFound) {
			t.Fatalf("missing cover error = %v", err)
		}
	}
}
