package repository

import (
	"encoding/json"
	"strings"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMediaViewIdentifierProjectionIsCorrelated(t *testing.T) {
	db, err := gorm.Open(postgres.Open(""), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	var rows []model.MediaView
	stmt := (&MediaViewRepository{db: db}).query(t.Context()).
		Where("m.id = ?", "media-1").Select(mediaViewSelect).Find(&rows).Statement
	sql := strings.Join(strings.Fields(stmt.SQL.String()), " ")
	for _, want := range []string{"LEFT JOIN LATERAL", "mid.metadata_id = mi.id", "mid.entity_kind = mi.kind"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("media view query missing %q: %s", want, sql)
		}
	}
	if strings.Contains(sql, "GROUP BY metadata_id, entity_kind") {
		t.Fatalf("media view query still aggregates all identifiers: %s", sql)
	}
}

func TestMetadataSearchSpecialCharactersDoNotMatchEverything(t *testing.T) {
	db, err := gorm.Open(postgres.Open(""), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	var rows []model.MetadataItem
	for input, want := range map[string]string{"%": `%\%%`, "_": `%\_%`, `\`: `%\\%`} {
		stmt := applyMetadataSearchLIKEFilter(
			db.Table("metadata_items AS search_metadata"),
			buildMetadataSearchTermGroups(MediaSearchTerms(input)),
			MetadataSearchFieldsWeb,
		).Find(&rows).Statement
		sql := strings.Join(strings.Fields(stmt.SQL.String()), " ")
		if !strings.Contains(sql, "LIKE") || !strings.Contains(sql, "ESCAPE") || len(stmt.Vars) == 0 || stmt.Vars[0] != want {
			t.Fatalf("LIKE metacharacter %q must be matched literally: sql=%s vars=%#v", input, sql, stmt.Vars)
		}
		if strings.Contains(sql, "scan_title") || strings.Contains(sql, "path") {
			t.Fatalf("metadata search must not inspect media fields: %s", sql)
		}
	}
	if terms := MediaSearchTerms("..."); len(terms) != 0 {
		t.Fatalf("delimiter-only search terms = %#v, want empty", terms)
	}
}

func TestMetadataSearchLIKEFilterRequiresChineseTokensAndKeepsNumbersAtomic(t *testing.T) {
	db, err := gorm.Open(postgres.Open(""), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	var rows []model.MetadataItem
	stmt := applyMetadataSearchLIKEFilter(
		db.Table("metadata_items AS search_metadata"),
		buildMetadataSearchTermGroups(MediaSearchTerms("死神 44")),
		MetadataSearchFieldsTitle,
	).Find(&rows).Statement
	sql := strings.Join(strings.Fields(stmt.SQL.String()), " ")
	if !strings.Contains(sql, " AND ") || !strings.Contains(sql, " OR ") {
		t.Fatalf("grouped LIKE query = %s", sql)
	}
	vars := make(map[any]bool, len(stmt.Vars))
	for _, value := range stmt.Vars {
		vars[value] = true
	}
	for _, want := range []string{
		"%死%", "%神%",
		"(^|[^0-9])44([^0-9]|$)",
		"(^|[^零一二三四五六七八九十百])四十四([^零一二三四五六七八九十百]|$)",
	} {
		if !vars[want] {
			t.Fatalf("grouped search vars=%#v, missing %q", stmt.Vars, want)
		}
	}
	if vars["%44%"] || vars["%四十四%"] || vars["%四%"] || vars["%十%"] {
		t.Fatalf("numeric equivalents must stay atomic: %#v", stmt.Vars)
	}
	if !strings.Contains(sql, " ~ ") {
		t.Fatalf("numeric equivalents must use whole-run matching: %s", sql)
	}
}

func TestMediaViewFiltersSortsAndPaginatesBySharedMetadata(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}

	publicMetadata := model.MetadataItem{
		Base: model.Base{ID: "metadata-public"}, Kind: model.MetadataKindMovie,
		Title: "Shared Public", ReleaseDate: "2025-01-01", Year: 2025, Source: "tmdb",
	}
	adultMetadata := model.MetadataItem{
		Base: model.Base{ID: "metadata-adult"}, Kind: model.MetadataKindMovie,
		Title: "Shared Adult", ReleaseDate: "2026-01-01", Year: 2026, NSFW: true, Source: "tmdb",
	}
	if err := repos.DB.Create(&[]model.MetadataItem{publicMetadata, adultMetadata}).Error; err != nil {
		t.Fatal(err)
	}
	media := []model.Media{
		{Base: model.Base{ID: "media-public"}, LibraryID: library.ID, MetadataID: publicMetadata.ID, Title: "Raw Public", Path: "/media/movies/public.mkv"},
		{Base: model.Base{ID: "media-adult"}, LibraryID: library.ID, MetadataID: adultMetadata.ID, Title: "Raw Adult", Path: "/media/movies/adult.mkv"},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	rows, total, err := repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{library.ID}, 0, 10, MediaQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != "media-public" || rows[0].Title != "Shared Public" {
		t.Fatalf("public view total=%d rows=%#v", total, rows)
	}

	rows, total, err = repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{library.ID}, 0, 1, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 1 || rows[0].ID != "media-adult" || rows[0].Year != 2026 {
		t.Fatalf("first page total=%d rows=%#v", total, rows)
	}
	rows, total, err = repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{library.ID}, 1, 1, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 1 || rows[0].ID != "media-public" {
		t.Fatalf("second page total=%d rows=%#v", total, rows)
	}

	rows, total, err = repos.MediaView.SearchFilteredPage(t.Context(), "Shared Adult", 0, 10, MediaQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("hidden NSFW search total=%d rows=%#v", total, rows)
	}
}

func TestMediaViewProjectsEpisodeArtworkAndParentIdentifiers(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	library := model.Library{Name: "TV", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}

	series := model.MetadataItem{
		Base: model.Base{ID: "metadata-series"}, Kind: model.MetadataKindSeries,
		Title: "Shared Series", Source: "tmdb",
	}
	seriesID := series.ID
	season := model.MetadataItem{
		Base: model.Base{ID: "metadata-season-1"}, Kind: model.MetadataKindSeason,
		ParentID: &seriesID, SeasonNum: 1, Title: "Season 1", Source: "tmdb",
	}
	seasonID := season.ID
	episodes := []model.MetadataItem{
		{Base: model.Base{ID: "metadata-episode-1"}, Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 1, Title: "Pilot", Source: "tmdb"},
		{Base: model.Base{ID: "metadata-episode-2"}, Kind: model.MetadataKindEpisode, ParentID: &seasonID, EpisodeNum: 2, Title: "Second", Source: "tmdb"},
	}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{Base: model.Base{ID: "identifier-tmdb"}, MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "123"},
		{Base: model.Base{ID: "identifier-tmdb-alias"}, MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "456"},
		{Base: model.Base{ID: "identifier-tmdb-movie"}, MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "999"},
		{Base: model.Base{ID: "identifier-douban"}, MetadataID: series.ID, Provider: "douban", EntityKind: model.MetadataKindSeries, ExternalID: "db-123"},
		{Base: model.Base{ID: "identifier-episode-tmdb"}, MetadataID: episodes[0].ID, Provider: "tmdb", EntityKind: model.MetadataKindEpisode, ExternalID: "601"},
		{Base: model.Base{ID: "identifier-episode-douban"}, MetadataID: episodes[0].ID, Provider: "douban", EntityKind: model.MetadataKindEpisode, ExternalID: "db-601"},
	}
	if err := repos.DB.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}

	assets := []model.ArtworkAsset{
		{Base: model.Base{ID: "asset-poster"}, SHA256: strings.Repeat("1", 64), StorageKey: "poster.jpg", MimeType: "image/jpeg"},
		{Base: model.Base{ID: "asset-backdrop"}, SHA256: strings.Repeat("2", 64), StorageKey: "backdrop.jpg", MimeType: "image/jpeg"},
		{Base: model.Base{ID: "asset-still"}, SHA256: strings.Repeat("3", 64), StorageKey: "still.jpg", MimeType: "image/jpeg"},
	}
	if err := repos.DB.Create(&assets).Error; err != nil {
		t.Fatal(err)
	}
	artworks := []model.MetadataArtwork{
		{MetadataID: series.ID, AssetID: "asset-poster", ArtworkType: model.ArtworkTypePoster},
		{MetadataID: series.ID, AssetID: "asset-backdrop", ArtworkType: model.ArtworkTypeBackdrop},
		{MetadataID: episodes[0].ID, AssetID: "asset-still", ArtworkType: model.ArtworkTypeStill},
	}
	if err := repos.DB.Create(&artworks).Error; err != nil {
		t.Fatal(err)
	}

	media := []model.Media{
		{Base: model.Base{ID: "media-episode-1"}, LibraryID: library.ID, MetadataID: episodes[0].ID, Title: "Raw Show", Path: "/media/tv/S01E01.mkv", SeasonNum: 1, EpisodeNum: 1},
		{Base: model.Base{ID: "media-episode-2"}, LibraryID: library.ID, MetadataID: episodes[1].ID, Title: "Raw Show", Path: "/media/tv/S01E02.mkv", SeasonNum: 1, EpisodeNum: 2},
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	rows, total, err := repos.MediaView.ListByLibrariesFiltered(t.Context(), []string{library.ID}, 0, 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("entity-kind-scoped identifiers must not duplicate media views: total=%d rows=%#v", total, rows)
	}

	first, err := repos.MediaView.FindByID(t.Context(), "media-episode-1")
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.SeriesID != series.ID || first.SeasonID != season.ID || first.SeriesTitle != series.Title || first.Title != "Pilot" {
		t.Fatalf("episode projection = %#v", first)
	}
	if first.PosterURL != "" || first.BackdropURL != "/api/artwork/asset-still" {
		t.Fatalf("episode artwork poster=%q backdrop=%q", first.PosterURL, first.BackdropURL)
	}
	if first.TMDbID != 601 || first.DoubanID != "db-601" {
		t.Fatalf("episode identifiers tmdb=%d douban=%q", first.TMDbID, first.DoubanID)
	}

	second, err := repos.MediaView.FindByID(t.Context(), "media-episode-2")
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.PosterURL != "" || second.BackdropURL != "" || second.TMDbID != 0 {
		t.Fatalf("episode inherited parent data = %#v", second)
	}

	payload, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"season_id", "series_title", "title", "nsfw", "tmdb_id"} {
		if count := strings.Count(string(payload), `"`+field+`":`); count != 1 {
			t.Fatalf("JSON field %q count=%d payload=%s", field, count, payload)
		}
	}
	if strings.Contains(string(payload), `"poster_url"`) {
		t.Fatalf("empty own poster should be omitted: %s", payload)
	}
}

func newMediaViewTestRepositories(t *testing.T) *Container {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}
