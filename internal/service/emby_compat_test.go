package service

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

func newTestEmbyService(t *testing.T) *EmbyService {
	t.Helper()
	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Favorite{}, &model.PlaybackHistory{}, &model.User{}, &model.Setting{})
	// 内存库 + 异步探测协程：限制为单连接，避免连接池新建连接时
	// 拿到一个空白的 :memory: 实例（no such table）。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	repos := repository.New(db)
	return NewEmbyService(&config.Config{}, zap.NewNop(), repos)
}

func TestEmbyLatestItemsOrderByReleaseDate(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	base := time.Now()
	testRows := []struct {
		id, title, path, releaseDate string
		createdAt                    time.Time
	}{
		{"older-release-newer-scan", "旧上映新入库", `/media/movies/old.mkv`, "2026-01-10", base.Add(2 * time.Hour)},
		{"newer-release-older-scan", "新上映", `/media/movies/new.mkv`, "2026-06-23", base},
	}
	for _, row := range testRows {
		metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
			Base: model.Base{ID: "metadata-" + row.id}, Kind: model.MetadataKindMovie,
			Title: row.title, Year: 2026, ReleaseDate: row.releaseDate, Source: "tmdb",
		})
		media := model.Media{
			Base: model.Base{ID: row.id, CreatedAt: row.createdAt}, LibraryID: lib.ID, MetadataID: metadata.ID,
			Title: row.title, Path: row.path, Year: 2026, ScrapeStatus: "matched",
		}
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	items, err := svc.LatestItems(t.Context(), "", lib.ID, 10)
	if err != nil {
		t.Fatalf("latest items: %v", err)
	}
	if len(items) != 2 || items[0]["Id"] != "metadata-newer-release-older-scan" {
		t.Fatalf("latest items should prefer release date over created_at, got %#v", items)
	}
	if _, ok := items[0]["PremiereDate"].(time.Time); !ok {
		t.Fatalf("latest item should expose PremiereDate for Emby clients: %#v", items[0])
	}
}

type fakeCloudPlaybackResolver struct {
	link *cloud.DirectLink
	typ  string
	ref  string
	ua   string
}

func (f *fakeCloudPlaybackResolver) CloudResolve(_ context.Context, typ, fileRef, clientUA string) (*cloud.DirectLink, error) {
	f.typ = typ
	f.ref = fileRef
	f.ua = clientUA
	return f.link, nil
}

type fakeCloudPlaybackProber struct {
	probe   *ProbeResult
	rawURL  string
	headers map[string]string
}

func (f *fakeCloudPlaybackProber) ProbeHTTP(_ context.Context, rawURL string, headers map[string]string) (*ProbeResult, error) {
	f.rawURL = rawURL
	f.headers = headers
	return f.probe, nil
}
