package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestEmbyLatestItemsStayWithinRequestedLibrary(t *testing.T) {
	svc := newTestEmbyService(t)
	local := model.Library{Name: "国产电影", Path: `/media/国产电影`, Type: "movie", Enabled: true}
	other := model.Library{Name: "其他电影", Path: `/media/其他电影`, Type: "movie", Enabled: true}
	for _, lib := range []*model.Library{&local, &other} {
		if err := svc.repo.Library.Create(t.Context(), lib); err != nil {
			t.Fatalf("create library: %v", err)
		}
	}
	media := []model.Media{
		{
			PermanentBase: model.PermanentBase{ID: "local-movie", CreatedAt: time.Now().Add(-time.Minute)},
			LibraryID:     local.ID,
			Title:         "本地版本",
			Path:          `/media/国产电影/local.mkv`,
		},
		{
			PermanentBase: model.PermanentBase{ID: "other-movie", CreatedAt: time.Now()},
			LibraryID:     other.ID,
			Title:         "其他版本",
			Path:          `/media/其他电影/other.mkv`,
		},
	}
	for i := range media {
		if err := svc.repo.DB.Create(&media[i]).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	latest, err := svc.LatestItems(t.Context(), "user-1", local.ID, 10, false)
	if err != nil {
		t.Fatalf("latest items: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("latest items = %#v, want requested library only", latest)
	}
	if latest[0]["Id"] != media[0].MetadataID {
		t.Fatalf("latest item = %#v, want local media", latest)
	}
}

func TestEmbyLocalAndHTTPMovieVersionsShareMediaSources(t *testing.T) {
	svc := newTestEmbyService(t)
	local := model.Library{Name: "国产电影", Path: `/media/国产电影`, Type: "movie", Enabled: true}
	remote := model.Library{Name: "远程电影", Path: `/media/远程电影`, Type: "movie", Enabled: true}
	for _, lib := range []*model.Library{&local, &remote} {
		if err := svc.repo.Library.Create(t.Context(), lib); err != nil {
			t.Fatalf("create library: %v", err)
		}
	}
	metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		Kind: model.MetadataKindMovie, Title: "流浪地球", Year: 2019, Source: "local",
	})
	for _, media := range []model.Media{
		{
			PermanentBase: model.PermanentBase{ID: "local-version", CreatedAt: time.Now()},
			LibraryID:     local.ID,
			MetadataID:    metadata.ID,
			Title:         "流浪地球",
			Year:          2019,
			Path:          `/media/国产电影/流浪地球.2019.1080p.mkv`,
			Container:     "mkv",
			Width:         1920,
		},
		{
			PermanentBase: model.PermanentBase{ID: "remote-version", CreatedAt: time.Now().Add(time.Minute)},
			LibraryID:     remote.ID,
			MetadataID:    metadata.ID,
			Title:         "流浪地球",
			Year:          2019,
			Path:          `https://example.invalid/流浪地球.2019.2160p.mkv`,
			Container:     "mkv",
			STRMURL:       "https://example.invalid/流浪地球.2019.2160p.mkv",
			Width:         3840,
		},
	} {
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	items, err := svc.Items(t.Context(), ItemsParams{ParentID: local.ID, IncludeItemTypes: []string{"Movie"}, Recursive: true, Limit: 10})
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	rows := items["Items"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("local/HTTP versions should show as one item, got %#v", rows)
	}
	if rows[0]["Id"] != metadata.ID {
		t.Fatalf("item id should use shared metadata, got %#v", rows[0])
	}
	sources := rows[0]["MediaSources"].([]map[string]any)
	if len(sources) != 2 {
		t.Fatalf("item should expose two media sources, got %#v", sources)
	}

	playback, err := svc.PlaybackInfo(t.Context(), "local-version", "user-1")
	if err != nil {
		t.Fatalf("playback: %v", err)
	}
	playSources := playback["MediaSources"].([]map[string]any)
	if len(playSources) != 2 {
		t.Fatalf("playback should expose local and HTTP versions, got %#v", playSources)
	}
}

func TestEmbyCountsSharedMetadataOnce(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-counted-once"}, Kind: model.MetadataKindMovie,
		Title: "同一作品", Source: "tmdb",
	})
	for _, media := range []model.Media{
		{PermanentBase: model.PermanentBase{ID: "count-version-1"}, LibraryID: lib.ID, MetadataID: metadata.ID, Title: "同一作品", Path: `/media/movies/same-1080p.mkv`},
		{PermanentBase: model.PermanentBase{ID: "count-version-2"}, LibraryID: lib.ID, MetadataID: metadata.ID, Title: "同一作品", Path: `/media/movies/same-2160p.mkv`},
	} {
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create media version: %v", err)
		}
	}

	items, err := svc.Items(t.Context(), ItemsParams{
		ParentID: lib.ID, IncludeItemTypes: []string{"Movie"}, Recursive: true, Limit: 10,
	})
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if items["TotalRecordCount"] != int64(1) {
		t.Fatalf("shared metadata should count once, got %#v", items)
	}

	counts, err := svc.ItemCounts(t.Context(), "user-1")
	if err != nil {
		t.Fatalf("item counts: %v", err)
	}
	if counts["ItemCount"] != int64(1) || counts["MovieCount"] != int64(1) {
		t.Fatalf("shared metadata counts = %#v, want one item and one movie", counts)
	}
}

