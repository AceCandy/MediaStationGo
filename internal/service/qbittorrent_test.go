package service

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestQBitAddTorrentRequiresVisibleNewTask(t *testing.T) {
	oldAttempts := qbitAddVerifyAttempts
	oldInterval := qbitAddVerifyInterval
	qbitAddVerifyAttempts = 2
	qbitAddVerifyInterval = time.Millisecond
	defer func() {
		qbitAddVerifyAttempts = oldAttempts
		qbitAddVerifyInterval = oldInterval
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte("[]"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})

	err := client.AddTorrent(context.Background(), server.URL+"/missing.torrent", "")
	if err == nil {
		t.Fatal("expected add to fail when no new torrent appears")
	}
	if !strings.Contains(err.Error(), "下载器未出现新任务") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQBitAddTorrentUploadsFetchedTorrentFile(t *testing.T) {
	var added atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/fixture.torrent":
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write([]byte("d4:infod4:name7:fixtureee"))
		case "/api/v2/torrents/add":
			reader, err := r.MultipartReader()
			if err != nil {
				t.Errorf("expected multipart add request: %v", err)
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			if !multipartHasTorrentFile(reader) {
				t.Error("expected qbit add request to upload torrent file")
				http.Error(w, "missing file", http.StatusBadRequest)
				return
			}
			added.Store(true)
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if added.Load() {
				_, _ = w.Write([]byte(`[{"hash":"abc123","name":"fixture"}]`))
				return
			}
			_, _ = w.Write([]byte("[]"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})

	if err := client.AddTorrent(context.Background(), server.URL+"/fixture.torrent", ""); err != nil {
		t.Fatalf("expected fetched torrent upload to succeed: %v", err)
	}
}

func TestQBitAddTorrentFileTreatsExistingInfoHashAsSuccess(t *testing.T) {
	torrentData := []byte("d4:infod4:name7:fixtureee")
	hash := torrentInfoHash(torrentData)
	if hash == "" {
		t.Fatal("expected fixture info hash")
	}
	var addCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			addCalled.Store(true)
			_, _ = w.Write([]byte("Fails."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[{"hash":"` + hash + `","name":"fixture"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})

	if err := client.AddTorrentFile(context.Background(), torrentData, "fixture.torrent", ""); err != nil {
		t.Fatalf("expected existing torrent to be accepted: %v", err)
	}
	if addCalled.Load() {
		t.Fatal("expected qbit add to be skipped for existing infohash")
	}
}

func multipartHasTorrentFile(reader *multipart.Reader) bool {
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return false
		}
		if part.FormName() == "torrents" && part.FileName() != "" {
			return true
		}
	}
}
