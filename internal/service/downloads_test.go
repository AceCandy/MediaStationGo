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
