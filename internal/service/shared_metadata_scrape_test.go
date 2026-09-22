package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestProviderMatchesShareMovieMetadataAcrossPaths(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.MetadataArtworkRecheck{}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Movies", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: lib.ID, Title: "Copy A", Path: filepath.Join(lib.Path, "a.mkv"), ScrapeStatus: "pending"},
		{LibraryID: lib.ID, Title: "Copy B", Path: filepath.Join(lib.Path, "b.mkv"), ScrapeStatus: "pending"},
	}
	for i := range rows {
		if err := repos.Media.Upsert(t.Context(), &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	match := &Match{
		Source: "tmdb", MediaType: "movie", TMDbID: 777, Title: "Shared Movie",
		PosterURL: "https://images.example.test/images/poster.png", AllowIdentifierMerge: true,
	}
	for i := range rows {
		if err := scraper.applyProviderMatch(t.Context(), &rows[i], &lib, match); err != nil {
			t.Fatal(err)
		}
	}
	var got []model.Media
	if err := repos.DB.Where("id IN ?", []string{rows[0].ID, rows[1].ID}).Order("id").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].MetadataID == "" || got[1].MetadataID == "" || got[0].MetadataID != got[1].MetadataID {
		t.Fatalf("movie copies did not share metadata: %#v", got)
	}
	if got[0].ID == got[1].ID || got[0].Path == got[1].Path {
		t.Fatal("media file identity must remain independent")
	}
}

