package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestDownloadViewsDoNotExposePrivateURL(t *testing.T) {
	rows := []model.DownloadTask{{
		UserID:   "u1",
		Source:   "qbittorrent",
		URL:      "https://tracker.example/download?id=1&passkey=private-token",
		Title:    "测试影片",
		SavePath: "/downloads",
		Status:   "queued",
	}}

	tasks, torrents := DownloadViews(rows, nil)
	data, err := json.Marshal(map[string]any{
		"tasks":    tasks,
		"torrents": torrents,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if strings.Contains(body, "private-token") || strings.Contains(body, "passkey") || strings.Contains(body, "tracker.example") {
		t.Fatalf("download views leaked private URL: %s", body)
	}
	if !strings.Contains(body, "测试影片") {
		t.Fatalf("download views should keep public title: %s", body)
	}
}

func TestPublicDownloadTitleUsesMagnetDisplayName(t *testing.T) {
	got := publicDownloadTitle("magnet:?xt=urn:btih:abc&dn=%E6%B5%8B%E8%AF%95%E5%BD%B1%E7%89%87")
	if got != "测试影片" {
		t.Fatalf("publicDownloadTitle = %q, want %q", got, "测试影片")
	}
}

func TestAddDownloadWithMetaSkipsExistingTaskBeforeQBAdd(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadTask{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	existing := &model.DownloadTask{
		UserID:   "u1",
		Source:   "qbittorrent",
		URL:      "https://pt.example/download?id=old&passkey=old",
		Title:    "Some Show S01E01 1080p",
		SavePath: "/downloads/tv",
		Status:   "completed",
		Progress: 1,
	}
	if err := repos.Download.Create(t.Context(), existing); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc.qb.Configure(QBitConfig{BaseURL: qb.URL, Username: "admin", Password: "admin"})
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "https://pt.example/download?id=new&passkey=new", "/downloads/tv", DownloadTaskMeta{
		Title: "Some Show S01E01 1080p",
	})
	if !errors.Is(err, ErrDownloadAlreadyExists) {
		t.Fatalf("err = %v, want ErrDownloadAlreadyExists", err)
	}
	if task == nil || task.ID != existing.ID {
		t.Fatalf("task = %#v, want existing task %#v", task, existing)
	}
	if got := atomic.LoadInt32(&addCalls); got != 0 {
		t.Fatalf("qb add calls = %d, want 0", got)
	}
}

func TestReloadConfigDoesNotFallbackToLegacyAfterClientDeleted(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if atomic.LoadInt32(&addCalls) > 0 {
				_, _ = w.Write([]byte(`[{"hash":"abc123","name":"Movie 2026 1080p","state":"downloading","progress":0.1}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadClient{}, &model.DownloadTask{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), "qbittorrent.url", qb.URL); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "qbittorrent.username", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "qbittorrent.password", "admin"); err != nil {
		t.Fatal(err)
	}
	client := &model.DownloadClient{Name: "qB", Type: "qbittorrent", Host: qb.URL, Username: "admin", Password: "admin", IsDefault: true, Enabled: true}
	if err := repos.DownloadClient.Create(t.Context(), client); err != nil {
		t.Fatal(err)
	}
	if err := repos.DownloadClient.Delete(t.Context(), client.ID); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	if err := svc.ReloadConfig(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=Movie+2026+1080p", "/downloads", DownloadTaskMeta{
		Title: "Movie 2026 1080p",
	})
	if err == nil {
		t.Fatal("expected add to fail when the configured downloader was deleted")
	}
	if got := atomic.LoadInt32(&addCalls); got != 0 {
		t.Fatalf("qb add calls = %d, want 0", got)
	}
}

func TestReloadConfigDoesNotFallbackToLegacyAfterClientDisabled(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadClient{}, &model.DownloadTask{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), "qbittorrent.url", qb.URL); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "qbittorrent.username", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "qbittorrent.password", "admin"); err != nil {
		t.Fatal(err)
	}
	client := &model.DownloadClient{Name: "qB", Type: "qbittorrent", Host: qb.URL, Username: "admin", Password: "admin", IsDefault: true, Enabled: true}
	if err := repos.DownloadClient.Create(t.Context(), client); err != nil {
		t.Fatal(err)
	}
	client.Enabled = false
	if err := repos.DownloadClient.Update(t.Context(), client); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	if err := svc.ReloadConfig(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&dn=Movie+2026+1080p", "/downloads", DownloadTaskMeta{
		Title: "Movie 2026 1080p",
	})
	if err == nil {
		t.Fatal("expected add to fail when the configured downloader was disabled")
	}
	if got := atomic.LoadInt32(&addCalls); got != 0 {
		t.Fatalf("qb add calls = %d, want 0", got)
	}
}
