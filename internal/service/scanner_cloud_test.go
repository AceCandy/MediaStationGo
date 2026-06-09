package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestScanCloudLibraryImportsRecursivePlayableMedia(t *testing.T) {
	empty := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file/sort" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if empty {
			_, _ = w.Write([]byte(`{"status":200,"code":0,"data":{"list":[]}}`))
			return
		}
		switch r.URL.Query().Get("pdir_fid") {
		case "0":
			_, _ = w.Write([]byte(`{"status":200,"code":0,"data":{"list":[
				{"fid":"d1","file_name":"Movies","dir":true,"size":0},
				{"fid":"f1","file_name":"Root.Movie.2024.mkv","dir":false,"size":123}
			]}}`))
		case "d1":
			_, _ = w.Write([]byte(`{"status":200,"code":0,"data":{"list":[
				{"fid":"f2","file_name":"Nested.Show.S01E02.mp4","dir":false,"size":456}
			]}}`))
		default:
			t.Fatalf("unexpected pdir_fid %q", r.URL.Query().Get("pdir_fid"))
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "quark",
		Config: map[string]any{
			"cookie": "kps=test",
			"base":   upstream.URL,
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "夸克网盘", Path: "cloud://quark/0", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud: %v", err)
	}
	if res.Visited != 2 || res.Added != 2 {
		t.Fatalf("scan result = %#v, want visited=2 added=2", res)
	}
	var rows []model.Media
	if err := repos.DB.Order("path").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("media rows = %d, want 2: %#v", len(rows), rows)
	}
	if rows[0].Path != "cloud://quark/f1" || rows[0].STRMURL != "/api/cloud/play/quark?ref=f1" {
		t.Fatalf("root media path/strm wrong: path=%q strm=%q", rows[0].Path, rows[0].STRMURL)
	}
	if rows[1].SeasonNum != 1 || rows[1].EpisodeNum != 2 || !strings.Contains(rows[1].STRMURL, "ref=f2") {
		t.Fatalf("nested episode metadata wrong: %#v", rows[1])
	}

	empty = true
	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("rescan cloud: %v", err)
	}
	if res.Removed != 2 {
		t.Fatalf("removed = %d, want 2", res.Removed)
	}
	if got := countMedia(t, repos); got != 0 {
		t.Fatalf("media count after prune = %d, want 0", got)
	}
}

func TestCloudLibraryPathParsing(t *testing.T) {
	typ, dir, ok := parseCloudLibraryPath("cloud://cloud115/abc%20123?ignored=1")
	if !ok || typ != "cloud115" || dir != "abc 123" {
		t.Fatalf("parse path got typ=%q dir=%q ok=%v", typ, dir, ok)
	}
	typ, dir, ok = parseCloudLibraryPath("cloud://quark?dir=0")
	if !ok || typ != "quark" || dir != "0" {
		t.Fatalf("parse query got typ=%q dir=%q ok=%v", typ, dir, ok)
	}
	if ref := cloudEntryRef("cloud115", "fid", "pick"); ref != "pick" {
		t.Fatalf("115 ref = %q, want pick", ref)
	}
}

func TestScanCloudLibraryReadsRemoteSTRMTarget(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			if r.URL.Path != "/dav/Links" {
				t.Fatalf("unexpected propfind path %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/dav/Links/</d:href>
    <d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/Links/Movie.strm</d:href>
    <d:propstat><d:prop><d:displayname>Movie.strm</d:displayname><d:getcontentlength>32</d:getcontentlength><d:resourcetype/></d:prop></d:propstat>
  </d:response>
</d:multistatus>`))
		case http.MethodGet:
			if r.URL.Path != "/dav/Links/Movie.strm" {
				t.Fatalf("unexpected get path %s", r.URL.Path)
			}
			_, _ = w.Write([]byte("https://cdn.example.com/Movie.mkv\n"))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"url": upstream.URL,
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList · Links", Path: "cloud://openlist/Links", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("scan result = %#v, want added=1", res)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.Path != "cloud://openlist/Links/Movie.strm" {
		t.Fatalf("path = %q", media.Path)
	}
	if media.STRMURL != "https://cdn.example.com/Movie.mkv" {
		t.Fatalf("strm target = %q", media.STRMURL)
	}
}
