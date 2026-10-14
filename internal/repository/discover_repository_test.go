package repository

import (
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"testing"
)

func TestDiscoverLibraryItemsUseVisibleFilesAndExactKind(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.MetadataItem{}, &model.MetadataIdentifier{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	ensureSourcePresentationTestSchema(t, db)
	repos := New(db)
	lib := model.Library{Name: "visible", Path: "/visible", Enabled: true}
	hidden := model.Library{Name: "hidden", Path: "/hidden", Enabled: true}
	for _, row := range []*model.Library{&lib, &hidden} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	makeWork := func(kind, id string) *model.MetadataItem {
		return createTestMetadata(t, repos, model.MetadataItem{Kind: kind, Title: "同名作品", Source: "tmdb"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: kind, ExternalID: id})
	}
	movie := makeWork("movie", "101")
	series := makeWork("series", "101")
	makeWork("movie", "102") // 同标题、只有元数据，不能算入库。
	private := makeWork("movie", "103")
	directSeries := makeWork("series", "104")
	seasonSeries := makeWork("series", "105")
	directSeason := createTestMetadata(t, repos, model.MetadataItem{Kind: "season", ParentID: &seasonSeries.ID, SeasonNum: 0, Source: "tmdb"})
	season := createTestMetadata(t, repos, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1, Title: "季", Source: "tmdb"})
	episode := createTestMetadata(t, repos, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 7, Title: "第七集", Source: "tmdb"})
	for _, row := range []model.Media{
		{MetadataID: movie.ID, LibraryID: lib.ID, Path: "/visible/movie.mkv"},
		{MetadataID: episode.ID, LibraryID: lib.ID, Path: "/visible/show.strm", STRMURL: "https://example.com/video.mp4"},
		{MetadataID: private.ID, LibraryID: hidden.ID, Path: "/hidden/private.mkv"},
		{MetadataID: directSeries.ID, LibraryID: lib.ID, Path: "/visible/series.mkv"},
		{MetadataID: directSeason.ID, LibraryID: lib.ID, Path: "/visible/season.mkv"},
		{MetadataID: movie.ID, LibraryID: lib.ID, Path: "/visible/movie-copy.mkv"},
		{LibraryID: lib.ID, Path: "/visible/unresolved.mkv", TMDbID: 102},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 身份及文件存在性检查不应依赖来源展示视图。
	if err := db.Exec("DROP VIEW metadata_presentations CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []DiscoverIdentity{{104, "tv"}, {105, "tv"}} {
		rows, err := repos.MediaView.FindDiscoverLibraryItems(t.Context(), []DiscoverIdentity{id, id}, MediaQueryFilter{})
		if err != nil || len(rows) != 1 || rows[0] != id {
			t.Fatalf("direct hierarchy ownership: got %#v, error %v", rows, err)
		}
	}
	items := []DiscoverIdentity{{101, "movie"}, {101, "tv"}, {102, "movie"}, {103, "movie"}}
	filter := MediaQueryFilter{AllowedLibraryIDs: []string{lib.ID}}
	assertFound := func(want ...DiscoverIdentity) {
		t.Helper()
		rows, err := repos.MediaView.FindDiscoverLibraryItems(t.Context(), items, filter)
		if err != nil {
			t.Fatal(err)
		}
		found := map[DiscoverIdentity]bool{}
		for _, row := range rows {
			found[row] = true
		}
		if len(found) != len(want) {
			t.Fatalf("got %#v want %#v", rows, want)
		}
		if len(rows) != len(found) {
			t.Fatalf("duplicate ownership results: %#v", rows)
		}
		for _, id := range want {
			if !found[id] {
				t.Fatalf("missing %#v in %#v", id, rows)
			}
		}
	}
	assertFound(items[0], items[1])
	if err := db.Where("metadata_id = ?", movie.ID).Delete(&model.Media{}).Error; err != nil {
		t.Fatal(err)
	}
	assertFound(items[1]) // 数值相同的电影被删除，整剧标记仍然存在。
	filter.HiddenLibraryIDs = []string{lib.ID}
	assertFound()
	filter = MediaQueryFilter{AllowedLibraryIDs: []string{"__locked__"}}
	assertFound()
	filter = MediaQueryFilter{}
	assertFound(items[1], items[3])
	if err := db.Model(&lib).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	assertFound(items[3])
	if err := db.Delete(&hidden).Error; err != nil {
		t.Fatal(err)
	}
	assertFound()
	if _, err := repos.MediaView.FindDiscoverLibraryItems(t.Context(), []DiscoverIdentity{{1, "season"}}, filter); err == nil {
		t.Fatal("accepted season")
	}
	if _, err := repos.MediaView.FindDiscoverLibraryItems(t.Context(), make([]DiscoverIdentity, 101), filter); err == nil {
		t.Fatal("accepted unbounded batch")
	}
}
