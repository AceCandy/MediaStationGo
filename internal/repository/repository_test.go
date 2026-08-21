package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func createTestMetadata(t *testing.T, repos *Container, item model.MetadataItem, identifiers ...model.MetadataIdentifier) *model.MetadataItem {
	t.Helper()
	if err := repos.Metadata.Create(t.Context(), &item, identifiers); err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	return &item
}

func TestValidateMetadataItemIdentity(t *testing.T) {
	parentID := "series-1"
	for name, item := range map[string]*model.MetadataItem{
		"episode without parent":      {Kind: model.MetadataKindEpisode, SeasonNum: 1, EpisodeNum: 1},
		"episode without number":      {Kind: model.MetadataKindEpisode, ParentID: &parentID, SeasonNum: 1},
		"movie with episode identity": {Kind: model.MetadataKindMovie, SeasonNum: 1, EpisodeNum: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateMetadataItem(item); err == nil {
				t.Fatal("expected invalid metadata identity to be rejected")
			}
		})
	}
	valid := &model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &parentID, SeasonNum: 0, EpisodeNum: 1}
	if err := validateMetadataItem(valid); err != nil {
		t.Fatalf("valid special episode rejected: %v", err)
	}
}

func TestNormalizeMetadataIdentifierAcceptsCatalogProvidersAndKinds(t *testing.T) {
	tests := map[string]struct {
		identifier model.MetadataIdentifier
		want       model.MetadataIdentifier
	}{
		"imdb series": {
			identifier: model.MetadataIdentifier{Provider: " IMDb ", EntityKind: " SERIES ", ExternalID: " tt1234567 "},
			want:       model.MetadataIdentifier{Provider: "imdb", EntityKind: model.MetadataKindSeries, ExternalID: "tt1234567"},
		},
		"tmdb season": {
			identifier: model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeason, ExternalID: "101"},
			want:       model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeason, ExternalID: "101"},
		},
		"thetvdb season": {
			identifier: model.MetadataIdentifier{Provider: "thetvdb", EntityKind: model.MetadataKindSeason, ExternalID: "201"},
			want:       model.MetadataIdentifier{Provider: "thetvdb", EntityKind: model.MetadataKindSeason, ExternalID: "201"},
		},
		"imdb episode": {
			identifier: model.MetadataIdentifier{Provider: "imdb", EntityKind: model.MetadataKindEpisode, ExternalID: "tt7654321"},
			want:       model.MetadataIdentifier{Provider: "imdb", EntityKind: model.MetadataKindEpisode, ExternalID: "tt7654321"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := normalizeMetadataIdentifier(&tt.identifier); err != nil {
				t.Fatalf("valid catalog identifier rejected: %v", err)
			}
			if tt.identifier.Provider != tt.want.Provider || tt.identifier.EntityKind != tt.want.EntityKind || tt.identifier.ExternalID != tt.want.ExternalID {
				t.Fatalf("catalog identifier was not normalized: got %#v, want %#v", tt.identifier, tt.want)
			}
		})
	}
}

func TestMediaUpsertRejectsRetiredCloudPath(t *testing.T) {
	err := (&MediaRepository{}).Upsert(t.Context(), &model.Media{Path: " CLOUD://OpenList/movie.mkv "})
	if !errors.Is(err, errCloudMediaPathUnsupported) {
		t.Fatalf("error = %v, want errCloudMediaPathUnsupported", err)
	}
}