func TestProviderMatchesShareOnlyTheSameEpisodeMetadata(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	lib := model.Library{Name: "TV", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: lib.ID, SeriesID: "local-show", Title: "Show", Path: filepath.Join(lib.Path, "a-s01e01.mkv"), SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "pending"},
		{LibraryID: lib.ID, SeriesID: "local-show", Title: "Show", Path: filepath.Join(lib.Path, "b-s01e01.mkv"), SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "pending"},
		{LibraryID: lib.ID, SeriesID: "local-show", Title: "Show", Path: filepath.Join(lib.Path, "s01e02.mkv"), SeasonNum: 1, EpisodeNum: 2, ScrapeStatus: "pending"},
	}
	for i := range rows {
		if err := repos.Media.Upsert(t.Context(), &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	match := &Match{Source: "tmdb", MediaType: "tv", TMDbID: 888, Title: "Shared Show"}
	for i := range rows {
		if err := scraper.applyProviderMatch(t.Context(), &rows[i], &lib, match); err != nil {
			t.Fatal(err)
		}
	}
	var got []model.Media
	if err := repos.DB.Where("id IN ?", []string{rows[0].ID, rows[1].ID, rows[2].ID}).Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	byPath := make(map[string]string, len(got))
	for _, media := range got {
		if media.MetadataID == "" {
			t.Fatalf("missing episode metadata ID: %#v", media)
		}
		byPath[media.Path] = media.MetadataID
	}
	if byPath[rows[0].Path] != byPath[rows[1].Path] {
		t.Fatal("two copies of the same episode must share metadata")
	}
	if byPath[rows[0].Path] == byPath[rows[2].Path] {
		t.Fatal("different episodes must not share episode metadata")
	}
}

func TestEpisodeShellsDoNotInheritParentMetadata(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	lib := model.Library{Name: "TV", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "Show", EpisodeTitle: "Own episode", Path: filepath.Join(lib.Path, "show-s01e01.mkv"), SeasonNum: 1, EpisodeNum: 1}
	media.SeriesID = localSeriesIdentity(&media)
	match := &Match{
		Source: "tmdb", MediaType: "tv", TMDbID: 888, Title: "Parent show", OriginalName: "Parent original",
		Overview: "Parent overview", Rating: 8.8, Year: 2024, ReleaseDate: "2024-01-02",
		Languages: []string{"ja"}, Countries: []string{"JP"}, Genres: []string{"Animation"}, NSFW: true,
		PosterURL: "https://images.example.test/images/parent-poster.png", BackdropURL: "https://images.example.test/images/parent-backdrop.png",
		LoadedCreditTypes: []string{model.CreditTypeActor}, Credits: []PersonCredit{{Name: "Parent actor", Type: model.CreditTypeActor}},
	}

	persisted, err := scraper.persistProviderMetadata(t.Context(), &media, &lib, match)
	if err != nil {
		t.Fatal(err)
	}
	assertChildOwnMetadata(t, persisted.Target, "Own episode", "", 0, 0, "", "", "", "", false)
	season, err := repos.Metadata.FindSeason(t.Context(), persisted.Series.ID, 1)
	if err != nil || season == nil {
		t.Fatalf("season = %#v, err = %v", season, err)
	}
	assertChildOwnMetadata(t, season, "第 1 季", "", 0, 0, "", "", "", "", false)
	assertNoChildArtworkOrCredits(t, repos, season.ID)
	assertNoChildArtworkOrCredits(t, repos, persisted.Target.ID)

	season.Overview, season.Year, season.Genres = "Own season", 2025, "Season genre"
	persisted.Target.Overview, persisted.Target.Year, persisted.Target.RuntimeSec = "Own episode overview", 2026, 1500
	persisted.Target.Languages, persisted.Target.Countries, persisted.Target.Genres = "ko", "KR", "Episode genre"
	if err := repos.Metadata.Update(t.Context(), season); err != nil {
		t.Fatal(err)
	}
	if err := repos.Metadata.Update(t.Context(), persisted.Target); err != nil {
		t.Fatal(err)
	}
	media.MetadataID = persisted.Target.ID
	persisted, err = scraper.persistProviderMetadata(t.Context(), &media, &lib, match)
	if err != nil {
		t.Fatal(err)
	}
	season, _ = repos.Metadata.FindSeason(t.Context(), persisted.Series.ID, 1)
	if season == nil || season.Overview != "Own season" || season.Year != 2025 || season.Genres != "Season genre" {
		t.Fatalf("season shell overwrote owned metadata: %#v", season)
	}
	if persisted.Target.Overview != "Own episode overview" || persisted.Target.Year != 2026 || persisted.Target.RuntimeSec != 1500 || persisted.Target.Languages != "ko" || persisted.Target.Countries != "KR" || persisted.Target.Genres != "Episode genre" {
		t.Fatalf("episode shell overwrote owned metadata: %#v", persisted.Target)
	}
}

func TestLocalEpisodePersistsOnlyEntityOwnedMetadata(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	lib := model.Library{Name: "TV", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "Show", Path: filepath.Join(lib.Path, "show-s01e01.mkv"), SeasonNum: 1, EpisodeNum: 1}
	media.SeriesID = localSeriesIdentity(&media)
	local := &LocalMetadata{
		Title: "Parent show", OriginalName: "Parent original", Overview: "Parent overview", Rating: 8.8,
		Year: 2024, ReleaseDate: "2024-01-02", Languages: "ja", Countries: "JP", Genres: "Parent genre", NSFW: true,
		PosterURL: "https://images.example.test/images/parent-poster.png", BackdropURL: "https://images.example.test/images/parent-backdrop.png",
		Credits: []PersonCredit{{Name: "Parent actor", Type: model.CreditTypeActor}}, LoadedCreditTypes: []string{model.CreditTypeActor},
		EpisodeTitle: "Own episode", EpisodeOverview: "Own overview", EpisodeRating: 7.7, EpisodeYear: 2025,
		EpisodeReleaseDate: "2025-02-03", EpisodeLanguages: "ko", EpisodeCountries: "KR", EpisodeGenres: "Own genre",
		EpisodeStillURL: "https://images.example.test/images/episode-still.png",
	}

	persisted, err := scraper.persistLocalMetadata(t.Context(), &media, &lib, local)
	if err != nil {
		t.Fatal(err)
	}
	assertChildOwnMetadata(t, persisted.Target, "Own episode", "Own overview", 7.7, 2025, "2025-02-03", "ko", "KR", "Own genre", false)
	for _, linkedID := range []string{persisted.Series.ID, *persisted.Target.ParentID, persisted.Target.ID} {
		media.MetadataID = linkedID
		again, err := scraper.persistLocalMetadata(t.Context(), &media, &lib, local)
		if err != nil {
			t.Fatal(err)
		}
		if again.Series.ID != persisted.Series.ID || again.Target.ID != persisted.Target.ID {
			t.Fatal("rescrape changed canonical series or episode identity")
		}
	}
	if asset, findErr := repos.Artwork.FindSelection(t.Context(), persisted.Target.ID, model.ArtworkTypeStill); findErr != nil || asset == nil {
		t.Fatalf("episode own still = %#v, err = %v", asset, findErr)
	}
	for _, artworkType := range []string{model.ArtworkTypePoster, model.ArtworkTypeBackdrop} {
		if asset, findErr := repos.Artwork.FindSelection(t.Context(), persisted.Target.ID, artworkType); findErr != nil || asset != nil {
			t.Fatalf("episode inherited %s: asset=%#v err=%v", artworkType, asset, findErr)
		}
	}
	var credits int64
	if err := repos.DB.Model(&model.MetadataCredit{}).Where("metadata_id = ?", persisted.Target.ID).Count(&credits).Error; err != nil {
		t.Fatal(err)
	}
	if credits != 0 {
		t.Fatalf("episode inherited %d parent credits", credits)
	}
}

func assertChildOwnMetadata(t *testing.T, item *model.MetadataItem, title, overview string, rating float32, year int, releaseDate, languages, countries, genres string, nsfw bool) {
	t.Helper()
	if item == nil || item.Title != title || item.OriginalName != "" || item.Overview != overview || item.Rating != rating || item.Year != year || item.ReleaseDate != releaseDate || item.Languages != languages || item.Countries != countries || item.Genres != genres || item.NSFW != nsfw {
		t.Fatalf("child metadata ownership mismatch: %#v", item)
	}
}

func assertNoChildArtworkOrCredits(t *testing.T, repos *repository.Container, metadataID string) {
	t.Helper()
	for _, artworkType := range []string{model.ArtworkTypePoster, model.ArtworkTypeBackdrop, model.ArtworkTypeStill} {
		if asset, err := repos.Artwork.FindSelection(t.Context(), metadataID, artworkType); err != nil || asset != nil {
			t.Fatalf("child inherited %s: asset=%#v err=%v", artworkType, asset, err)
		}
	}
	var credits int64
	if err := repos.DB.Model(&model.MetadataCredit{}).Where("metadata_id = ?", metadataID).Count(&credits).Error; err != nil {
		t.Fatal(err)
	}
	if credits != 0 {
		t.Fatalf("child inherited %d parent credits", credits)
	}
}

func TestProviderMatchDoesNotMergeLocalNFOFields(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	root := t.TempDir()
	mediaPath := filepath.Join(root, "Show {tmdb-12345}", "Season 02", "Show - S02E01.mkv")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfoPath(mediaPath), []byte(`<episodedetails><title>本地单集标题</title><showtitle>间谍过家家</showtitle><plot>本地简介</plot><season>2</season><episode>1</episode><uniqueid type="tmdb">12345</uniqueid></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "TV", Path: root, Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, SeriesID: "local-show", Title: "Show", Path: mediaPath, SeasonNum: 2, EpisodeNum: 1, ScrapeStatus: "pending"}
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.MetadataID == "" {
		t.Fatal("provider match did not link shared metadata")
	}
	item, err := repos.Metadata.FindByID(t.Context(), got.MetadataID)
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.Title != "任务代号: 猫" || item.OriginalName != "" || item.Overview != "单集剧情" {
		t.Fatalf("provider metadata was mixed with local NFO: %#v", item)
	}
	if strings.Contains(item.Title+item.Overview, "本地") {
		t.Fatalf("local NFO overrode provider metadata: %#v", item)
	}
}

func TestProviderErrorDoesNotFallBackToLocalNFO(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateScraperTestModels(t, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	root := t.TempDir()
	mediaPath := filepath.Join(root, "movie.mkv")
	if err := os.WriteFile(nfoPath(mediaPath), []byte(`<movie><title>本地电影</title><plot>本地简介</plot><uniqueid type="tmdb">42</uniqueid></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "movie", Path: mediaPath, ScrapeStatus: "pending"}
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper := NewScraperService(cfg, zap.NewNop(), repos, NewTMDbProvider(cfg, zap.NewNop(), nil), nil, nil, nil, NewHub(zap.NewNop()))
	if err := scraper.EnrichOne(t.Context(), &media); err == nil {
		t.Fatal("expected provider error")
	}
	var got model.Media
	if err := db.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ScrapeStatus != "error" || got.MetadataID != "" || got.ScrapeError == "" {
		t.Fatalf("provider error incorrectly fell back to local metadata: %#v", got)
	}
	var metadataCount int64
	if err := db.Model(&model.MetadataItem{}).Count(&metadataCount).Error; err != nil {
		t.Fatal(err)
	}
	if metadataCount != 0 {
		t.Fatalf("metadata rows after provider error = %d, want none", metadataCount)
	}
}
