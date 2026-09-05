package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testutil"
)

func TestListLibrariesHidesAdultDirectoriesUnlessAdminRequestsAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.PlaybackHistory{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	viewer := &model.User{Username: "viewer", PasswordHash: "hash", Role: "admin", HideAdult: true}
	if err := repos.User.Create(t.Context(), viewer); err != nil {
		t.Fatal(err)
	}
	safe := model.Library{Name: "电影", Path: "/media/movie", Type: "movie", Enabled: true}
	adult := model.Library{Name: "9KG", Path: "/media/9KG", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &safe); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &adult); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), service.AdultLibraryIDsSettingKey, `["`+adult.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.Upsert(t.Context(), &model.Media{LibraryID: safe.ID, Title: "误入普通库的成人条目", Path: "/media/movie/nsfw.mkv", NSFW: true}); err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	visible := requestLibraries(t, svc, viewer.ID, "admin", "/api/libraries")
	if len(visible) != 1 || visible[0].ID != safe.ID {
		t.Fatalf("watching library list should hide adult directories, got %#v", visible)
	}

	all := requestLibraries(t, svc, viewer.ID, "admin", "/api/libraries?include_hidden=1")
	if len(all) != 2 {
		t.Fatalf("admin include_hidden list should keep management access, got %#v", all)
	}
}

func TestListLibrariesIncludeHiddenKeepsLibraryDisplayName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "国产剧", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	all := requestLibraries(t, svc, "admin", "admin", "/api/libraries?include_hidden=1")
	if len(all) != 1 {
		t.Fatalf("include_hidden list = %#v, want one library", all)
	}
	if all[0].Name != "国产剧" {
		t.Fatalf("library display name = %q, want 国产剧", all[0].Name)
	}
}

func TestListLibrariesShowsAllOrdinaryLibraries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	root := model.Library{Name: "电影", Path: "/media/movies", Type: "movie", Enabled: true}
	auto := model.Library{Name: "欧美剧", Path: "/media/tv/western", Type: "tv", Enabled: true}
	for _, lib := range []*model.Library{&root, &auto} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	all := requestLibraries(t, svc, "admin", "admin", "/api/libraries?include_hidden=1")
	if len(all) != 2 {
		t.Fatalf("include_hidden list = %#v, want root plus auto category library", all)
	}
	ids := map[string]bool{}
	for _, lib := range all {
		ids[lib.ID] = true
	}
	if !ids[root.ID] || !ids[auto.ID] {
		t.Fatalf("include_hidden list = %#v, want root %s and auto category %s", all, root.ID, auto.ID)
	}

	visible := requestLibraries(t, svc, "user-1", "user", "/api/libraries")
	if len(visible) != 2 {
		t.Fatalf("visible list = %#v, want root plus auto category library", visible)
	}
	ids = map[string]bool{}
	for _, lib := range visible {
		ids[lib.ID] = true
	}
	if !ids[root.ID] || !ids[auto.ID] {
		t.Fatalf("visible list = %#v, want root %s and auto category %s", visible, root.ID, auto.ID)
	}

	got := requestLibrary(t, svc, "user-1", "user", "/api/libraries/"+auto.ID, auto.ID)
	if got.ID != auto.ID || got.Name != auto.Name {
		t.Fatalf("auto category detail = %#v, want accessible category library", got)
	}
}

func TestGetLibraryAllowsEmptyLibrary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "空媒体库", Path: "/media/empty", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	got := requestLibrary(t, svc, "user-1", "user", "/api/libraries/"+lib.ID, lib.ID)
	if got.ID != lib.ID || got.Name != "空媒体库" {
		t.Fatalf("library detail = %#v, want empty library detail", got)
	}
	media := requestMediaList(t, svc, "/api/libraries/"+lib.ID+"/media", lib.ID)
	if media.Total != 0 || len(media.Items) != 0 {
		t.Fatalf("empty library media = %#v, want no items", media)
	}
}

func TestListMediaGroupsMultipleVersionsByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "流浪地球", Year: 2019, Source: "local"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&[]model.Media{
		{
			PermanentBase: model.PermanentBase{ID: "movie-1080", CreatedAt: time.Now().Add(-time.Minute)},
			LibraryID:     lib.ID,
			MetadataID:    metadata.ID,
			Title:         "流浪地球",
			Path:          "/media/movies/The.Wandering.Earth.2019.1080p.mkv",
			Year:          2019,
			Width:         1920,
			Height:        1080,
			SizeBytes:     100,
		},
		{
			PermanentBase: model.PermanentBase{ID: "movie-2160", CreatedAt: time.Now()},
			LibraryID:     lib.ID,
			MetadataID:    metadata.ID,
			Title:         "流浪地球",
			Path:          "/media/movies/The.Wandering.Earth.2019.2160p.strm",
			STRMURL:       "https://media.example.test/The.Wandering.Earth.2019.2160p.mkv",
			Year:          2019,
			Width:         3840,
			Height:        2160,
			SizeBytes:     200,
		},
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	grouped := requestMediaList(t, svc, "/api/libraries/"+lib.ID+"/media", lib.ID)
	if grouped.Total != 1 || len(grouped.Items) != 1 {
		t.Fatalf("grouped response total=%d len=%d body=%#v", grouped.Total, len(grouped.Items), grouped)
	}
	if grouped.Items[0].ID != "movie-1080" {
		t.Fatalf("primary id = %q, want local version to remain primary", grouped.Items[0].ID)
	}
	if len(grouped.Items[0].Versions) != 2 {
		t.Fatalf("versions = %#v, want both versions", grouped.Items[0].Versions)
	}
	if grouped.Items[0].Versions[0].ID != "movie-1080" || grouped.Items[0].Versions[1].ID != "movie-2160" {
		t.Fatalf("versions should keep local before remote: %#v", grouped.Items[0].Versions)
	}

	raw := requestMediaList(t, svc, "/api/libraries/"+lib.ID+"/media?group_versions=0", lib.ID)
	if raw.Total != 2 || len(raw.Items) != 2 {
		t.Fatalf("raw response total=%d len=%d body=%#v", raw.Total, len(raw.Items), raw)
	}
}