func TestEmbyVersionPaginationFillsLogicalPage(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	first := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-many-versions"}, Kind: model.MetadataKindMovie,
		Title: "多版本作品", Source: "tmdb",
	})
	second := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-second-work"}, Kind: model.MetadataKindMovie,
		Title: "第二部作品", Source: "tmdb",
	})
	for i := 0; i < 9; i++ {
		media := model.Media{
			PermanentBase: model.PermanentBase{ID: "many-version-" + strconv.Itoa(i), CreatedAt: time.Now().Add(time.Duration(i) * time.Minute)},
			LibraryID:     lib.ID, MetadataID: first.ID, Title: first.Title,
			Path: fmt.Sprintf(`/media/movies/many-%d.mkv`, i),
		}
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create many-version media: %v", err)
		}
	}
	if err := svc.repo.DB.Create(&model.Media{
		PermanentBase: model.PermanentBase{ID: "second-work-version", CreatedAt: time.Now().Add(-time.Hour)},
		LibraryID:     lib.ID, MetadataID: second.ID, Title: second.Title, Path: `/media/movies/second.mkv`,
	}).Error; err != nil {
		t.Fatalf("create second media: %v", err)
	}

	items, err := svc.Items(t.Context(), ItemsParams{
		ParentID: lib.ID, IncludeItemTypes: []string{"Movie"}, Recursive: true, Limit: 2,
	})
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	rows := items["Items"].([]map[string]any)
	if len(rows) != 2 || items["TotalRecordCount"] != int64(2) {
		t.Fatalf("logical page = %#v, want two works", items)
	}
}

func TestEmbySharedMetadataAcrossIndependentLibrariesUsesOneGlobalItemAndVisibleSources(t *testing.T) {
	svc := newTestEmbyService(t)
	if err := svc.repo.DB.AutoMigrate(&model.PlayProfile{}); err != nil {
		t.Fatalf("migrate play profiles: %v", err)
	}
	firstLibrary := model.Library{Name: "电影一", Path: `/media/movies-a`, Type: "movie", Enabled: true}
	secondLibrary := model.Library{Name: "电影二", Path: `/media/movies-b`, Type: "movie", Enabled: true}
	for _, library := range []*model.Library{&firstLibrary, &secondLibrary} {
		if err := svc.repo.Library.Create(t.Context(), library); err != nil {
			t.Fatalf("create library: %v", err)
		}
	}
	metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-cross-library"}, Kind: model.MetadataKindMovie,
		Title: "跨库作品", Source: "tmdb",
	})
	createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-catalog-only"}, Kind: model.MetadataKindMovie,
		Title: metadata.Title, Source: "tmdb",
	})
	versions := []model.Media{
		{PermanentBase: model.PermanentBase{ID: "cross-library-a"}, LibraryID: firstLibrary.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: `/media/movies-a/work.1080p.mkv`},
		{PermanentBase: model.PermanentBase{ID: "cross-library-b"}, LibraryID: secondLibrary.ID, MetadataID: metadata.ID, Title: metadata.Title, Path: `/media/movies-b/work.2160p.mkv`},
	}
	if err := svc.repo.DB.Create(&versions).Error; err != nil {
		t.Fatalf("create versions: %v", err)
	}

	for _, libraryID := range []string{firstLibrary.ID, secondLibrary.ID} {
		out, err := svc.Items(t.Context(), ItemsParams{ParentID: libraryID, IncludeItemTypes: []string{"Movie"}, Recursive: true, Limit: 10})
		if err != nil {
			t.Fatalf("library items: %v", err)
		}
		if rows := out["Items"].([]map[string]any); len(rows) != 1 || rows[0]["Id"] != metadata.ID {
			t.Fatalf("library %q items = %#v, want shared work once", libraryID, out)
		}
	}

	global, err := svc.Items(t.Context(), ItemsParams{SearchTerm: metadata.Title, Recursive: true, Limit: 10})
	if err != nil {
		t.Fatalf("global items: %v", err)
	}
	if rows := global["Items"].([]map[string]any); len(rows) != 1 || global["TotalRecordCount"] != int64(1) {
		t.Fatalf("global shared work = %#v, want one logical item", global)
	}

	playback, err := svc.PlaybackInfo(t.Context(), versions[0].ID, "")
	if err != nil {
		t.Fatalf("unrestricted playback: %v", err)
	}
	if sources := playback["MediaSources"].([]map[string]any); len(sources) != 2 {
		t.Fatalf("unrestricted sources = %#v, want both libraries", sources)
	}

	viewer := model.User{Username: "cross-library-viewer", Role: "user", IsActive: true}
	if err := svc.repo.User.Create(t.Context(), &viewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if err := svc.repo.DB.Create(&model.PlayProfile{
		UserID: viewer.ID, Name: "受限", IsDefault: true,
		AllowedLibraryIDs: `["` + firstLibrary.ID + `"]`,
	}).Error; err != nil {
		t.Fatalf("create play profile: %v", err)
	}
	restricted, err := svc.Item(t.Context(), metadata.ID, viewer.ID)
	if err != nil {
		t.Fatalf("restricted item: %v", err)
	}
	if sources := restricted["MediaSources"].([]map[string]any); len(sources) != 1 || sources[0]["Id"] != versions[0].ID {
		t.Fatalf("restricted sources = %#v, want only allowed library", sources)
	}
	if hidden, err := svc.PlaybackInfo(t.Context(), versions[1].ID, viewer.ID); err != nil || hidden != nil {
		t.Fatalf("hidden concrete source playback = %#v, %v; want nil", hidden, err)
	}
}

