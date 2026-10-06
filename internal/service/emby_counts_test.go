package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestEmbyItemCountsWorksAndAllFiles(t *testing.T) {
	e := newTestEmbyService(t)
	db := e.repo.DB
	// 本用例显式建立各来源绑定，避免旧夹具为独立来源自动补普通元数据。
	if err := db.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	visible := model.Library{Name: "Visible", Path: "/synthetic/visible", Type: "tv"}
	hidden := model.Library{Name: "Hidden", Path: "/synthetic/hidden", Type: "tv"}
	for _, lib := range []*model.Library{&visible, &hidden} {
		if err := db.Create(lib).Error; err != nil {
			t.Fatal(err)
		}
	}
	create := func(row any) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	addWork := func(source, kind, suffix, libraryID string, versions int) {
		t.Helper()
		id := source + "-" + kind + "-" + suffix
		base := model.PermanentBase{ID: id}
		itemID := id
		switch source {
		case "ordinary":
			create(&model.MetadataItem{PermanentBase: base, Kind: kind, Title: id})
			if kind == "series" {
				seasonID := id + "-season"
				create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: seasonID}, Kind: "season", ParentID: &id, Title: seasonID})
				itemID = id + "-episode"
				create(&model.MetadataItem{PermanentBase: model.PermanentBase{ID: itemID}, Kind: "episode", ParentID: &seasonID, EpisodeNum: 1, Title: itemID})
			}
		case "nfo":
			create(&model.NFOItem{PermanentBase: base, LibraryID: libraryID, LocalKey: id, Kind: kind, NFOFields: model.NFOFields{Title: id}})
			if kind == "series" {
				seasonID := id + "-season"
				create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: seasonID}, LibraryID: libraryID, LocalKey: seasonID, Kind: "season", ParentID: &id})
				itemID = id + "-episode"
				create(&model.NFOItem{PermanentBase: model.PermanentBase{ID: itemID}, LibraryID: libraryID, LocalKey: itemID, Kind: "episode", ParentID: &seasonID})
			}
		case "hongguo":
			create(&model.HongGuoWork{PermanentBase: base, SourceID: id, Kind: kind, Title: id, RelatedAlbumID: "same-album", SeasonIndex: 1})
			itemID = id + "-episode"
			create(&model.HongGuoEpisode{PermanentBase: model.PermanentBase{ID: itemID}, WorkID: id, Number: 1})
		case "huangguoai":
			create(&model.HuangGuoAIWork{PermanentBase: base, SourceID: id, SourceCategory: "ai-duanju", Kind: kind, Title: id})
			itemID = id + "-episode"
			create(&model.HuangGuoAIEpisode{PermanentBase: model.PermanentBase{ID: itemID}, WorkID: id, Number: 1, PagePath: "/synthetic"})
		}
		for i := 0; i < versions; i++ {
			m := model.Media{PermanentBase: model.PermanentBase{ID: fmt.Sprintf("%s-file-%d", id, i)}, LibraryID: libraryID, Path: fmt.Sprintf("/synthetic/%s-%d.mp4", id, i), CatalogSource: source, PartGroupKey: id, PartIndex: i + 1}
			if kind == "series" {
				m.SeasonNum, m.EpisodeNum = 1, 1
			}
			if source == "ordinary" {
				m.CatalogSource, m.MetadataID = "", itemID
			}
			create(&m)
			switch source {
			case "nfo":
				create(&model.NFOMediaBinding{MediaID: m.ID, ItemID: itemID})
			case "hongguo":
				create(&model.HongGuoMediaBinding{MediaID: m.ID, WorkID: id, EpisodeID: &itemID})
			case "huangguoai":
				create(&model.HuangGuoAIMediaBinding{MediaID: m.ID, WorkID: id, EpisodeID: itemID})
			}
		}
	}
	for _, source := range []string{"ordinary", "nfo", "hongguo", "huangguoai"} {
		addWork(source, "movie", "visible", visible.ID, 2)
		addWork(source, "series", "visible", visible.ID, 2)
		addWork(source, "movie", "hidden", hidden.ID, 1)
		addWork(source, "movie", "fileless", visible.ID, 0)
	}
	// 同一作品在另一媒体库也有版本：作品仍只计一次，文件按库分别计数。
	for _, source := range []string{"ordinary", "hongguo", "huangguoai"} {
		workID := source + "-movie-visible"
		m := model.Media{PermanentBase: model.PermanentBase{ID: source + "-cross-library"}, LibraryID: hidden.ID, Path: "/synthetic/" + source + "-cross-library.mp4", CatalogSource: source}
		if source == "ordinary" {
			m.CatalogSource, m.MetadataID = "", workID
		}
		create(&m)
		epID := workID + "-episode"
		switch source {
		case "hongguo":
			create(&model.HongGuoMediaBinding{MediaID: m.ID, WorkID: workID, EpisodeID: &epID})
		case "huangguoai":
			create(&model.HuangGuoAIMediaBinding{MediaID: m.ID, WorkID: workID, EpisodeID: epID})
		}
	}
	// 两个源作品属于同一官方合集，数量统计仍分别计数。
	addWork("hongguo", "series", "second-season", visible.ID, 1)
	// 未绑定资料的文件也属于全部 media 数量。
	if err := db.Exec("INSERT INTO media (id,library_id,path,catalog_source) VALUES (?,?,?,'')", "unbound", visible.ID, "/synthetic/unbound.mp4").Error; err != nil {
		t.Fatal(err)
	}
	for _, unknown := range []bool{false, true} {
		if unknown {
			for _, table := range []string{"metadata_items", "hongguo_works", "huangguoai_works"} {
				if err := db.Exec("UPDATE " + table + " SET library_ids=NULL").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Exec("UPDATE nfo_items SET latest_media_added_at=NULL").Error; err != nil {
				t.Fatal(err)
			}
		}
		for _, tc := range []struct {
			name       string
			visibility MediaVisibility
			movies     int64
			series     int
			files      int64
		}{
			{"all", MediaVisibility{IncludeNSFW: true}, 8, 5, 25},
			{"allowed", MediaVisibility{IncludeNSFW: true, LibraryRestricted: true, AllowedLibraryIDs: []string{visible.ID}}, 4, 5, 18},
			{"hidden", MediaVisibility{IncludeNSFW: true, HiddenLibraryIDs: []string{hidden.ID}}, 4, 5, 18},
			{"other", MediaVisibility{IncludeNSFW: true, LibraryRestricted: true, AllowedLibraryIDs: []string{hidden.ID}}, 7, 0, 7},
			{"intersection", MediaVisibility{IncludeNSFW: true, LibraryRestricted: true, AllowedLibraryIDs: []string{visible.ID}, HiddenLibraryIDs: []string{visible.ID}}, 0, 0, 0},
			{"locked", MediaVisibility{IncludeNSFW: true, LibraryRestricted: true}, 0, 0, 0},
		} {
			t.Run(fmt.Sprintf("%s/unknown=%t", tc.name, unknown), func(t *testing.T) {
				e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {visibility: tc.visibility, expiresAt: time.Now().Add(time.Hour)}}
				got, err := e.ItemCounts(t.Context(), "viewer")
				if unknown {
					tc.movies, tc.series = 0, 0
				}
				want := map[string]any{"MovieCount": tc.movies, "SeriesCount": tc.series, "EpisodeCount": tc.files, "ItemCount": tc.movies + int64(tc.series)}
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("counts = %v, %v; want %v", got, err, want)
				}
			})
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := e.ItemCounts(ctx, "viewer"); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("canceled request = %v, %v", got, err)
	}
	if err := db.Exec("ALTER TABLE huangguoai_works RENAME TO unavailable_works").Error; err != nil {
		t.Fatal(err)
	}
	if got, err := e.ItemCounts(t.Context(), "viewer"); err == nil || got != nil {
		t.Fatalf("failed source returned partial counts: %v, %v", got, err)
	}
}