func TestListMediaVersionsReturnsOnlyVisibleSiblings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	viewer := &model.User{Username: "viewer-versions", PasswordHash: "hash", Role: "user", HideAdult: true}
	if err := repos.User.Create(t.Context(), viewer); err != nil {
		t.Fatal(err)
	}
	safe := model.Library{Name: "电影", Path: "/media/movies", Type: "movie", Enabled: true}
	hidden := model.Library{Name: "隐藏库", Path: "/media/hidden", Type: "movie", Enabled: true}
	for _, lib := range []*model.Library{&safe, &hidden} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Setting.Set(t.Context(), service.AdultLibraryIDsSettingKey, `["`+hidden.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "同一作品", Source: "local"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&[]model.Media{
		{PermanentBase: model.PermanentBase{ID: "version-safe-1"}, LibraryID: safe.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: "/media/movies/version-1.mkv"},
		{PermanentBase: model.PermanentBase{ID: "version-safe-2"}, LibraryID: safe.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: "/media/movies/version-2.mkv"},
		{PermanentBase: model.PermanentBase{ID: "version-hidden"}, LibraryID: hidden.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: "/media/hidden/version-3.mkv"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	items := requestMediaVersions(t, svc, viewer.ID, "version-safe-1", http.StatusOK)
	if len(items) != 2 {
		t.Fatalf("visible versions = %#v, want two safe versions", items)
	}
	if err := repos.History.Upsert(t.Context(), &model.PlaybackHistory{
		UserID: viewer.ID, MetadataID: metadata.ID, MediaID: "version-safe-1", PositionMs: 30_000, DurationMs: 120_000,
	}); err != nil {
		t.Fatal(err)
	}
	items = requestMediaVersions(t, svc, viewer.ID, metadata.ID, http.StatusOK)
	if len(items) != 2 || items[0].ID != "version-safe-1" {
		t.Fatalf("preferred version = %#v, want last played version first", items)
	}
	detail := requestMediaDetail(t, svc, viewer.ID, metadata.ID)
	if detail.LibraryID != safe.ID {
		t.Fatalf("metadata detail library = %q, want visible library %q", detail.LibraryID, safe.ID)
	}
	for _, item := range items {
		if item.LibraryID != safe.ID {
			t.Fatalf("hidden version leaked: %#v", item)
		}
	}

	requestMediaVersions(t, svc, viewer.ID, "missing", http.StatusNotFound)
}

func TestGetMediaSTRMTargetReturnsOnlyVisibleTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	viewer := &model.User{Username: "viewer-strm-target", PasswordHash: "hash", Role: "user", HideAdult: true}
	if err := repos.User.Create(t.Context(), viewer); err != nil {
		t.Fatal(err)
	}
	safe := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	hidden := model.Library{Name: "隐藏库", Path: t.TempDir(), Type: "movie", Enabled: true}
	for _, lib := range []*model.Library{&safe, &hidden} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Setting.Set(t.Context(), service.AdultLibraryIDsSettingKey, `["`+hidden.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "STRM 电影", Source: "local"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	target := "https://cdn.example.test/movie.mkv"
	safePath := filepath.Join(safe.Path, "movie.strm")
	hiddenPath := filepath.Join(hidden.Path, "hidden.strm")
	for _, path := range []string{safePath, hiddenPath} {
		if err := os.WriteFile(path, []byte("# comment\n"+target+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.DB.Create(&[]model.Media{
		{PermanentBase: model.PermanentBase{ID: "strm-visible"}, LibraryID: safe.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: safePath},
		{PermanentBase: model.PermanentBase{ID: "strm-hidden"}, LibraryID: hidden.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: hiddenPath},
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{Repo: repos, Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos)}

	request := func(mediaID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(middleware.CtxUserID, viewer.ID)
		c.Set(middleware.CtxUserRole, "user")
		c.Params = gin.Params{{Key: "id", Value: mediaID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/api/media/"+mediaID+"/strm-target", nil)
		getMediaSTRMTargetHandler(svc)(c)
		return w
	}

	w := request("strm-visible")
	if w.Code != http.StatusOK {
		t.Fatalf("visible STRM target status = %d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload["target"] != target {
		t.Fatalf("STRM target response = %#v", payload)
	}
	if w := request("strm-hidden"); w.Code != http.StatusNotFound {
		t.Fatalf("hidden STRM target status = %d body=%s", w.Code, w.Body.String())
	}
	if w := request(metadata.ID); w.Code != http.StatusNotFound {
		t.Fatalf("metadata ID STRM target status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestListLibrarySeriesDoesNotTruncateLargeEpisodeLibraries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "国漫", Path: "/media/anime", Type: "anime", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	seriesMetadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "series-large"}, Kind: model.MetadataKindSeries, Title: "大剧", Source: "local"}
	if err := repos.DB.Create(&seriesMetadata).Error; err != nil {
		t.Fatal(err)
	}
	seasonMetadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "season-large"}, Kind: model.MetadataKindSeason, ParentID: &seriesMetadata.ID, SeasonNum: 1, Title: "Season 1", Source: "local"}
	if err := repos.DB.Create(&seasonMetadata).Error; err != nil {
		t.Fatal(err)
	}
	episodeMetadata := make([]model.MetadataItem, 0, 2001)
	rows := make([]model.Media, 0, 2001)
	for i := 1; i <= 2001; i++ {
		episodeID := fmt.Sprintf("episode-%04d", i)
		episodeMetadata = append(episodeMetadata, model.MetadataItem{
			PermanentBase: model.PermanentBase{ID: episodeID}, Kind: model.MetadataKindEpisode, ParentID: &seasonMetadata.ID,
			EpisodeNum: i, Title: fmt.Sprintf("Episode %d", i), Source: "local",
		})
		rows = append(rows, model.Media{
			PermanentBase: model.PermanentBase{ID: fmt.Sprintf("ep-%04d", i), CreatedAt: time.Now().Add(time.Duration(i) * time.Second)},
			LibraryID:     lib.ID,
			MetadataID:    episodeID,
			Title:         "大剧",
			Path:          fmt.Sprintf("/media/anime/大剧 (2026) {tmdb-123}/Season 1/大剧.S01E%04d.mkv", i),
			SeasonNum:     1,
			EpisodeNum:    i,
		})
	}
	if err := repos.DB.CreateInBatches(episodeMetadata, 500).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	series := requestLibrarySeries(t, svc, "/api/libraries/"+lib.ID+"/series", lib.ID)
	if series.Total != 1 || len(series.Items) != 1 {
		t.Fatalf("series response total=%d len=%d body=%#v", series.Total, len(series.Items), series)
	}
	if series.Items[0].Count != 2001 {
		t.Fatalf("series count = %d, want 2001", series.Items[0].Count)
	}
	if !strings.HasPrefix(series.Items[0].Key, "series:") ||
		strings.Contains(series.Items[0].Key, "lib:") ||
		strings.Contains(series.Items[0].Key, "show:") {
		t.Fatalf("series key = %q, want compact non-raw key", series.Items[0].Key)
	}
	episodes := requestLibrarySeriesEpisodes(t, svc, "/api/libraries/"+lib.ID+"/series/episodes?key="+url.QueryEscape(series.Items[0].Key), lib.ID)
	if episodes.Total != 2001 || len(episodes.Items) != 2001 {
		t.Fatalf("episodes total=%d len=%d, want 2001", episodes.Total, len(episodes.Items))
	}
	if episodes.Items[0].EpisodeNum != 1 || episodes.Items[len(episodes.Items)-1].EpisodeNum != 2001 {
		t.Fatalf("episode order first=%d last=%d", episodes.Items[0].EpisodeNum, episodes.Items[len(episodes.Items)-1].EpisodeNum)
	}
}

func TestScanLibraryHandlerQueuesLocalScan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.LibraryRoot{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	svc := &service.Container{
		Log:   log,
		Repo:  repos,
		Scan:  service.NewScannerService(&config.Config{}, log, repos, service.NewHub(log), nil, nil),
		Tasks: service.NewTaskTrackerService(log, nil),
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: lib.ID}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/libraries/"+lib.ID+"/scan", nil)

	scanLibraryHandler(svc)(c)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want accepted", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"queued":true`) {
		t.Fatalf("body=%s, want queued local scan", w.Body.String())
	}
}

func TestStartLibraryRootScanTaskTracksEventAndTargetsRoot(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.LibraryRoot{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "Movie.A.mkv"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "Movie.B.mkv"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "电影", Path: rootA, Type: "movie", Enabled: true}
	roots := []model.LibraryRoot{{Path: rootA, Enabled: true}, {Path: rootB, Enabled: true}}
	if err := repos.Library.CreateWithRoots(t.Context(), &lib, roots); err != nil {
		t.Fatal(err)
	}
	persistedRoots, err := repos.Library.ListRoots(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	tracker := service.NewTaskTrackerService(log, nil)
	svc := &service.Container{
		Log:   log,
		Repo:  repos,
		Scan:  service.NewScannerService(&config.Config{}, log, repos, service.NewHub(log), nil, nil),
		Tasks: tracker,
	}

	started, err := startLibraryRootScanTask(svc, lib.ID, persistedRoots[1].ID, lib.Name, persistedRoots[1].Path, service.TaskTriggerEvent, "新增路径自动扫描")
	if err != nil || !started {
		t.Fatalf("started = %v, err = %v", started, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		recent := tracker.Snapshot().Recent
		if len(recent) == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		if recent[0].Trigger != service.TaskTriggerEvent || recent[0].Status != service.TaskStatusCompleted {
			t.Fatalf("task = %#v", recent[0])
		}
		var paths []string
		if err := db.Model(&model.Media{}).Pluck("path", &paths).Error; err != nil {
			t.Fatal(err)
		}
		if len(paths) != 1 || paths[0] != filepath.Join(rootB, "Movie.B.mkv") {
			t.Fatalf("media paths = %#v, want only root B", paths)
		}
		return
	}
	t.Fatal("event scan task did not finish")
}

func TestScrapeOptionsFromRequestPreservesEpisodeImagesFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/media/ep-1/scrape", bytes.NewBufferString(`{"episode_images":false,"refresh_matched":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	options, err := scrapeOptionsFromRequest(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if options.EpisodeArtwork == nil {
		t.Fatal("EpisodeArtwork is nil, want explicit false")
	}
	if *options.EpisodeArtwork {
		t.Fatal("EpisodeArtwork = true, want false")
	}
	if !options.IncludeMatched {
		t.Fatal("IncludeMatched = false, want true from refresh_matched")
	}
}

func requestLibraries(t *testing.T, svc *service.Container, userID, role, path string) []model.Library {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, userID)
	c.Set(middleware.CtxUserRole, role)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	listLibrariesHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, w.Code, w.Body.String())
	}
	var libs []model.Library
	if err := json.Unmarshal(w.Body.Bytes(), &libs); err != nil {
		t.Fatalf("decode libraries: %v", err)
	}
	return libs
}

func requestLibrary(t *testing.T, svc *service.Container, userID, role, path, libraryID string) model.Library {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, userID)
	c.Set(middleware.CtxUserRole, role)
	c.Params = gin.Params{{Key: "id", Value: libraryID}}
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	getLibraryHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, w.Code, w.Body.String())
	}
	var lib model.Library
	if err := json.Unmarshal(w.Body.Bytes(), &lib); err != nil {
		t.Fatalf("decode library: %v", err)
	}
	return lib
}

type mediaListResponse struct {
	Items []service.MediaItem `json:"items"`
	Total int64               `json:"total"`
}

type seriesListResponse struct {
	Items []service.SeriesCard `json:"items"`
	Total int64                `json:"total"`
}

type seriesEpisodesResponse struct {
	Items []model.MediaView `json:"items"`
	Total int64             `json:"total"`
}

func requestMediaList(t *testing.T, svc *service.Container, path, libraryID string) mediaListResponse {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, "user-1")
	c.Set(middleware.CtxUserRole, "user")
	c.Params = gin.Params{{Key: "id", Value: libraryID}}
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	listMediaHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, w.Code, w.Body.String())
	}
	var payload mediaListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode media list: %v", err)
	}
	return payload
}