func TestEmbyMetadataVersionsShareUserStateAndKeepSourceIDs(t *testing.T) {
	svc := newTestEmbyService(t)
	if err := svc.repo.DB.AutoMigrate(&model.Playlist{}, &model.PlaylistItem{}); err != nil {
		t.Fatalf("migrate playlists: %v", err)
	}
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-shared-movie"}, Kind: model.MetadataKindMovie,
		Title: "共享电影", Source: "tmdb",
	})
	media1080 := model.Media{
		PermanentBase: model.PermanentBase{ID: "shared-1080"}, LibraryID: lib.ID, MetadataID: metadata.ID,
		Title: "共享电影", Path: `/media/movies/shared.1080p.mkv`, Width: 1920, DurationSec: 120,
	}
	media2160 := model.Media{
		PermanentBase: model.PermanentBase{ID: "shared-2160"}, LibraryID: lib.ID, MetadataID: metadata.ID,
		Title: "共享电影", Path: `/media/movies/shared.2160p.mkv`, Width: 3840, DurationSec: 120,
	}
	if err := svc.repo.DB.Create(&[]model.Media{media1080, media2160}).Error; err != nil {
		t.Fatalf("create media versions: %v", err)
	}

	out, err := svc.Items(t.Context(), ItemsParams{UserID: "user-1", ParentID: lib.ID, IncludeItemTypes: []string{"Movie"}, Recursive: true, Limit: 10})
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	items := out["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Id"] != metadata.ID {
		t.Fatalf("versions should expose one metadata item id: %#v", items)
	}
	sources := items[0]["MediaSources"].([]map[string]any)
	if len(sources) != 2 {
		t.Fatalf("metadata item should expose two sources: %#v", sources)
	}
	sourceIDs := []string{sources[0]["Id"].(string), sources[1]["Id"].(string)}
	sort.Strings(sourceIDs)
	if sourceIDs[0] != media1080.ID || sourceIDs[1] != media2160.ID {
		t.Fatalf("media sources should keep concrete media ids: %#v", sources)
	}

	if err := svc.SetFavorite(t.Context(), "user-1", metadata.ID, true); err != nil {
		t.Fatalf("set favorite: %v", err)
	}
	if err := svc.RecordProgress(t.Context(), "user-1", metadata.ID, media1080.ID, "", 30_000*10_000, 0); err != nil {
		t.Fatalf("unknown runtime should be ignored: %v", err)
	}
	var historyCount int64
	if err := svc.repo.DB.Model(&model.PlaybackHistory{}).Where("user_id = ?", "user-1").Count(&historyCount).Error; err != nil || historyCount != 0 {
		t.Fatalf("unknown runtime history count = %d, err = %v", historyCount, err)
	}
	if err := svc.repo.MediaProbe.Upsert(t.Context(), &model.MediaProbeMetadata{
		MediaID: media1080.ID, ProbeJSON: `{"schema_version":1,"format":{"duration":120},"streams":[]}`,
		SchemaVersion: ProbeDocumentSchemaVersion, SummaryVersion: ProbeSummaryVersion, DurationMS: 120_000, ProbedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create probe summary: %v", err)
	}
	fullViewQueries := 0
	callbackName := "test:record-progress-media-source-fast-path"
	if err := svc.repo.DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "metadata_identifiers") {
			fullViewQueries++
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	if view, err := svc.repo.MediaView.FindByID(t.Context(), media1080.ID); err != nil || view == nil {
		t.Fatalf("load full media view control: %#v, %v", view, err)
	}
	if fullViewQueries == 0 {
		t.Fatal("query callback did not detect the full media view control")
	}
	fullViewQueries = 0
	if err := svc.RecordProgress(t.Context(), "user-1", metadata.ID, media1080.ID, "", 30_000*10_000, 0); err != nil {
		t.Fatalf("record progress: %v", err)
	}
	if err := svc.repo.DB.Callback().Query().Remove(callbackName); err != nil {
		t.Fatalf("remove query callback: %v", err)
	}
	if fullViewQueries != 0 {
		t.Fatalf("record progress full media view queries = %d, want 0", fullViewQueries)
	}
	var favorite model.Favorite
	if err := svc.repo.DB.Where("user_id = ?", "user-1").First(&favorite).Error; err != nil {
		t.Fatalf("find favorite: %v", err)
	}
	if favorite.MetadataID != metadata.ID {
		t.Fatalf("favorite metadata id = %q, want %q", favorite.MetadataID, metadata.ID)
	}
	var history model.PlaybackHistory
	if err := svc.repo.DB.Where("user_id = ?", "user-1").First(&history).Error; err != nil {
		t.Fatalf("find history: %v", err)
	}
	if history.MetadataID != metadata.ID || history.MediaID != media1080.ID {
		t.Fatalf("history should keep metadata and last source ids: %#v", history)
	}
	if history.DurationMs != 120_000 {
		t.Fatalf("history duration = %d", history.DurationMs)
	}
	unrelatedMetadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-unrelated-progress"}, Kind: model.MetadataKindMovie,
		Title: "其他电影", Source: "tmdb",
	})
	unrelatedMedia := model.Media{
		PermanentBase: model.PermanentBase{ID: "media-unrelated-progress"}, LibraryID: lib.ID, MetadataID: unrelatedMetadata.ID,
		Title: "其他电影", Path: `/media/movies/unrelated.mkv`, DurationSec: 120,
	}
	if err := svc.repo.DB.Create(&unrelatedMedia).Error; err != nil {
		t.Fatalf("create unrelated media: %v", err)
	}
	if err := svc.RecordProgress(t.Context(), "user-2", metadata.ID, unrelatedMedia.ID, "", 30_000*10_000, 120_000*10_000); err != nil {
		t.Fatalf("record progress with mismatched source: %v", err)
	}
	var mismatchedHistory model.PlaybackHistory
	if err := svc.repo.DB.Where("user_id = ?", "user-2").First(&mismatchedHistory).Error; err != nil {
		t.Fatalf("find mismatched-source history: %v", err)
	}
	if mismatchedHistory.MetadataID != metadata.ID || mismatchedHistory.MediaID == unrelatedMedia.ID {
		t.Fatalf("mismatched source changed progress target: %#v", mismatchedHistory)
	}
	item, err := svc.Item(t.Context(), media2160.ID, "user-1")
	if err != nil {
		t.Fatalf("item through other version: %v", err)
	}
	userData := item["UserData"].(map[string]any)
	if item["Id"] != metadata.ID || userData["IsFavorite"] != true || userData["PlaybackPositionTicks"] != int64(30_000*10_000) {
		t.Fatalf("other version should share favorite and progress: %#v", item)
	}

	playback := NewPlaybackService(zap.NewNop(), svc.repo)
	playlist := model.Playlist{UserID: "user-1", Name: "稍后观看"}
	if err := svc.repo.DB.Create(&playlist).Error; err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if err := playback.AddToPlaylist(t.Context(), playlist.ID, media1080.ID); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}
	var playlistItem model.PlaylistItem
	if err := svc.repo.DB.Where("playlist_id = ?", playlist.ID).First(&playlistItem).Error; err != nil {
		t.Fatalf("find playlist item: %v", err)
	}
	if playlistItem.MetadataID != metadata.ID || playlistItem.MediaID != media1080.ID {
		t.Fatalf("playlist item should keep metadata and preferred source ids: %#v", playlistItem)
	}

	if err := svc.repo.DB.Unscoped().Delete(&model.Media{}, "id = ?", media1080.ID).Error; err != nil {
		t.Fatalf("delete last played version: %v", err)
	}
	resume, err := svc.ResumeItems(t.Context(), "user-1", 10)
	if err != nil {
		t.Fatalf("resume after source deletion: %v", err)
	}
	resumeItems := resume["Items"].([]map[string]any)
	if len(resumeItems) != 1 || resumeItems[0]["Id"] != metadata.ID {
		t.Fatalf("state should survive concrete source deletion: %#v", resumeItems)
	}
	detail, err := playback.GetPlaylist(t.Context(), playlist.ID, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatalf("playlist after source deletion: %v", err)
	}
	if len(detail.Items) != 1 || detail.Items[0].ID != media2160.ID {
		t.Fatalf("playlist should fall back to another metadata version: %#v", detail.Items)
	}
}

