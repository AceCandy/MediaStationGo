package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

func TestEmbyRootItemsExposeLibraries(t *testing.T) {
	svc := newTestEmbyService(t)
	for _, lib := range []model.Library{
		{Name: "电影", Path: `F:\downloads\电影`, Type: "movie", Enabled: true},
		{Name: "综艺", Path: `F:\downloads\综艺`, Type: "variety", Enabled: true},
	} {
		if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
			t.Fatalf("create library: %v", err)
		}
	}

	root, err := svc.Items(t.Context(), ItemsParams{Limit: 50})
	if err != nil {
		t.Fatalf("root items: %v", err)
	}
	items := root["Items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("expected root items to expose libraries, got %#v", items)
	}
	if items[0]["Type"] != "CollectionFolder" || items[1]["Type"] != "CollectionFolder" {
		t.Fatalf("root should return collection folders: %#v", items)
	}
	if items[1]["CollectionType"] != "tvshows" {
		t.Fatalf("variety libraries should use tvshows collection type: %#v", items[1])
	}
}

func TestEmbyFolderItemQueryExposesLibrariesForHome(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := svc.repo.DB.Create(&model.Media{Base: model.Base{ID: "movie-1"}, LibraryID: lib.ID, Title: "不应出现在文件夹查询", Path: `/media/movies/a.mkv`}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	out, err := svc.Items(t.Context(), ItemsParams{
		IncludeItemTypes: []string{"Folder", "CollectionFolder"},
		Limit:            50,
	})
	if err != nil {
		t.Fatalf("folder items: %v", err)
	}
	items := out["Items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("expected one library folder, got %#v", items)
	}
	if items[0]["Type"] != "CollectionFolder" || items[0]["IsFolder"] != true {
		t.Fatalf("folder query should return collection folders, got %#v", items[0])
	}
}

func TestEmbyUnsupportedItemTypesDoNotLeakAllMedia(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := svc.repo.DB.Create(&model.Media{Base: model.Base{ID: "movie-1"}, LibraryID: lib.ID, Title: "普通电影", Path: `/media/movies/a.mkv`}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	for _, includeType := range []string{"BoxSet", "Game", "Book", "Audio", "MusicAlbum", "Playlist", "TvChannel"} {
		out, err := svc.Items(t.Context(), ItemsParams{
			IncludeItemTypes: []string{includeType},
			Recursive:        true,
			Limit:            50,
		})
		if err != nil {
			t.Fatalf("%s items: %v", includeType, err)
		}
		if out["TotalRecordCount"] != int64(0) {
			t.Fatalf("%s should not return media rows, got %#v", includeType, out)
		}
		items := out["Items"].([]map[string]any)
		if len(items) != 0 {
			t.Fatalf("%s should return an empty list, got %#v", includeType, items)
		}
	}
}

func TestEmbyItemsFiltersFavorites(t *testing.T) {
	svc := newTestEmbyService(t)
	viewer := &model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "user", Tier: "free", IsActive: true}
	if err := svc.repo.User.Create(t.Context(), viewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	favorite := model.Media{Base: model.Base{ID: "fav-1"}, LibraryID: lib.ID, Title: "收藏电影", Path: `/media/movies/fav.mkv`}
	normal := model.Media{Base: model.Base{ID: "normal-1"}, LibraryID: lib.ID, Title: "普通电影", Path: `/media/movies/normal.mkv`}
	if err := svc.repo.DB.Create(&favorite).Error; err != nil {
		t.Fatalf("create favorite media: %v", err)
	}
	if err := svc.repo.DB.Create(&normal).Error; err != nil {
		t.Fatalf("create normal media: %v", err)
	}
	if err := svc.repo.DB.Create(&model.Favorite{UserID: viewer.ID, MetadataID: favorite.MetadataID, MediaID: favorite.ID}).Error; err != nil {
		t.Fatalf("create favorite: %v", err)
	}

	out, err := svc.Items(t.Context(), ItemsParams{
		UserID:    viewer.ID,
		Filters:   []string{"IsFavorite"},
		Recursive: true,
		Limit:     50,
	})
	if err != nil {
		t.Fatalf("favorite items: %v", err)
	}
	if out["TotalRecordCount"] != int64(1) {
		t.Fatalf("expected one favorite, got %#v", out)
	}
	items := out["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Id"] != favorite.MetadataID {
		t.Fatalf("favorite filter returned wrong items: %#v", items)
	}
	userData := items[0]["UserData"].(map[string]any)
	if userData["IsFavorite"] != true {
		t.Fatalf("favorite payload should carry IsFavorite=true: %#v", userData)
	}
}

func TestEmbyItemsFiltersResumableForHome(t *testing.T) {
	svc := newTestEmbyService(t)
	viewer := &model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "user", Tier: "free", IsActive: true}
	if err := svc.repo.User.Create(t.Context(), viewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	resumable := model.Media{Base: model.Base{ID: "resume-1"}, LibraryID: lib.ID, Title: "继续观看", Path: `/media/movies/resume.mkv`, DurationSec: 120}
	normal := model.Media{Base: model.Base{ID: "normal-1"}, LibraryID: lib.ID, Title: "普通电影", Path: `/media/movies/normal.mkv`, DurationSec: 120}
	if err := svc.repo.DB.Create(&resumable).Error; err != nil {
		t.Fatalf("create resumable media: %v", err)
	}
	if err := svc.repo.DB.Create(&normal).Error; err != nil {
		t.Fatalf("create normal media: %v", err)
	}
	if err := svc.repo.DB.Create(&model.PlaybackHistory{
		UserID:     viewer.ID,
		MetadataID: resumable.MetadataID,
		MediaID:    resumable.ID,
		PositionMs: 30_000,
		DurationMs: 120_000,
		WatchedAt:  time.Now(),
		Completed:  false,
	}).Error; err != nil {
		t.Fatalf("create playback history: %v", err)
	}

	out, err := svc.Items(t.Context(), ItemsParams{
		UserID:     viewer.ID,
		Filters:    []string{"IsResumable"},
		Recursive:  true,
		SortBy:     "DatePlayed",
		SortOrder:  "Descending",
		Limit:      50,
		StartIndex: 0,
	})
	if err != nil {
		t.Fatalf("resumable items: %v", err)
	}
	if out["TotalRecordCount"] != int64(1) {
		t.Fatalf("expected one resumable item, got %#v", out)
	}
	items := out["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Id"] != resumable.MetadataID {
		t.Fatalf("resumable filter returned wrong items: %#v", items)
	}
}

func TestEmbyUserPolicyDisablesDownloadsForViewers(t *testing.T) {
	svc := newTestEmbyService(t)
	viewer := &model.User{Username: "viewer", Role: "user", Tier: "free", IsActive: true}
	admin := &model.User{Username: "admin", Role: "admin", Tier: "plus", IsActive: true}
	if err := svc.repo.User.Create(t.Context(), viewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if err := svc.repo.User.Create(t.Context(), admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	viewerPayload, err := svc.FindUser(t.Context(), viewer.ID)
	if err != nil {
		t.Fatalf("viewer payload: %v", err)
	}
	adminPayload, err := svc.FindUser(t.Context(), admin.ID)
	if err != nil {
		t.Fatalf("admin payload: %v", err)
	}
	viewerPolicy := viewerPayload["Policy"].(map[string]any)
	adminPolicy := adminPayload["Policy"].(map[string]any)
	if viewerPolicy["EnableMediaPlayback"] != true {
		t.Fatalf("viewer must keep playback enabled: %#v", viewerPolicy)
	}
	if viewerPolicy["EnableContentDownloading"] != false ||
		viewerPolicy["EnableSyncTranscoding"] != false ||
		viewerPolicy["EnableMediaConversion"] != false {
		t.Fatalf("viewer must not be allowed to download/sync media: %#v", viewerPolicy)
	}
	if adminPolicy["EnableContentDownloading"] != true {
		t.Fatalf("admin should keep downloading capability: %#v", adminPolicy)
	}
}

func TestEmbyHidesAdultLibrariesForUserLock(t *testing.T) {
	svc := newTestEmbyService(t)
	viewer := &model.User{Username: "viewer", Role: "user", Tier: "free", IsActive: true, HideAdult: true}
	if err := svc.repo.User.Create(t.Context(), viewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	safe := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	adult := model.Library{Name: "9KG 成人", Path: `/media/9KG`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &safe); err != nil {
		t.Fatalf("create safe library: %v", err)
	}
	if err := svc.repo.Library.Create(t.Context(), &adult); err != nil {
		t.Fatalf("create adult library: %v", err)
	}
	if err := svc.repo.Setting.Set(t.Context(), AdultLibraryIDsSettingKey, `["`+adult.ID+`"]`); err != nil {
		t.Fatalf("set adult libraries: %v", err)
	}
	if err := svc.repo.DB.Create(&model.Media{LibraryID: safe.ID, Title: "安全电影", Path: `/media/movies/a.mkv`}).Error; err != nil {
		t.Fatalf("create safe media: %v", err)
	}
	if err := svc.repo.DB.Create(&model.Media{LibraryID: adult.ID, Title: "成人电影", Path: `/media/9KG/a.mkv`}).Error; err != nil {
		t.Fatalf("create adult media: %v", err)
	}

	root, err := svc.Items(t.Context(), ItemsParams{UserID: viewer.ID, Limit: 50})
	if err != nil {
		t.Fatalf("root items: %v", err)
	}
	items := root["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Name"] != "电影" {
		t.Fatalf("adult library should be hidden: %#v", items)
	}
	adultItems, err := svc.Items(t.Context(), ItemsParams{UserID: viewer.ID, ParentID: adult.ID, Limit: 50})
	if err != nil {
		t.Fatalf("adult items: %v", err)
	}
	if got := adultItems["TotalRecordCount"]; got != int64(0) {
		t.Fatalf("adult media should be hidden, total=%#v payload=%#v", got, adultItems)
	}
}

func TestEmbyPlaybackInfoRespectsDirectPlayOnly(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{Base: model.Base{ID: "m-1"}, LibraryID: lib.ID, Title: "Inception", Path: `/media/movies/inception.mkv`}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	pb, err := svc.PlaybackInfo(t.Context(), "m-1", "user-1")
	if err != nil {
		t.Fatalf("playback info: %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["SupportsTranscoding"] != true {
		t.Fatalf("expected SupportsTranscoding=true by default, got %#v", src["SupportsTranscoding"])
	}
	if _, ok := src["TranscodingUrl"]; !ok {
		t.Fatalf("expected TranscodingUrl present by default: %#v", src)
	}
	if src["TranscodingUrl"] != "/Videos/m-1/master.m3u8" {
		t.Fatalf("expected HLS TranscodingUrl by default, got %#v", src["TranscodingUrl"])
	}

	if err := svc.repo.Setting.Set(t.Context(), PlaybackDirectOnlySettingKey, "true"); err != nil {
		t.Fatalf("enable direct-only: %v", err)
	}
	pb, err = svc.PlaybackInfo(t.Context(), "m-1", "user-1")
	if err != nil {
		t.Fatalf("playback info (direct-only): %v", err)
	}
	src = pb["MediaSources"].([]map[string]any)[0]
	if src["SupportsTranscoding"] != false {
		t.Fatalf("expected SupportsTranscoding=false in direct-only mode, got %#v", src["SupportsTranscoding"])
	}
	if _, ok := src["TranscodingUrl"]; ok {
		t.Fatalf("expected no TranscodingUrl in direct-only mode: %#v", src)
	}
	if src["SupportsDirectPlay"] != true || src["DirectStreamUrl"] != "/Videos/m-1/stream.mkv" {
		t.Fatalf("direct-only must still allow direct play: %#v", src)
	}
}

func TestEmbyPlaybackInfoUsesSharedMetadataForNameAndVisibility(t *testing.T) {
	svc := newTestEmbyService(t)
	viewer := &model.User{Username: "viewer", Role: "user", Tier: "free", IsActive: true, HideAdult: true}
	if err := svc.repo.User.Create(t.Context(), viewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	metadata := createServiceTestMetadata(t, svc.repo.DB, model.MetadataItem{
		Kind: model.MetadataKindMovie, Title: "共享标题", NSFW: true, Source: "tmdb",
	})
	media := model.Media{
		Base: model.Base{ID: "shared-playback"}, LibraryID: lib.ID, MetadataID: metadata.ID,
		Title: "扫描文件名", Path: `/media/movies/shared.mkv`,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	pb, err := svc.PlaybackInfo(t.Context(), media.ID, "")
	if err != nil {
		t.Fatalf("playback info: %v", err)
	}
	source := pb["MediaSources"].([]map[string]any)[0]
	if source["Name"] != metadata.Title {
		t.Fatalf("media source name = %#v, want shared title %q", source["Name"], metadata.Title)
	}
	hidden, err := svc.PlaybackInfo(t.Context(), media.ID, viewer.ID)
	if err != nil {
		t.Fatalf("hidden playback info: %v", err)
	}
	if hidden != nil {
		t.Fatalf("shared NSFW metadata must hide playback, got %#v", hidden)
	}
}

func TestEmbyPlaybackInfoKeepsSTRMBehindStreamEndpoint(t *testing.T) {
	svc := newTestEmbyService(t)
	if err := svc.repo.Setting.Set(t.Context(), CloudPlaybackModeSettingKey, CloudPlaybackModeSTRM); err != nil {
		t.Fatalf("set cloud playback mode: %v", err)
	}
	lib := model.Library{Name: "OpenList", Path: `cloud://openlist/Movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{
		Base:      model.Base{ID: "cloud-1"},
		LibraryID: lib.ID,
		Title:     "Cloud Movie",
		Path:      `cloud://openlist/Movies/f1.mkv`,
		STRMURL:   `/api/cloud/play/openlist?ref=%2FMovies%2Ff1.mkv`,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	pb, err := svc.PlaybackInfo(t.Context(), "cloud-1", "user-1")
	if err != nil {
		t.Fatalf("playback info: %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["IsRemote"] != true {
		t.Fatalf("strm media should be marked remote: %#v", src)
	}
	if src["DirectStreamUrl"] != "/api/stream/cloud-1" {
		t.Fatalf("strm playback should prefer /api/stream when enabled: %#v", src)
	}
	if src["Path"] != "/api/stream/cloud-1" {
		t.Fatalf("path should prefer /api/stream when enabled: %#v", src)
	}
	streams := src["MediaStreams"].([]map[string]any)
	if len(streams) == 0 || streams[0]["Type"] != "Video" {
		t.Fatalf("strm media should expose a fallback video stream for Android clients: %#v", src)
	}
}

func TestEmbyMediaSourceUsesLocalSTRMTargetContainer(t *testing.T) {
	svc := newTestEmbyService(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "LocalMovie.mkv")
	if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(dir, "LocalMovie.strm")
	if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	media := &model.Media{
		Base:  model.Base{ID: "local-path-strm"},
		Title: "Local STRM",
		Path:  strmPath,
	}

	src := svc.mediaSource(t.Context(), media, media.Title, false, false)
	if src["Container"] != "mkv" || src["IsRemote"] != false {
		t.Fatalf("local strm source should expose mkv as local media: %#v", src)
	}
	if src["DirectStreamUrl"] != "/Videos/local-path-strm/stream.mkv" {
		t.Fatalf("local strm stream URL should use target extension: %#v", src)
	}
	if src["Path"] != "/Videos/local-path-strm/stream.mkv" {
		t.Fatalf("local strm path should not expose the strm text file: %#v", src)
	}
}

func TestEmbyPlaybackInfoAsynchronouslyProbesLocalSTRMTarget(t *testing.T) {
	svc := newTestEmbyService(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "LocalMovie.mkv")
	if err := os.WriteFile(target, []byte("target-video"), 0o644); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(dir, "LocalMovie.strm")
	if err := os.WriteFile(strmPath, []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		Base: model.Base{ID: "local-playback-probe"}, Title: "Local STRM",
		Path: strmPath, Container: "strm",
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	prober := &fakeCloudPlaybackProber{probe: &ProbeResult{
		DurationSec: 3661, Width: 3840, Height: 2160, VideoCodec: "hevc", AudioCodec: "eac3", Container: "matroska,webm",
	}}
	svc.SetCloudProbe(nil, prober)

	if _, err := svc.PlaybackInfo(t.Context(), media.ID, "user-1"); err != nil {
		t.Fatalf("playback info: %v", err)
	}
	var persisted model.Media
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := svc.repo.DB.First(&persisted, "id = ?", media.ID).Error; err != nil {
			t.Fatal(err)
		}
		if persisted.DurationSec > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if prober.path != target || persisted.DurationSec != 3661 || persisted.SizeBytes != int64(len("target-video")) {
		t.Fatalf("local playback probe path/media = %q/%#v", prober.path, persisted)
	}
	pb, err := svc.PlaybackInfo(t.Context(), media.ID, "user-1")
	if err != nil {
		t.Fatalf("playback info after probe: %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["RunTimeTicks"] != int64(3661)*10_000_000 || src["Size"] != int64(len("target-video")) {
		t.Fatalf("playback info did not read persisted probe metadata: %#v", src)
	}
}

func TestEmbyTrackProbeReservationDeduplicatesMedia(t *testing.T) {
	svc := newTestEmbyService(t)
	if !svc.reserveTrackProbe("media-1") {
		t.Fatal("first track probe reservation should succeed")
	}
	if svc.reserveTrackProbe("media-1") {
		t.Fatal("duplicate track probe reservation should fail")
	}
	if svc.reserveTrackProbe("media-2") {
		t.Fatal("track probe reservations should respect the global ffprobe limit")
	}
	svc.releaseTrackProbe("media-1")
	if !svc.reserveTrackProbe("media-1") {
		t.Fatal("reservation should succeed after release")
	}
}

func TestEmbyLocalSTRMProbeDiscardsStaleTargetResult(t *testing.T) {
	svc := newTestEmbyService(t)
	dir := t.TempDir()
	targetA := filepath.Join(dir, "Movie-A.mkv")
	targetB := filepath.Join(dir, "Movie-B.mkv")
	if err := os.WriteFile(targetA, []byte("video-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetB, []byte("video-b"), 0o644); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		Base: model.Base{ID: "local-stale-probe"}, Path: filepath.Join(dir, "Movie.strm"),
		Container: "strm", STRMURL: targetA,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	prober := &fakeCloudPlaybackProber{
		probe: &ProbeResult{DurationSec: 100, Container: "matroska,webm"}, started: started, release: release,
	}
	svc.SetCloudProbe(nil, prober)

	if _, err := svc.PlaybackInfo(t.Context(), media.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := svc.repo.DB.Model(&model.Media{}).Where("id = ?", media.ID).Update("strm_url", targetB).Error; err != nil {
		t.Fatal(err)
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for {
		svc.trackProbeMu.Lock()
		pending := len(svc.trackProbeInFlight)
		svc.trackProbeMu.Unlock()
		if pending == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var persisted model.Media
	if err := svc.repo.DB.First(&persisted, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.DurationSec != 0 {
		t.Fatalf("stale target metadata was persisted: %#v", persisted)
	}
}

func TestEmbyPlaybackInfoUsesVideoStreamWhenSTRMDisabled(t *testing.T) {
	svc := newTestEmbyService(t)
	if err := svc.repo.Setting.Set(t.Context(), CloudPlaybackModeSettingKey, CloudPlaybackModeRedirectProxy); err != nil {
		t.Fatalf("set cloud playback mode: %v", err)
	}
	lib := model.Library{Name: "OpenList", Path: `cloud://openlist/Movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{
		Base:      model.Base{ID: "cloud-302"},
		LibraryID: lib.ID,
		Title:     "Cloud 302 Movie",
		Path:      `cloud://openlist/Movies/Movie.mkv`,
		STRMURL:   `/api/cloud/play/openlist?ref=%2FMovies%2FMovie.mkv`,
		Container: "mkv",
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	pb, err := svc.PlaybackInfo(t.Context(), "cloud-302", "user-1")
	if err != nil {
		t.Fatalf("playback info: %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["DirectStreamUrl"] != "/Videos/cloud-302/stream.mkv" {
		t.Fatalf("302/proxy mode should use Emby video stream URL: %#v", src)
	}
	if src["Path"] != "/Videos/cloud-302/stream.mkv" {
		t.Fatalf("302/proxy mode path should use Emby video stream URL: %#v", src)
	}
}

func TestEmbyPlaybackInfoProbesMissingCloudTrackMetadata(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "OpenList", Path: `cloud://openlist/Movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{
		Base:      model.Base{ID: "cloud-probe-1"},
		LibraryID: lib.ID,
		Title:     "云盘电影",
		Path:      `cloud://openlist/Movies/Movie.mkv`,
		STRMURL:   `http://nas.local/api/cloud/play/openlist?ref=%2FMovies%2FMovie.mkv`,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	resolver := &fakeCloudPlaybackResolver{
		link: &cloud.DirectLink{
			URL:     "http://cdn.example.test/Movie.mkv",
			Headers: map[string]string{"Authorization": "Bearer probe-token"},
		},
	}
	prober := &fakeCloudPlaybackProber{
		probe: &ProbeResult{
			DurationSec: 3661,
			Width:       3840,
			Height:      2160,
			VideoCodec:  "hevc",
			AudioCodec:  "eac3",
			Container:   "matroska,webm",
		},
	}
	svc.SetCloudProbe(resolver, prober)

	if _, err := svc.PlaybackInfo(t.Context(), "cloud-probe-1", "user-1"); err != nil {
		t.Fatalf("playback info: %v", err)
	}

	var persisted model.Media
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := svc.repo.DB.First(&persisted, "id = ?", "cloud-probe-1").Error; err != nil {
			t.Fatalf("reload media: %v", err)
		}
		if persisted.DurationSec > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if persisted.DurationSec != 3661 || persisted.Width != 3840 || persisted.Height != 2160 || persisted.VideoCodec != "hevc" || persisted.AudioCodec != "eac3" {
		t.Fatalf("probe metadata not persisted: %#v", persisted)
	}
	if resolver.typ != "openlist" || resolver.ref != "/Movies/Movie.mkv" {
		t.Fatalf("resolver called with typ=%q ref=%q", resolver.typ, resolver.ref)
	}
	if prober.rawURL != "http://cdn.example.test/Movie.mkv" || prober.headers["Authorization"] != "Bearer probe-token" {
		t.Fatalf("probe called with url=%q headers=%#v", prober.rawURL, prober.headers)
	}

	pb, err := svc.PlaybackInfo(t.Context(), "cloud-probe-1", "user-1")
	if err != nil {
		t.Fatalf("playback info (second): %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["RunTimeTicks"] != int64(3661)*10_000_000 {
		t.Fatalf("runtime ticks not filled after async probe: %#v", src)
	}
	streams := src["MediaStreams"].([]map[string]any)
	if len(streams) != 2 || streams[0]["Codec"] != "hevc" || streams[1]["Codec"] != "eac3" {
		t.Fatalf("media streams not filled after async probe: %#v", streams)
	}
}