func TestMediaUpsertSkipsUnchangedExistingRow(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "电影", Path: "/media/movie", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "已有影片",
		Path:         "/media/movie/existing.mkv",
		SizeBytes:    1024,
		DurationSec:  60,
		Width:        1920,
		Height:       1080,
		VideoCodec:   "h264",
		AudioCodec:   "aac",
		Container:    "matroska,webm",
		ScrapeStatus: "pending",
	}
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	var before model.Media
	if err := repos.DB.Where("path = ?", media.Path).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	again := model.Media{
		LibraryID:    lib.ID,
		Title:        before.Title,
		Path:         before.Path,
		SizeBytes:    before.SizeBytes,
		DurationSec:  before.DurationSec,
		Width:        before.Width,
		Height:       before.Height,
		VideoCodec:   before.VideoCodec,
		AudioCodec:   before.AudioCodec,
		Container:    before.Container,
		ScrapeStatus: before.ScrapeStatus,
	}
	if err := repos.Media.Upsert(t.Context(), &again); err != nil {
		t.Fatal(err)
	}
	var after model.Media
	if err := repos.DB.Where("path = ?", media.Path).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("unchanged upsert touched updated_at: before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestMediaUpsertRefreshesExternalIDFromPathHint(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "国产剧", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	path := "/media/tv/折腰 (2025) {tmdb-296753}/Season 1/折腰.S01E01.mkv"
	existing := model.Media{
		LibraryID:    lib.ID,
		Title:        "折腰",
		Path:         path,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       220269,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), &existing); err != nil {
		t.Fatal(err)
	}
	next := model.Media{
		LibraryID:  lib.ID,
		Title:      "折腰",
		Path:       path,
		SeasonNum:  1,
		EpisodeNum: 1,
		TMDbID:     296753,
	}
	if err := repos.Media.Upsert(t.Context(), &next); err != nil {
		t.Fatal(err)
	}
	var got model.Media
	if err := repos.DB.Where("path = ?", path).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.TMDbID != 296753 || got.ScrapeStatus != "pending" {
		t.Fatalf("path hint should refresh tmdb and retry scrape, got tmdb=%d status=%q", got.TMDbID, got.ScrapeStatus)
	}
}