func TestEmbyLatestItemsCollapsesMovieVersions(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	now := time.Now()
	media := []model.Media{
		{
			PermanentBase: model.PermanentBase{ID: "dune-1080", CreatedAt: now.Add(time.Minute)},
			LibraryID:     lib.ID,
			Title:         "Dune",
			Year:          2021,
			TMDbID:        438631,
			Path:          `/media/movies/Dune.2021.1080p.mkv`,
			Width:         1920,
			SizeBytes:     100,
		},
		{
			PermanentBase: model.PermanentBase{ID: "dune-2160", CreatedAt: now.Add(2 * time.Minute)},
			LibraryID:     lib.ID,
			Title:         "Dune",
			Year:          2021,
			TMDbID:        438631,
			Path:          `/media/movies/Dune.2021.2160p.mkv`,
			Width:         3840,
			SizeBytes:     200,
		},
		{
			PermanentBase: model.PermanentBase{ID: "matrix", CreatedAt: now},
			LibraryID:     lib.ID,
			Title:         "The Matrix",
			Year:          1999,
			TMDbID:        603,
			Path:          `/media/movies/The.Matrix.1999.mkv`,
		},
	}
	for i := range media {
		if err := svc.repo.DB.Create(&media[i]).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	latest, err := svc.LatestItems(t.Context(), "user-1", lib.ID, 10, false)
	if err != nil {
		t.Fatalf("latest items: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("latest items = %#v, want Dune collapsed plus Matrix", latest)
	}
	if latest[0]["Id"] != media[0].MetadataID {
		t.Fatalf("latest first item = %#v, want best Dune version", latest[0])
	}
	sources := latest[0]["MediaSources"].([]map[string]any)
	if len(sources) != 2 {
		t.Fatalf("collapsed latest item should expose both versions, got %#v", sources)
	}
}

func TestEmbyLatestItemsPaginatesMetadataBeforeLoadingVersions(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/latest`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	first := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-latest-many"}, Kind: model.MetadataKindMovie,
		Title: "多版本新片", ReleaseDate: "2026-08-08", Source: "tmdb",
	})
	second := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		PermanentBase: model.PermanentBase{ID: "metadata-latest-second"}, Kind: model.MetadataKindMovie,
		Title: "第二部新片", ReleaseDate: "2026-08-07", Source: "tmdb",
	})
	now := time.Now()
	versions := make([]model.Media, 101, 102)
	for i := range versions {
		versions[i] = model.Media{
			PermanentBase: model.PermanentBase{ID: fmt.Sprintf("latest-many-%03d", i), CreatedAt: now.Add(time.Duration(i) * time.Second)},
			LibraryID:     lib.ID, MetadataID: first.ID, Title: first.Title,
			Path: fmt.Sprintf(`/media/latest/many-%03d.mkv`, i),
		}
	}
	versions = append(versions, model.Media{
		PermanentBase: model.PermanentBase{ID: "latest-second", CreatedAt: now.Add(-time.Hour)},
		LibraryID:     lib.ID, MetadataID: second.ID, Title: second.Title, Path: `/media/latest/second.mkv`,
	})
	if err := svc.repo.DB.Create(&versions).Error; err != nil {
		t.Fatalf("create latest versions: %v", err)
	}

	latest, err := svc.LatestItems(t.Context(), "", lib.ID, 2, false)
	if err != nil {
		t.Fatalf("latest items: %v", err)
	}
	if len(latest) != 2 || latest[0]["Id"] != first.ID || latest[1]["Id"] != second.ID {
		t.Fatalf("latest logical page = %#v, want both works", latest)
	}
}
