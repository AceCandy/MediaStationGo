package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type openListTestEntry struct {
	Name  string
	Size  int64
	IsDir bool
}

func newOpenListAPIServer(t *testing.T, list func(path string, page, perPage int) ([]openListTestEntry, int)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fs/list" {
			t.Fatalf("unexpected openlist api request %s", r.URL.Path)
		}
		var in struct {
			Path    string `json:"path"`
			Page    int    `json:"page"`
			PerPage int    `json:"per_page"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatalf("decode openlist list request: %v", err)
		}
		if in.Path == "" {
			in.Path = "/"
		}
		if in.Page <= 0 {
			in.Page = 1
		}
		if in.PerPage <= 0 {
			in.PerPage = 500
		}
		entries, total := list(in.Path, in.Page, in.PerPage)
		content := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			content = append(content, map[string]any{
				"name":   entry.Name,
				"size":   entry.Size,
				"is_dir": entry.IsDir,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"message": "success",
			"data": map[string]any{
				"content": content,
				"total":   total,
			},
		})
	}))
}

func TestScanCloudLibraryImportsRecursivePlayableMedia(t *testing.T) {
	empty := false
	upstream := newOpenListAPIServer(t, func(path string, page, perPage int) ([]openListTestEntry, int) {
		if empty {
			return nil, 0
		}
		switch path {
		case "/":
			return []openListTestEntry{
				{Name: "Movies", IsDir: true},
				{Name: "Root.Movie.2024.mkv", Size: 123},
			}, 2
		case "/Movies":
			return []openListTestEntry{
				{Name: "Nested.Show.S01E02.mp4", Size: 456},
			}, 1
		default:
			t.Fatalf("unexpected openlist path %q", path)
			return nil, 0
		}
	})
	defer upstream.Close()

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{})
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": upstream.URL,
			"token":  "openlist-token",
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud: %v", err)
	}
	if res.Visited != 2 || res.Added != 2 {
		t.Fatalf("scan result = %#v, want visited=2 added=2", res)
	}
	var rows []model.Media
	if err := repos.DB.Order("path").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("media rows = %d, want 2: %#v", len(rows), rows)
	}
	if rows[0].Path != "cloud://openlist/Movies/Nested.Show.S01E02.mp4" || !strings.Contains(rows[0].STRMURL, "ref=%2FMovies%2FNested.Show.S01E02.mp4") {
		t.Fatalf("nested media path/strm wrong: path=%q strm=%q", rows[0].Path, rows[0].STRMURL)
	}
	if rows[0].SeasonNum != 1 || rows[0].EpisodeNum != 2 {
		t.Fatalf("nested episode metadata wrong: %#v", rows[0])
	}
	if rows[1].Path != "cloud://openlist/Root.Movie.2024.mkv" || rows[1].STRMURL != "/api/cloud/play/openlist?ref=%2FRoot.Movie.2024.mkv" {
		t.Fatalf("root media path/strm wrong: path=%q strm=%q", rows[0].Path, rows[0].STRMURL)
	}

	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("rescan same cloud: %v", err)
	}
	if res.Added != 0 || res.Updated != 0 || res.Skipped != 2 {
		t.Fatalf("same cloud rescan should skip unchanged rows, got %#v", res)
	}

	empty = true
	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("rescan cloud: %v", err)
	}
	if res.Removed != 2 {
		t.Fatalf("removed = %d, want 2", res.Removed)
	}
	if got := countMedia(t, repos); got != 0 {
		t.Fatalf("media count after prune = %d, want 0", got)
	}
	var allRows int64
	if err := repos.DB.Unscoped().Model(&model.Media{}).Count(&allRows).Error; err != nil {
		t.Fatal(err)
	}
	if allRows != 0 {
		t.Fatalf("unscoped media count after cloud prune = %d, want 0", allRows)
	}
}

func TestCloudLibraryPathParsing(t *testing.T) {
	typ, dir, ok := parseCloudLibraryPath("cloud://cloud115/abc%20123?ignored=1")
	if !ok || typ != "cloud115" || dir != "abc 123" {
		t.Fatalf("parse path got typ=%q dir=%q ok=%v", typ, dir, ok)
	}
	typ, dir, ok = parseCloudLibraryPath("cloud://openlist/Movies?dir=%2FMovies")
	if !ok || typ != "openlist" || dir != "Movies" {
		t.Fatalf("parse query got typ=%q dir=%q ok=%v", typ, dir, ok)
	}
	if ref := cloudEntryRef("cloud115", "fid", "pick"); ref != "pick" {
		t.Fatalf("115 ref = %q, want pick", ref)
	}
}

func TestCloudMountConflictDetectsNestedMounts(t *testing.T) {
	root := model.Library{Base: model.Base{ID: "root"}, Name: "115", Path: "cloud://cloud115", Enabled: true}
	childPath := BuildCloudLibraryPath("cloud115", "child-id", "parent-id/child-id")
	info, ok := ParseCloudLibraryMount(childPath)
	if !ok || info.ScanDir != "child-id" || info.DisplayDir != "parent-id/child-id" {
		t.Fatalf("parse child mount = %#v ok=%v", info, ok)
	}

	conflict := FindCloudMountConflict([]model.Library{root}, "cloud115", "child-id", "parent-id/child-id")
	if conflict != nil {
		t.Fatalf("child mount under existing root should be allowed, got conflict %#v", conflict)
	}

	sibling := model.Library{Base: model.Base{ID: "sibling"}, Name: "Sibling", Path: BuildCloudLibraryPath("cloud115", "sibling-id", "parent-id/sibling-id"), Enabled: true}
	conflict = FindCloudMountConflict([]model.Library{sibling}, "cloud115", "child-id", "parent-id/child-id")
	if conflict != nil {
		t.Fatalf("sibling conflict = %#v, want nil", conflict)
	}

	conflict = FindCloudMountConflict([]model.Library{sibling}, "cloud115", "parent-id", "parent-id")
	if conflict == nil || !conflict.Nested {
		t.Fatalf("parent mount over existing child = %#v, want nested conflict", conflict)
	}
	oldIDPath := BuildCloudLibraryPath("cloud115", "child-id", "old-parent-id/child-id")
	conflict = FindCloudMountConflict([]model.Library{{Base: model.Base{ID: "old"}, Name: "Old", Path: oldIDPath, Enabled: true}}, "cloud115", "child-id", "父目录/子目录")
	if conflict == nil || !conflict.Exact {
		t.Fatalf("same scan dir with renamed display path = %#v, want exact conflict", conflict)
	}

	root.CreatedAt = root.CreatedAt.Add(-1)
	child := model.Library{Base: model.Base{ID: "child"}, Name: "Child", Path: childPath, Enabled: true}
	if shadow := CloudLibraryShadowed([]model.Library{root, child}, child); shadow != nil {
		t.Fatalf("child should not be shadowed by root: %#v", shadow)
	}
	if shadow := CloudLibraryShadowed([]model.Library{root, child}, root); shadow == nil || !shadow.Nested {
		t.Fatalf("root should be shadowed by child, got %#v", shadow)
	}
}

func TestCancelCloudScansForProviderSignalsRunningScan(t *testing.T) {
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repository.New(nil), NewHub(zap.NewNop()), nil, nil)
	cancelled := false
	scanner.cloudScans["lib-1"] = &cloudScanEntry{
		status: CloudScanStatus{LibraryID: "lib-1", Provider: "openlist", State: "running"},
		cancel: func() {
			cancelled = true
		},
	}

	if got := scanner.CancelCloudScansForProvider("openlist"); got != 1 {
		t.Fatalf("cancelled = %d, want 1", got)
	}
	if !cancelled {
		t.Fatal("cancel func was not called")
	}
	if state := scanner.cloudScans["lib-1"].status.State; state != "canceling" {
		t.Fatalf("state = %q, want canceling", state)
	}
}

func TestInferCloudMountMediaType(t *testing.T) {
	cases := map[string]string{
		"/日漫":      "anime",
		"/国漫":      "anime",
		"/欧美动漫":    "anime",
		"/电视剧/国产剧": "tv",
		"/电视剧/欧美剧": "tv",
		"/电视剧/日韩剧": "tv",
		"/电影/动画电影": "movie",
		"/电影/华语电影": "movie",
		"/电影/外语电影": "movie",
		"/综艺":      "variety",
	}
	for dir, want := range cases {
		if got := InferCloudMountMediaType(dir, "OpenList · "+dir); got != want {
			t.Fatalf("%s type = %s, want %s", dir, got, want)
		}
	}
}

func TestScan115CloudLibraryKeepsDisplayHierarchyAndSeasonCounts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("cid") {
		case "100":
			_, _ = w.Write([]byte(`{"state":true,"data":[
				{"cid":"s1","n":"Season 1","s":0},
				{"cid":"s2","n":"Season 2","s":0}
			]}`))
		case "s1":
			_, _ = w.Write([]byte(`{"state":true,"data":[
				{"fid":"f101","n":"剑来 - S01E01.mkv","s":1001,"pc":"pick101"},
				{"fid":"f125","n":"剑来 - S01E25.mkv","s":1025,"pc":"pick125"}
			]}`))
		case "s2":
			_, _ = w.Write([]byte(`{"state":true,"data":[
				{"fid":"f201","n":"剑来 - S02E01.mkv","s":2001,"pc":"pick201"},
				{"fid":"f204","n":"剑来 - S02E04.mkv","s":2004,"pc":"pick204"}
			]}`))
		default:
			t.Fatalf("unexpected cid %q", r.URL.Query().Get("cid"))
		}
	}))
	defer upstream.Close()

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{})
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "cloud115",
		Config: map[string]any{
			"cookie": "UID=test",
			"base":   upstream.URL,
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "115 · 国漫 · 剑来", Path: BuildCloudLibraryPath("cloud115", "100", "动漫/国漫/剑来"), Type: "anime", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud115: %v", err)
	}
	if res.Added != 4 {
		t.Fatalf("scan result = %#v, want added=4", res)
	}
	var rows []model.Media
	if err := repos.DB.Order("path").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("media rows = %d, want 4", len(rows))
	}
	want := map[string][2]int{
		"cloud://cloud115/动漫/国漫/剑来/Season 1/剑来 - S01E01.mkv": {1, 1},
		"cloud://cloud115/动漫/国漫/剑来/Season 1/剑来 - S01E25.mkv": {1, 25},
		"cloud://cloud115/动漫/国漫/剑来/Season 2/剑来 - S02E01.mkv": {2, 1},
		"cloud://cloud115/动漫/国漫/剑来/Season 2/剑来 - S02E04.mkv": {2, 4},
	}
	for _, row := range rows {
		seasonEpisode, ok := want[row.Path]
		if !ok {
			t.Fatalf("unexpected path %q", row.Path)
		}
		if row.Title != "剑来" {
			t.Fatalf("title = %q, want 剑来", row.Title)
		}
		if row.SeasonNum != seasonEpisode[0] || row.EpisodeNum != seasonEpisode[1] {
			t.Fatalf("%s season/episode = %d/%d, want %d/%d", row.Path, row.SeasonNum, row.EpisodeNum, seasonEpisode[0], seasonEpisode[1])
		}
		if !strings.Contains(row.STRMURL, "ref=pick") {
			t.Fatalf("115 playback should keep pickcode ref, got %q", row.STRMURL)
		}
	}
}

func TestCloudSeriesTitlePrefersShowFolder(t *testing.T) {
	title, year := cloudSeriesTitleFromMediaPath("cloud://openlist/国产剧/紫川 (2024) {tmdb-247590}/Season 2/紫川.2024.S02E24.第24集.2160p.WEB-DL.H.265-ColorTV.mkv")
	if title != "紫川" || year != 2024 {
		t.Fatalf("cloud series title = %q/%d, want 紫川/2024", title, year)
	}
	title, year = cloudSeriesTitleFromMediaPath("cloud://openlist/国产剧/紫川.2024.S02E24.mkv")
	if title != "" || year != 0 {
		t.Fatalf("single category folder should not override title, got %q/%d", title, year)
	}
}

func TestCloudMetadataNeedsRefreshWhenPathHintConflicts(t *testing.T) {
	existing := existingCloudMedia{
		Year:   2025,
		TMDbID: 220269,
	}
	local := &LocalMetadata{
		Year:     2025,
		TMDbID:   296753,
		PathHint: true,
	}
	if !cloudMetadataNeedsRefresh(existing, local) {
		t.Fatal("conflicting explicit cloud path hint should refresh existing media")
	}
}

func TestScanOpenListCloudLibraryUsesAPIPaginationBeyondFirstPage(t *testing.T) {
	const totalFiles = 125
	requestedPages := map[int]bool{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fs/list" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.Header.Get("Authorization") != "openlist-token" {
			t.Fatalf("missing openlist token: %q", r.Header.Get("Authorization"))
		}
		var in struct {
			Path    string `json:"path"`
			Page    int    `json:"page"`
			PerPage int    `json:"per_page"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if in.Path != "/Movies" {
			t.Fatalf("path = %q, want /Movies", in.Path)
		}
		if in.PerPage <= 100 {
			t.Fatalf("per_page = %d, want API pagination larger than legacy 100", in.PerPage)
		}
		requestedPages[in.Page] = true
		effectivePageSize := in.PerPage
		if effectivePageSize > 100 {
			effectivePageSize = 100
		}
		start := (in.Page - 1) * effectivePageSize
		content := []map[string]any{}
		for idx := start; idx < totalFiles && idx < start+effectivePageSize; idx++ {
			content = append(content, map[string]any{
				"name":   fmt.Sprintf("Movie.%03d.mkv", idx+1),
				"size":   int64(1024 + idx),
				"is_dir": false,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"message": "success",
			"data": map[string]any{
				"content": content,
				"total":   totalFiles,
			},
		})
	}))
	defer upstream.Close()

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{})
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": upstream.URL,
			"token":  "openlist-token",
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList · Movies", Path: BuildCloudLibraryPath("openlist", "/Movies", "/Movies"), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan openlist: %v", err)
	}
	if res.Added != totalFiles {
		t.Fatalf("scan result = %#v, want added=%d", res, totalFiles)
	}
	if got := countMedia(t, repos); got != totalFiles {
		t.Fatalf("media count = %d, want %d", got, totalFiles)
	}
	if !requestedPages[1] || !requestedPages[2] {
		t.Fatalf("expected pagination beyond the first 100 entries, got pages %#v", requestedPages)
	}
}