func TestMediaUpsertMatchedIncomingRefreshesScrapedMetadata(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "剧集", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	path := "/media/tv/show/S01E01.mkv"
	existing := model.Media{
		LibraryID:    lib.ID,
		Title:        "扫描标题",
		Path:         path,
		ScrapeStatus: "no_match",
	}
	if err := repos.Media.Upsert(t.Context(), &existing); err != nil {
		t.Fatal(err)
	}
	series := createTestMetadata(t, repos, model.MetadataItem{
		Base: model.Base{ID: "metadata-series-1"}, Kind: model.MetadataKindSeries,
		Title: "中文剧名", OriginalName: "Original Show", Source: "tmdb",
	},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "123"},
		model.MetadataIdentifier{Provider: "bangumi", EntityKind: model.MetadataKindSeries, ExternalID: "456"},
		model.MetadataIdentifier{Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "db-1"},
		model.MetadataIdentifier{Provider: "thetvdb", EntityKind: model.MetadataKindSeries, ExternalID: "tvdb-1"},
	)
	season := createTestMetadata(t, repos, model.MetadataItem{
		Base: model.Base{ID: "metadata-season-1"}, Kind: model.MetadataKindSeason, ParentID: &series.ID,
		Title: "第一季", SeasonNum: 1, Source: "tmdb",
	})
	episode := createTestMetadata(t, repos, model.MetadataItem{
		Base: model.Base{ID: "metadata-episode-1"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID,
		Title: "第一集", OriginalName: "Episode One", Overview: "剧情简介",
		Rating: 8.6, Year: 2026, EpisodeNum: 1, Languages: "zh,en", Countries: "CN",
		Genres: "剧情,悬疑", NSFW: true, Source: "tmdb",
	},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindEpisode, ExternalID: "601"},
		model.MetadataIdentifier{Provider: "bangumi", EntityKind: model.MetadataKindEpisode, ExternalID: "602"},
		model.MetadataIdentifier{Provider: "douban", EntityKind: model.MetadataKindEpisode, ExternalID: "db-e1"},
		model.MetadataIdentifier{Provider: "thetvdb", EntityKind: model.MetadataKindEpisode, ExternalID: "tvdb-e1"},
	)
	assets := []model.ArtworkAsset{
		{Base: model.Base{ID: "asset-backdrop-1"}, SHA256: "backdrop-hash-1", StorageKey: "sha256/ba/backdrop.jpg", MimeType: "image/jpeg"},
	}
	if err := repos.DB.Create(&assets).Error; err != nil {
		t.Fatal(err)
	}
	artworks := []model.MetadataArtwork{
		{MetadataID: episode.ID, AssetID: assets[0].ID, ArtworkType: model.ArtworkTypeStill},
	}
	if err := repos.DB.Create(&artworks).Error; err != nil {
		t.Fatal(err)
	}
	incoming := model.Media{
		LibraryID:    lib.ID,
		Title:        "中文剧名",
		Path:         path,
		SeasonNum:    1,
		EpisodeNum:   1,
		ScrapeStatus: "matched",
		MetadataID:   episode.ID,
	}
	if err := repos.Media.Upsert(t.Context(), &incoming); err != nil {
		t.Fatal(err)
	}
	var got model.Media
	if err := repos.DB.Where("path = ?", path).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.MetadataID != episode.ID || got.ScrapeStatus != "matched" || got.SeasonNum != 1 || got.EpisodeNum != 1 {
		t.Fatalf("matched metadata link not refreshed: %#v", got)
	}
	view, err := repos.MediaView.FindByID(t.Context(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || view.SeriesTitle != "中文剧名" || view.Title != "第一集" || view.OriginalName != "Episode One" || view.Overview != "剧情简介" {
		t.Fatalf("shared names/overview not projected: %#v", view)
	}
	if view.PosterURL != "" || view.BackdropURL != "/api/artwork/asset-backdrop-1" {
		t.Fatalf("shared artwork not projected: %#v", view)
	}
	if view.TMDbID != 601 || view.BangumiID != 602 || view.DoubanID != "db-e1" || view.TheTVDBID != "tvdb-e1" {
		t.Fatalf("shared provider identifiers not projected: %#v", view)
	}
	if view.Year != 2026 || view.SeasonNum != 1 || view.EpisodeNum != 1 || view.Rating != 8.6 || view.Languages != "zh,en" || view.Countries != "CN" || view.Genres != "剧情,悬疑" || !view.NSFW {
		t.Fatalf("shared detail metadata not projected: %#v", view)
	}
}

func TestListByLibraryOrdersByReleaseDate(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "国产剧", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	testRows := []struct {
		id, title, path, releaseDate string
		year                         int
	}{
		{"metadata-list-old", "旧片但最近更新", "/media/tv/old.mkv", "2026-01-10", 2026},
		{"metadata-list-newest", "最新首播", "/media/tv/newest.mkv", "2026-06-23", 2026},
		{"metadata-list-year", "无完整日期", "/media/tv/year-only.mkv", "", 2025},
	}
	for _, row := range testRows {
		metadata := createTestMetadata(t, repos, model.MetadataItem{
			Base: model.Base{ID: row.id}, Kind: model.MetadataKindSeries, Title: row.title,
			Year: row.year, ReleaseDate: row.releaseDate, Source: "tmdb",
		})
		media := model.Media{LibraryID: lib.ID, MetadataID: metadata.ID, Title: row.title, Path: row.path, Year: row.year, ScrapeStatus: "matched"}
		if err := repos.Media.Upsert(t.Context(), &media); err != nil {
			t.Fatal(err)
		}
	}

	items, total, err := repos.Media.ListByLibrary(t.Context(), lib.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("list total=%d len=%d", total, len(items))
	}
	if got := []string{items[0].Title, items[1].Title, items[2].Title}; got[0] != "最新首播" || got[1] != "旧片但最近更新" || got[2] != "无完整日期" {
		t.Fatalf("release-date order = %#v", got)
	}
}

func TestMediaUpsertScanDoesNotClearMatchedMetadata(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "剧集", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	path := "/media/tv/间谍过家家/Season 01/间谍过家家 - S01E01.mkv"
	series := createTestMetadata(t, repos, model.MetadataItem{
		Base: model.Base{ID: "metadata-spy-series"}, Kind: model.MetadataKindSeries,
		Title: "间谍过家家", OriginalName: "SPY×FAMILY", Overview: "剧情简介", Year: 2022, Source: "tmdb",
	},
		model.MetadataIdentifier{Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"},
		model.MetadataIdentifier{Provider: "bangumi", EntityKind: model.MetadataKindSeries, ExternalID: "67890"},
		model.MetadataIdentifier{Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "db-spy"},
		model.MetadataIdentifier{Provider: "thetvdb", EntityKind: model.MetadataKindSeries, ExternalID: "tvdb-spy"},
	)
	season := createTestMetadata(t, repos, model.MetadataItem{
		Base: model.Base{ID: "metadata-spy-season"}, Kind: model.MetadataKindSeason, ParentID: &series.ID,
		Title: "第一季", SeasonNum: 1, Source: "tmdb",
	})
	episode := createTestMetadata(t, repos, model.MetadataItem{
		Base: model.Base{ID: "metadata-spy-episode"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID,
		Title: "行动代号〈梟〉", OriginalName: "OPERATION STRIX", Overview: "第一集简介", Year: 2022,
		EpisodeNum: 1, Source: "tmdb",
	})
	existing := model.Media{
		LibraryID:    lib.ID,
		MetadataID:   episode.ID,
		Title:        "间谍过家家",
		Path:         path,
		Year:         2022,
		SeasonNum:    1,
		EpisodeNum:   1,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), &existing); err != nil {
		t.Fatal(err)
	}
	scan := model.Media{
		LibraryID:         lib.ID,
		Title:             "Spy.x.Family.S01E01.2022.1080p.WEB-DL",
		Path:              path,
		ScanFileSizeBytes: 2048,
		ScanFileMTimeNS:   1500,
		SeasonNum:         1,
		EpisodeNum:        1,
	}
	if err := repos.Media.Upsert(t.Context(), &scan); err != nil {
		t.Fatal(err)
	}

	var got model.Media
	if err := repos.DB.Where("path = ?", path).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.MetadataID != episode.ID || got.ScrapeStatus != "matched" {
		t.Fatalf("matched metadata link/status were overwritten by scan: %#v", got)
	}
	if got.ScanFileSizeBytes != 2048 || got.ScanFileMTimeNS != 1500 {
		t.Fatalf("file scan fingerprint was not refreshed: %#v", got)
	}
	view, err := repos.MediaView.FindByID(t.Context(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view == nil || view.SeriesTitle != "间谍过家家" || view.Title != "行动代号〈梟〉" || view.OriginalName != "OPERATION STRIX" || view.Overview != "第一集简介" {
		t.Fatalf("shared metadata was not preserved after scan: %#v", view)
	}
	if view.TMDbID != 0 || view.BangumiID != 0 || view.DoubanID != "" || view.TheTVDBID != "" {
		t.Fatalf("episode view inherited series provider identifiers: %#v", view)
	}
	if scan.ID != got.ID || scan.MetadataID != episode.ID || scan.Title != got.Title || scan.ScrapeStatus != "matched" {
		t.Fatalf("upsert caller did not receive fresh matched row: %#v want %#v", scan, got)
	}
}

func TestMediaUpsertKeepsLocalLibraryIDOnRescan(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	localA := model.Library{Name: "Movies A", Path: "/media/a", Type: "movie", Enabled: true}
	localB := model.Library{Name: "Movies B", Path: "/media/b", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &localA); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &localB); err != nil {
		t.Fatal(err)
	}
	localPath := "/media/a/Inception (2010)/Inception.mkv"
	if err := repos.Media.Upsert(t.Context(), &model.Media{LibraryID: localA.ID, Title: "Inception", Path: localPath}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.Upsert(t.Context(), &model.Media{LibraryID: localB.ID, Title: "Inception", Path: localPath}); err != nil {
		t.Fatal(err)
	}
	var localGot model.Media
	if err := repos.DB.Where("path = ?", localPath).First(&localGot).Error; err != nil {
		t.Fatal(err)
	}
	if localGot.LibraryID != localA.ID {
		t.Fatalf("local media library_id must not migrate, want %q got %q", localA.ID, localGot.LibraryID)
	}
}

type fakeMediaSearchBackend struct {
	ids []string
	err error
}

func (f fakeMediaSearchBackend) SearchMediaIDs(context.Context, string, int, int, MediaQueryFilter) ([]string, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return append([]string(nil), f.ids...), int64(len(f.ids)), nil
}

func TestMediaSearchUsesExternalBackendAndFallsBack(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "Movies", Path: "/media/movie", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	for _, row := range []model.Media{
		{Base: model.Base{ID: "m-1"}, LibraryID: lib.ID, Title: "Alpha", Path: "/media/a.mkv"},
		{Base: model.Base{ID: "m-2"}, LibraryID: lib.ID, Title: "Beta", Path: "/media/b.mkv"},
	} {
		metadata := createTestMetadata(t, repos, model.MetadataItem{Kind: model.MetadataKindMovie, Title: row.Title, Source: "local"})
		row.MetadataID = metadata.ID
		if err := repos.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	repos.Media.SetSearchBackend(fakeMediaSearchBackend{ids: []string{"m-2", "m-1"}})
	items, total, err := repos.Media.SearchFilteredPage(t.Context(), "anything", 0, 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 || items[0].ID != "m-2" || items[1].ID != "m-1" {
		t.Fatalf("external search result total=%d items=%#v", total, items)
	}

	repos.Media.SetSearchBackend(fakeMediaSearchBackend{err: errors.New("opensearch down")})
	items, total, err = repos.Media.SearchFilteredPage(t.Context(), "Alpha", 0, 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "m-1" {
		t.Fatalf("fallback result total=%d items=%#v", total, items)
	}
}

func TestMediaSearchFilteredSupportsChineseFuzzyTerms(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "国产剧", Path: "/media/国产剧", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	testRows := []struct {
		mediaID, metadataID, title, originalName, path, genres string
	}{
		{"m-ferry", "metadata-ferry", "灵魂摆渡·十年", "The Ferry Man 10th Anniversary", "/media/国产剧/灵魂摆渡·十年/S01E01.mkv", "悬疑,奇幻"},
		{"m-ashes", "metadata-ashes", "翘楚", "Ashes to Crown", "/media/国产剧/翘楚/S01E01.mkv", "剧情"},
	}
	for _, row := range testRows {
		metadata := createTestMetadata(t, repos, model.MetadataItem{
			Base: model.Base{ID: row.metadataID}, Kind: model.MetadataKindSeries,
			Title: row.title, OriginalName: row.originalName, Genres: row.genres, Source: "tmdb",
		})
		media := model.Media{
			Base: model.Base{ID: row.mediaID}, LibraryID: lib.ID, MetadataID: metadata.ID,
			Title: row.title, Path: row.path, ScrapeStatus: "matched",
		}
		if err := repos.Media.Upsert(t.Context(), &media); err != nil {
			t.Fatalf("upsert media: %v", err)
		}
	}

	items, err := repos.Media.SearchFiltered(t.Context(), "灵魂 十年", 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatalf("search chinese terms: %v", err)
	}
	if len(items) == 0 || items[0].ID != "m-ferry" {
		t.Fatalf("chinese fuzzy search missed target: %#v", items)
	}

	items, err = repos.Media.SearchFiltered(t.Context(), "Ferry", 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatalf("search original name: %v", err)
	}
	if len(items) == 0 || items[0].ID != "m-ferry" {
		t.Fatalf("original-name search missed target: %#v", items)
	}

	items, err = repos.Media.SearchFiltered(t.Context(), "悬疑", 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatalf("search genre: %v", err)
	}
	if len(items) == 0 || items[0].ID != "m-ferry" {
		t.Fatalf("genre search missed target: %#v", items)
	}
}