func requestMediaVersions(t *testing.T, svc *service.Container, userID, mediaID string, wantStatus int) []model.MediaView {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, userID)
	c.Set(middleware.CtxUserRole, "user")
	c.Params = gin.Params{{Key: "id", Value: mediaID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/media/"+mediaID+"/versions", nil)
	listMediaVersionsHandler(svc)(c)
	if w.Code != wantStatus {
		t.Fatalf("GET media versions status = %d body=%s, want %d", w.Code, w.Body.String(), wantStatus)
	}
	if wantStatus != http.StatusOK {
		return nil
	}
	var items []model.MediaView
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode media versions: %v", err)
	}
	return items
}

func requestMediaDetail(t *testing.T, svc *service.Container, userID, mediaID string) model.MediaView {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, userID)
	c.Set(middleware.CtxUserRole, "user")
	c.Params = gin.Params{{Key: "id", Value: mediaID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/media/"+mediaID, nil)
	getMediaHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET media detail status = %d body=%s", w.Code, w.Body.String())
	}
	var item model.MediaView
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode media detail: %v", err)
	}
	return item
}

func requestLibrarySeries(t *testing.T, svc *service.Container, path, libraryID string) seriesListResponse {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, "user-1")
	c.Set(middleware.CtxUserRole, "user")
	c.Params = gin.Params{{Key: "id", Value: libraryID}}
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	listLibrarySeriesHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, w.Code, w.Body.String())
	}
	var payload seriesListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode series list: %v", err)
	}
	return payload
}

