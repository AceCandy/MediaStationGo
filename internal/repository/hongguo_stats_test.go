package repository

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoConcurrentProgressAndRollback(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.HongGuoUserState{}, &model.HongGuoPlaybackEvent{}); err != nil {
		t.Fatal(err)
	}
	r, ctx := New(db), t.Context()
	view := model.MediaView{Media: model.Media{PermanentBase: model.PermanentBase{ID: "source-file"}, LibraryID: "source-library", CatalogSource: "hongguo", LookupCatalogID: "900000000000000001", EpisodeNum: 1}}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- r.HongGuo.RecordProgress(ctx, "user", "same-session", view, 30000, 120000, false)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&model.HongGuoPlaybackEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("event dedup: %d %v", count, err)
	}
	if err := r.HongGuo.RecordProgress(ctx, "user", strings.Repeat("x", 129), view, 60000, 120000, false); err == nil {
		t.Fatal("expected event insert failure")
	}
	state, err := r.HongGuo.UserState(ctx, "user", view.LookupCatalogID, 1)
	if err != nil || state.PositionMs != 30000 {
		t.Fatalf("failed event did not roll back state: %+v %v", state, err)
	}
}

func TestHongGuoPlaybackStatistics(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	r, ctx := New(db), t.Context()
	lib := model.Library{Name: "红果", Path: "/hg", Type: model.LibraryTypeHongGuo}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	w, err := r.HongGuo.SaveDetail(ctx, hongguo.Work{SourceID: "900000000000000001", Title: "源剧", EpisodeCount: 2, Snapshot: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	m := model.Media{LibraryID: lib.ID, Path: "/hg/1.strm", CatalogSource: "hongguo", LookupCatalogID: w.SourceID, SeasonNum: 1, EpisodeNum: 1}
	if err := r.Media.Upsert(ctx, &m); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	for i, user := range []string{"a", "a", "b"} {
		event := model.HongGuoPlaybackEvent{UserID: user, SessionID: string(rune('a' + i)), SourceID: w.SourceID, EpisodeNumber: 1, MediaID: m.ID, LibraryID: lib.ID, PlayedAt: now.Add(time.Duration(i) * time.Minute)}
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	f := PlaybackStatsFilter{Grain: "day", From: now.Add(-time.Hour), To: now.Add(time.Hour), TimeZone: "UTC", Page: 1, PageSize: 1, RankGrain: "day", RankPeriod: "2026-09-13", RankFrom: now, RankTo: now.Add(time.Hour)}
	stats, err := r.HongGuo.PlaybackStats(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 3 || len(stats.Buckets) != 1 || stats.Buckets[0].Count != 3 || len(stats.Details.Items) != 1 || stats.Details.Items[0].UserID != "b" || !stats.Details.Items[0].MediaAvailable || len(stats.Ranking.Items) != 1 || stats.Ranking.Items[0].Count != 3 {
		t.Fatalf("wrong statistics: %+v", stats)
	}
	if err := r.HongGuo.SaveAlbum(ctx, w.SourceID, hongguo.Album{ID: "9000000000000000099", Season: 3}); err != nil {
		t.Fatal(err)
	}
	f.UserID, f.MediaType, f.LibraryIDs = "a", "tv", []string{lib.ID}
	f.RankTo = now.Add(time.Minute)
	stats, err = r.HongGuo.PlaybackStats(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 || stats.Details.Items[0].SeasonNum != 3 || stats.Details.Items[0].SeriesTitle != w.Title || stats.Ranking.Items[0].Count != 1 || stats.Ranking.Items[0].GroupID != "hongguo-"+w.SourceID {
		t.Fatalf("filters or stable grouping: %+v", stats)
	}
	if err := db.Delete(&m).Error; err != nil {
		t.Fatal(err)
	}
	stats, err = r.HongGuo.PlaybackStats(ctx, f)
	if err != nil || stats.Total != 2 || stats.Details.Items[0].MediaAvailable {
		t.Fatalf("deleted media: %+v %v", stats, err)
	}
	f.Page = 4
	stats, err = r.HongGuo.PlaybackStats(ctx, f)
	if err != nil || len(stats.Details.Items) != 0 || stats.Total != 2 {
		t.Fatalf("empty page: %+v %v", stats, err)
	}
	f.Page, f.MediaType = 1, "movie"
	stats, err = r.HongGuo.PlaybackStats(ctx, f)
	if err != nil || stats.Total != 0 {
		t.Fatalf("movie filter: %+v %v", stats, err)
	}
	legacy, err := r.PlaybackEvent.Stats(ctx, f)
	if err != nil || legacy.Total != 0 {
		t.Fatalf("source events leaked to legacy: %+v %v", legacy, err)
	}
}
