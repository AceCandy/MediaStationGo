package repository

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestPlaybackStatsAllSystems(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	create := func(row any) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	user := model.User{Username: "stats-user"}
	create(&user)
	libs := []model.Library{{Name: "普通", Type: "movie", Path: "/catalog"}, {Name: "红果", Type: "hongguo", Path: "/hongguo"}, {Name: "非常规", Type: "nfo_tv", Path: "/nfo"}}
	create(&libs)
	movie := model.MetadataItem{Kind: "movie", Title: "同名作品", Source: "test"}
	create(&movie)
	work := model.HongGuoWork{SourceID: "stats-source", Kind: "movie", Title: movie.Title}
	create(&work)
	series := model.NFOItem{LibraryID: libs[2].ID, LocalKey: "series", Kind: "series", NFOFields: model.NFOFields{Title: movie.Title}}
	create(&series)
	season := model.NFOItem{LibraryID: libs[2].ID, LocalKey: "season", Kind: "season", ParentID: &series.ID, SeasonNum: 2, NFOFields: model.NFOFields{Title: "第二季"}}
	create(&season)
	episodes := []model.NFOItem{
		{LibraryID: libs[2].ID, LocalKey: "ep1", Kind: "episode", ParentID: &season.ID, SeasonNum: 2, EpisodeNum: 1, NFOFields: model.NFOFields{Title: "第一集"}},
		{LibraryID: libs[2].ID, LocalKey: "ep2", Kind: "episode", ParentID: &season.ID, SeasonNum: 2, EpisodeNum: 2, NFOFields: model.NFOFields{Title: "第二集"}},
	}
	create(&episodes)
	files := []model.Media{
		{LibraryID: libs[0].ID, Path: "/catalog/a.mkv", MetadataID: movie.ID},
		{LibraryID: libs[1].ID, Path: "/hongguo/a.mkv", CatalogSource: "hongguo"},
		{LibraryID: libs[2].ID, Path: "/nfo/a.mkv", CatalogSource: "nfo"},
		{LibraryID: libs[2].ID, Path: "/nfo/b.mkv", CatalogSource: "nfo"},
	}
	create(&files)
	create(&model.HongGuoMediaBinding{MediaID: files[1].ID, WorkID: work.ID})
	for i := range episodes {
		create(&model.NFOMediaBinding{MediaID: files[2+i].ID, ItemID: episodes[i].ID, Fingerprint: "test"})
	}
	base := time.Date(2026, 9, 20, 15, 50, 0, 0, time.UTC)
	// 三套事件交错跨日；最早三条的时间和跨表 ID 相同，也必须各自保留。
	for i := range 9 {
		at := base.Add(time.Duration(i*2) * time.Minute)
		if i < 3 {
			at = base
		}
		session := fmt.Sprintf("session-%d", i)
		eventID := fmt.Sprintf("event-%d", i/3)
		switch i % 3 {
		case 0:
			create(&model.PlaybackEvent{Base: model.Base{ID: eventID}, UserID: user.ID, SessionID: session, MetadataID: movie.ID, MediaID: files[0].ID, LibraryID: libs[0].ID, PlayedAt: at})
		case 1:
			create(&model.HongGuoPlaybackEvent{PermanentBase: model.PermanentBase{ID: eventID}, UserID: user.ID, SessionID: session, SourceID: work.SourceID, EpisodeNumber: 1, MediaID: files[1].ID, LibraryID: libs[1].ID, PlayedAt: at})
		case 2:
			ep := (i / 3) % 2
			create(&model.NFOPlaybackEvent{PermanentBase: model.PermanentBase{ID: eventID}, UserID: user.ID, SessionID: session, ItemID: episodes[ep].ID, MediaID: files[2+ep].ID, LibraryID: libs[2].ID, PlayedAt: at})
		}
	}
	r := New(db)
	f := PlaybackStatsFilter{Grain: "day", From: base, To: base.Add(time.Hour), TimeZone: "Asia/Shanghai", Page: 1, PageSize: 2, RankGrain: "day", RankPeriod: "2026-09-20", RankFrom: base, RankTo: base.Add(10 * time.Minute)}
	stats, err := r.PlaybackStats(t.Context(), "all", f)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 9 || stats.Details.Total != 9 || len(stats.Buckets) != 2 || stats.Buckets[0].Count != 5 || stats.Buckets[1].Count != 4 {
		t.Fatalf("counts/buckets: %+v", stats)
	}
	if len(stats.Details.Items) != 2 || stats.Details.Items[0].System != "nfo" || stats.Details.Items[1].System != "hongguo" {
		t.Fatalf("merged page: %+v", stats.Details)
	}
	if len(stats.Ranking.Items) != 3 {
		t.Fatalf("same titles merged: %+v", stats.Ranking)
	}
	for _, item := range stats.Ranking.Items {
		want := int64(2)
		if item.System == "nfo" {
			want = 1
			if item.GroupID != "nfo-"+season.ID || item.Title != series.Title || item.SeasonNum != 2 {
				t.Fatalf("NFO season rank: %+v", item)
			}
		}
		if item.Count != want {
			t.Fatalf("rank intersection: %+v", item)
		}
	}
	for _, system := range []string{"catalog", "hongguo", "nfo"} {
		one, err := r.PlaybackStats(t.Context(), system, f)
		if err != nil || one.Total != 3 || one.Details.Items[0].System != system {
			t.Fatalf("system %s: %+v %v", system, one, err)
		}
	}
	seen := map[string]bool{}
	for page := 1; page <= 6; page++ {
		f.Page = page
		part, err := r.PlaybackStats(t.Context(), "all", f)
		if err != nil || part.Total != 9 {
			t.Fatalf("page %d: %+v %v", page, part, err)
		}
		for _, item := range part.Details.Items {
			key := item.System + ":" + item.ID
			if seen[key] {
				t.Fatalf("duplicate on page %d: %s", page, key)
			}
			seen[key] = true
		}
	}
	if len(seen) != 9 {
		t.Fatalf("paged events=%d", len(seen))
	}
	f.Page, f.PageSize, f.MediaType = 1, 20, "tv"
	onlyTV, err := r.PlaybackStats(t.Context(), "all", f)
	if err != nil || onlyTV.Total != 3 || onlyTV.Details.Items[0].System != "nfo" {
		t.Fatalf("TV filter: %+v %v", onlyTV, err)
	}
	f.MediaType, f.LibraryIDs, f.UserID = "", []string{libs[0].ID, libs[2].ID}, user.ID
	filtered, err := r.PlaybackStats(t.Context(), "all", f)
	if err != nil || filtered.Total != 6 {
		t.Fatalf("library/user filter: %+v %v", filtered, err)
	}
	f.UserID = "other-user"
	empty, err := r.PlaybackStats(t.Context(), "all", f)
	if err != nil || empty.Total != 0 || len(empty.Details.Items) != 0 || len(empty.Ranking.Items) != 0 {
		t.Fatalf("other user: %+v %v", empty, err)
	}
	f.UserID, f.LibraryIDs = "", nil
	if err := db.Delete(&files[2]).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.NFOMediaBinding{}).Where("media_id = ?", files[3].ID).Update("item_id", episodes[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	local, err := r.PlaybackStats(t.Context(), "nfo", f)
	if err != nil || local.Total != 3 {
		t.Fatalf("retained events: %+v %v", local, err)
	}
	for _, item := range local.Details.Items {
		if item.MediaAvailable || item.MetadataID != "" {
			t.Fatalf("deleted/rebound file linked: %+v", item)
		}
	}
	if _, err := r.PlaybackStats(t.Context(), "unknown", f); !errors.Is(err, ErrPlaybackStatsSystem) {
		t.Fatalf("invalid system: %v", err)
	}
	for i := range 12 {
		item := model.NFOItem{LibraryID: libs[2].ID, LocalKey: fmt.Sprintf("movie-%d", i), Kind: "movie", NFOFields: model.NFOFields{Title: movie.Title}}
		create(&item)
		create(&model.NFOPlaybackEvent{UserID: user.ID, SessionID: "same-session", ItemID: item.ID, MediaID: "missing-file", LibraryID: libs[2].ID, PlayedAt: base})
	}
	f.MediaType = "movie"
	top, err := r.PlaybackStats(t.Context(), "all", f)
	if err != nil || top.Total != 18 || len(top.Ranking.Items) != 10 || top.Ranking.Items[0].Count != 2 || top.Ranking.Items[1].Count != 2 {
		t.Fatalf("combined Top 10: %+v %v", top, err)
	}
	localMovies, err := r.PlaybackStats(t.Context(), "nfo", f)
	if err != nil || localMovies.Total != 12 || len(localMovies.Ranking.Items) != 10 {
		t.Fatalf("independent duplicate movies: %+v %v", localMovies, err)
	}
}
