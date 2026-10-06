package repository

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestHongGuoSearchBatchCommitBoundaries(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	ctx := t.Context()
	for _, row := range []any{
		&model.Library{Base: model.Base{ID: "library"}, Name: "batch", Path: "/batch", Type: model.LibraryTypeHongGuo},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work"}, SourceID: "12345678901", Kind: "series", Title: "Target"},
		&model.HongGuoEpisode{WorkID: "work", Number: 1},
	} {
		if err := repos.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	backend := &hongGuoSearchTestBackend{}
	repos.HongGuo.SetSearchBackend(backend)
	if _, err := repos.HongGuo.BackfillSearchIndex(ctx, 10, 0); err != nil {
		t.Fatal(err)
	}
	writer, flush := repos.Media.WithBatchedHongGuoSearch(2)
	file := func(i int) *model.Media {
		return &model.Media{LibraryID: "library", CatalogSource: "hongguo", LookupCatalogID: "12345678901", Path: fmt.Sprintf("/batch/%d.mkv", i), SeasonNum: 1, EpisodeNum: 1}
	}
	if err := writer.Upsert(ctx, file(1)); err != nil || len(backend.upserts) != 0 {
		t.Fatalf("first commit err=%v writes=%v", err, backend.upserts)
	}
	if err := repos.Setting.Set(ctx, "hongguo.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Upsert(ctx, file(2)); err == nil || len(backend.upserts) != 0 {
		t.Fatal("rolled back file was published or counted toward the batch")
	}
	flush(ctx)
	if len(backend.upserts) != 1 {
		t.Fatalf("partial commit flush writes=%v", backend.upserts)
	}
	var count int64
	if err := repos.DB.Model(&model.Media{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("committed files=%d err=%v", count, err)
	}
	if err := repos.Setting.Set(ctx, "hongguo.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 3; i++ {
		if err := writer.Upsert(ctx, file(i)); err != nil {
			t.Fatal(err)
		}
	}
	flush(ctx)
	if len(backend.upserts) != 2 {
		t.Fatalf("full batch should publish once, writes=%v", backend.upserts)
	}
	if err := repos.Media.Upsert(ctx, file(4)); err != nil || len(backend.upserts) != 3 {
		t.Fatalf("direct writer lost immediate refresh err=%v writes=%v", err, backend.upserts)
	}
	if err := writer.Upsert(ctx, file(5)); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	repos.HongGuo.searchRebuild = true
	flush(cancelled)
	if !repos.HongGuo.searchFailed.Load() || !repos.HongGuo.searchRebuildInvalid || len(backend.upserts) != 3 {
		t.Fatal("cancelled partial publication did not block stale index/rebuild")
	}
}

func TestHongGuoSearchBatchRefreshesOldAndCurrentAlbums(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	ctx := t.Context()
	for _, row := range []any{
		&model.Library{Base: model.Base{ID: "library"}, Name: "batch", Path: "/batch", Type: model.LibraryTypeHongGuo},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "old"}, SourceID: "12345678901", Kind: "series", Title: "Old", RelatedAlbumID: "old-album", SeasonIndex: 1},
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "new"}, SourceID: "12345678902", Kind: "series", Title: "New", RelatedAlbumID: "new-album", SeasonIndex: 1},
		&model.HongGuoEpisode{WorkID: "old", Number: 1},
		&model.HongGuoEpisode{WorkID: "new", Number: 1},
	} {
		if err := repos.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	backend := &recordingMetadataSearchBackend{}
	repos.HongGuo.SetSearchBackend(backend)
	file := model.Media{LibraryID: "library", CatalogSource: "hongguo", LookupCatalogID: "12345678901", Path: "/batch/file.mkv", SeasonNum: 1, EpisodeNum: 1}
	if err := repos.Media.Upsert(ctx, &file); err != nil {
		t.Fatal(err)
	}
	backend.upserts, backend.deletes = nil, nil
	writer, flush := repos.Media.WithBatchedHongGuoSearch(100)
	file.LookupCatalogID = "12345678902"
	if err := writer.Upsert(ctx, &file); err != nil {
		t.Fatal(err)
	}
	// 捕获后合集发生变化，刷新必须同时保留旧身份并读取当前投影。
	if err := repos.DB.Model(&model.HongGuoWork{}).Where("id = ?", "new").Update("related_album_id", "current-album").Error; err != nil {
		t.Fatal(err)
	}
	flush(ctx)
	if len(backend.upserts) != 1 || backend.upserts[0].ID != "hg-group-current-album" || !reflect.DeepEqual(backend.upserts[0].LibraryIDs, []string{"library"}) {
		t.Fatalf("current document=%v", backend.upserts)
	}
	for _, id := range []string{"hg-work-old", "hg-work-new", "hg-group-old-album", "hg-group-new-album"} {
		if !slices.Contains(backend.deletes, id) {
			t.Fatalf("missing old identity %q: %v", id, backend.deletes)
		}
	}
}