func TestScanCloudLibraryQueuesMissingExistingTrackMetadataBeforeNewFiles(t *testing.T) {
	const newFiles = maxCloudMediaProbeQueuePerScan + 5
	upstream := newOpenListAPIServer(t, func(path string, page, perPage int) ([]openListTestEntry, int) {
		if path != "/" {
			t.Fatalf("unexpected openlist path %q", path)
		}
		entries := make([]openListTestEntry, 0, newFiles+1)
		for i := 0; i < newFiles; i++ {
			entries = append(entries, openListTestEntry{Name: fmt.Sprintf("New.Movie.%02d.mkv", i), Size: int64(1000 + i)})
		}
		entries = append(entries, openListTestEntry{Name: "Existing.Show.S01E01.mkv", Size: 2048})
		return entries, len(entries)
	})
	defer upstream.Close()

	db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{})
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": upstream.URL,
			"token":  "openlist-token",
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	existingPath := "cloud://openlist/Existing.Show.S01E01.mkv"
	if err := repos.DB.Create(&model.Media{
		LibraryID:  lib.ID,
		Title:      "Existing Show",
		Path:       existingPath,
		SizeBytes:  2048,
		Container:  "mkv",
		STRMURL:    "/api/cloud/play/openlist?ref=%2FExisting.Show.S01E01.mkv",
		SeasonNum:  1,
		EpisodeNum: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), NewFFprobeService(&config.Config{}, log), nil)
	scanner.storage = storage

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud: %v", err)
	}
	if res.Added != newFiles || res.Skipped != 1 {
		t.Fatalf("scan result = %#v, want new files added and existing skipped", res)
	}
	foundExistingProbe := false
	for {
		select {
		case task := <-scanner.cloudMediaProbeQueue:
			if task.path == existingPath {
				foundExistingProbe = true
			}
		default:
			if !foundExistingProbe {
				t.Fatal("existing media missing track metadata did not receive probe budget before new files")
			}
			return
		}
	}
}