func requestLibrarySeriesEpisodes(t *testing.T, svc *service.Container, path, libraryID string) seriesEpisodesResponse {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, "user-1")
	c.Set(middleware.CtxUserRole, "user")
	c.Params = gin.Params{{Key: "id", Value: libraryID}}
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	listLibrarySeriesEpisodesHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, w.Code, w.Body.String())
	}
	var payload seriesEpisodesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode series episodes: %v", err)
	}
	return payload
}

// 空库的 media / series 列表必须返回 "items":[]（而不是 Go nil slice 序列化出的 null）。
// 前端 [].concat(null) 会得到 [null]，随后在渲染期解引用 null 崩溃，导致「空库点进去
// 报错且无法返回」。这条测试钉死该 JSON 契约，防止再退化。
func TestEmptyLibraryListsReturnEmptyArraysNotNull(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	movie := model.Library{Name: "空电影库", Path: "/media/empty-movie", Type: "movie", Enabled: true}
	tv := model.Library{Name: "空剧集库", Path: "/media/empty-tv", Type: "tv", Enabled: true}
	for _, lib := range []*model.Library{&movie, &tv} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	cases := []struct {
		name    string
		path    string
		lib     string
		handler gin.HandlerFunc
	}{
		{"media", "/api/libraries/" + movie.ID + "/media", movie.ID, listMediaHandler(svc)},
		{"series", "/api/libraries/" + tv.ID + "/series", tv.ID, listLibrarySeriesHandler(svc)},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(middleware.CtxUserID, "user-1")
		c.Set(middleware.CtxUserRole, "user")
		c.Params = gin.Params{{Key: "id", Value: tc.lib}}
		c.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
		tc.handler(c)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d body=%s", tc.name, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if strings.Contains(body, `"items":null`) {
			t.Fatalf("%s: empty library returned items:null (crashes frontend): %s", tc.name, body)
		}
		if !strings.Contains(body, `"items":[]`) {
			t.Fatalf("%s: expected items:[] for empty library, got %s", tc.name, body)
		}
	}
}

func migrateMediaHandlerTestDB(db *gorm.DB, models ...any) error {
	models = append(models,
		&model.MetadataItem{}, &model.MetadataIdentifier{},
		&model.ArtworkAsset{}, &model.MetadataArtwork{},
		&model.Person{}, &model.MetadataCredit{},
	)
	if err := db.AutoMigrate(models...); err != nil {
		return err
	}
	return testutil.RegisterMediaMetadataFixtures(db)
}