func TestParseCloudArtworkURL(t *testing.T) {
	typ, ref, ok := ParseCloudArtworkURL("http://nas.local/api/cloud/play/openlist?ref=%2FAnime%2FJianLai%2Fposter.jpg")
	if !ok || typ != "openlist" || ref != "/Anime/JianLai/poster.jpg" {
		t.Fatalf("parse cloud image url = typ=%q ref=%q ok=%v", typ, ref, ok)
	}
	typ, ref, ok = ParseCloudArtworkURL("/api/img/cloud/openlist?ref=%2FAnime%2FJianLai%2Fposter.jpg")
	if !ok || typ != "openlist" || ref != "/Anime/JianLai/poster.jpg" {
		t.Fatalf("parse cached cloud artwork url = typ=%q ref=%q ok=%v", typ, ref, ok)
	}
	typ, ref, ok = ParseCloudArtworkURL("/api/img/cloud/openlist?ref=%2FMovies%2FMovie.tbn")
	if !ok || typ != "openlist" || ref != "/Movies/Movie.tbn" {
		t.Fatalf("parse tbn cloud artwork url = typ=%q ref=%q ok=%v", typ, ref, ok)
	}
	if _, _, ok := ParseCloudArtworkURL("/api/cloud/play/openlist?ref=%2FAnime%2FJianLai%2Fmovie.mkv"); ok {
		t.Fatal("video cloud url should not be treated as artwork")
	}
	if _, _, ok := ParseCloudArtworkURL("https://image.tmdb.org/t/p/w500/poster.jpg"); ok {
		t.Fatal("remote HTTP poster should not be treated as cloud artwork")
	}
}
