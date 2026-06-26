package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestDownloadViewsDoNotExposePrivateURL(t *testing.T) {
	rows := []model.DownloadTask{{
		UserID:   "u1",
		Source:   "qbittorrent",
		URL:      "https://tracker.example/download?id=1&passkey=private-token",
		Title:    "测试影片",
		SavePath: "/downloads",
		Status:   "queued",
	}}

	tasks, torrents := DownloadViews(rows, nil)
	data, err := json.Marshal(map[string]any{
		"tasks":    tasks,
		"torrents": torrents,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if strings.Contains(body, "private-token") || strings.Contains(body, "passkey") || strings.Contains(body, "tracker.example") {
		t.Fatalf("download views leaked private URL: %s", body)
	}
	if !strings.Contains(body, "测试影片") {
		t.Fatalf("download views should keep public title: %s", body)
	}
}

func TestLiveTorrentSnapshotUsesPollingSnapshot(t *testing.T) {
	now := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	svc := NewDownloadService(zap.NewNop(), nil, NewHub(zap.NewNop()), nil)
	svc.now = func() time.Time { return now }

	live := []QBitTorrent{{
		Hash:     "hash-1",
		Name:     "Release.Name.S01E01",
		State:    "downloading",
		Progress: 0.5,
	}}
	svc.processDownloadSnapshot(t.Context(), live, nil)
	live[0].Name = "mutated"

	got := svc.LiveTorrentSnapshot(30 * time.Second)
	if len(got) != 1 || got[0].Name != "Release.Name.S01E01" {
		t.Fatalf("snapshot = %#v, want cloned live torrent", got)
	}
	got[0].Name = "changed"
	again := svc.LiveTorrentSnapshot(30 * time.Second)
	if len(again) != 1 || again[0].Name != "Release.Name.S01E01" {
		t.Fatalf("snapshot was mutable through caller: %#v", again)
	}

	now = now.Add(31 * time.Second)
	if stale := svc.LiveTorrentSnapshot(30 * time.Second); len(stale) != 0 {
		t.Fatalf("stale snapshot = %#v, want empty", stale)
	}
}

func TestDownloadCompleteNotificationPayloadUsesTaskMetadata(t *testing.T) {
	body, data := downloadCompleteNotificationPayload(QBitTorrent{
		Hash:        "done123",
		Name:        "Release.Name.S01E02.1080p",
		SavePath:    "/downloads/show",
		ContentPath: "/downloads/show/Release.Name.S01E02.1080p.mkv",
	}, &model.DownloadTask{
		Title:            "正式标题",
		PosterURL:        "https://img.example/poster.jpg",
		BackdropURL:      "https://img.example/backdrop.jpg",
		MediaType:        "tv",
		MediaCategory:    "日番",
		Overview:         "简介",
		OriginalName:     "Original Title",
		OriginalLanguage: "ja",
		Year:             2026,
		Rating:           8.7,
		Genres:           "动画,剧情",
	})

	if !strings.Contains(body, "任务：正式标题") {
		t.Fatalf("body should prefer task title, got %q", body)
	}
	if !strings.Contains(body, "保存路径：/downloads/show/Release.Name.S01E02.1080p.mkv") {
		t.Fatalf("body should include content path, got %q", body)
	}
	for key, want := range map[string]interface{}{
		"resource_title":    "Release.Name.S01E02.1080p",
		"title":             "正式标题",
		"poster_url":        "https://img.example/poster.jpg",
		"backdrop_url":      "https://img.example/backdrop.jpg",
		"media_type":        "tv",
		"media_category":    "日番",
		"overview":          "简介",
		"original_title":    "Original Title",
		"original_language": "ja",
		"year":              2026,
		"rating":            float32(8.7),
		"genres":            "动画,剧情",
	} {
		if got := data[key]; got != want {
			t.Fatalf("data[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestSyncDownloadTaskProgressSkipsUnchangedCompletedTask(t *testing.T) {
	db := newServiceTestDB(t, &model.DownloadTask{})
	repos := repository.New(db)
	task := &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:test",
		Title:    "Already.Done.S01E01",
		SavePath: "/downloads",
		Status:   "completed",
		Progress: 1,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	var before model.DownloadTask
	if err := db.First(&before, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc.syncDownloadTaskProgress(t.Context(), QBitTorrent{
		Name:     task.Title,
		Progress: 1,
		State:    "completed",
	}, tasksByIdentity([]model.DownloadTask{before}))
	var after model.DownloadTask
	if err := db.First(&after, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("unchanged completed torrent touched updated_at: before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestSyncDownloadTaskProgressMatchesSeasonFolderTorrentName(t *testing.T) {
	db := newServiceTestDB(t, &model.DownloadTask{})
	repos := repository.New(db)
	task := &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:test",
		Title:    "The First Jasmine S01E01 1080p TX WEB-DL AAC2.0 H.264-MWeb",
		SavePath: "/downloads/未分类",
		Status:   "queued",
		Progress: 0.5,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc.syncDownloadTaskProgress(t.Context(), QBitTorrent{
		Name:     "The.First.Jasmine.S01.1080p.TX.WEB-DL.AAC2.0.H.264-MWeb",
		Progress: 1,
		State:    "stalledUP",
	}, tasksByTorrentIdentity([]model.DownloadTask{*task}))

	var after model.DownloadTask
	if err := db.First(&after, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != "completed" || after.Progress != 1 {
		t.Fatalf("task completion = %s/%v, want completed/1", after.Status, after.Progress)
	}
}

func TestProcessDownloadSnapshotQueuesCompletedPendingTaskOnFirstSnapshot(t *testing.T) {
	db := newServiceTestDB(t, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	task := &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:test",
		Title:    "Blades of the Guardians S02E01 1080p TX WEB-DL AAC2.0 H.264-MWeb",
		SavePath: "/downloads/未分类",
		Status:   "queued",
		Progress: 0,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organizer.auto_after_download", "true"); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:     "quickdone",
		Name:     "Blades.of.the.Guardians.S02.1080p.TX.WEB-DL.AAC2.0.H.264-MWeb",
		Progress: 1,
		State:    "stalledUP",
	}}, tasksByTorrentIdentity([]model.DownloadTask{*task}))

	if got := len(svc.organizeQueue); got != 1 {
		t.Fatalf("queued completed organize jobs = %d, want 1", got)
	}
}

func TestProcessDownloadSnapshotDoesNotQueueActiveDownloadAtFullProgress(t *testing.T) {
	db := newServiceTestDB(t, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	task := &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:test",
		Title:    "Still Downloading S01E01",
		SavePath: "/downloads/未分类",
		Status:   "downloading",
		Progress: 0.99,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organizer.auto_after_download", "true"); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:     "notdone",
		Name:     "Still.Downloading.S01E01",
		Progress: 1,
		State:    "downloading",
	}}, tasksByTorrentIdentity([]model.DownloadTask{*task}))

	if got := len(svc.organizeQueue); got != 0 {
		t.Fatalf("queued active download organize jobs = %d, want 0", got)
	}
	var after model.DownloadTask
	if err := db.First(&after, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status == "completed" {
		t.Fatalf("active download status = %q, should not be completed", after.Status)
	}
}

func TestProcessDownloadSnapshotDoesNotQueueFullProgressWithoutQBitState(t *testing.T) {
	db := newServiceTestDB(t, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	task := &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:test",
		Title:    "Missing State S01E01",
		SavePath: "/downloads/未分类",
		Status:   "downloading",
		Progress: 0.99,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organizer.auto_after_download", "true"); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:     "missing-state",
		Name:     "Missing.State.S01E01",
		Progress: 1,
	}}, tasksByTorrentIdentity([]model.DownloadTask{*task}))

	if got := len(svc.organizeQueue); got != 0 {
		t.Fatalf("queued full-progress torrent without state = %d, want 0", got)
	}
	var after model.DownloadTask
	if err := db.First(&after, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status == "completed" {
		t.Fatalf("missing-state torrent status = %q, should not be completed", after.Status)
	}
}

func TestProcessDownloadSnapshotDoesNotTrustCompletionOnForActiveDownload(t *testing.T) {
	db := newServiceTestDB(t, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	task := &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:test",
		Title:    "Still Downloading With Completion Timestamp S01E01",
		SavePath: "/downloads/未分类",
		Status:   "downloading",
		Progress: 0.5,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organizer.auto_after_download", "true"); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:         "notdone-completion-on",
		Name:         "Still.Downloading.With.Completion.Timestamp.S01E01",
		Progress:     0.5,
		State:        "downloading",
		CompletionOn: time.Now().Unix(),
	}}, tasksByTorrentIdentity([]model.DownloadTask{*task}))

	if got := len(svc.organizeQueue); got != 0 {
		t.Fatalf("queued active download organize jobs = %d, want 0", got)
	}
	var after model.DownloadTask
	if err := db.First(&after, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status == "completed" || after.Progress >= 1 {
		t.Fatalf("active download mutated to completed state: status=%q progress=%v", after.Status, after.Progress)
	}
}

func TestProcessDownloadSnapshotSkipsUntrackedCompletedTorrentOnFirstSnapshot(t *testing.T) {
	db := newServiceTestDB(t, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)

	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:         "historydone",
		Name:         "Large.History.Pack.2026.1080p",
		Progress:     1,
		State:        "stalledUP",
		CompletionOn: time.Now().Unix(),
	}}, tasksByTorrentIdentity(nil))

	if got := len(svc.organizeQueue); got != 0 {
		t.Fatalf("queued untracked historical torrents = %d, want 0", got)
	}
}

func TestCompletedTorrentSourceDoesNotFallbackToSavePath(t *testing.T) {
	root := t.TempDir()
	savePath := filepath.Join(root, "downloads", "日番")
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := NewDownloadService(zap.NewNop(), newOrganizerTestRepo(t), NewHub(zap.NewNop()), nil)

	got := svc.completedTorrentSource(t.Context(), QBitTorrent{
		Hash:        "done123",
		Name:        "Missing.Payload.S01",
		SavePath:    savePath,
		ContentPath: filepath.Join(savePath, "Missing.Payload.S01", "Missing.Payload.S01E01.mkv"),
	})

	if got != "" {
		t.Fatalf("completedTorrentSource fell back to whole save_path %q; want empty", got)
	}
}

func TestDownloadPollBaselinesAlreadyCompletedTorrents(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)

	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:     "already-complete",
		Name:     "Already Complete S01E01",
		Progress: 1,
		State:    "stalledUP",
	}}, nil)

	if got := len(svc.organizeQueue); got != 0 {
		t.Fatalf("first poll queued %d organize jobs, want 0", got)
	}
	if !svc.prevStates["already-complete"] {
		t.Fatal("first poll should remember completed baseline state")
	}

	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:     "late-complete",
		Name:     "Late Complete S01E01",
		Progress: 1,
		State:    "stalledUP",
	}}, nil)
	if got := len(svc.organizeQueue); got != 0 {
		t.Fatalf("newly discovered completed torrent queued %d organize jobs, want 0", got)
	}

	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:     "new-download",
		Name:     "New Download S01E01",
		Progress: 0.5,
	}}, nil)
	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{{
		Hash:     "new-download",
		Name:     "New Download S01E01",
		Progress: 1,
		State:    "stalledUP",
	}}, nil)

	if got := len(svc.organizeQueue); got != 1 {
		t.Fatalf("completion transition queued %d organize jobs, want 1", got)
	}
}

func TestDownloadPollCatchesUpRecentlyCompletedTorrents(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	if err := repos.DB.AutoMigrate(&model.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	task := &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:fresh",
		Title:    "Fresh Complete S01E01",
		SavePath: "/downloads",
		Status:   "queued",
		Progress: 0,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organizer.auto_after_download", "true"); err != nil {
		t.Fatal(err)
	}
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)

	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{
		{Hash: "fresh-complete", Name: "Fresh Complete S01E01", Progress: 1, State: "stalledUP", CompletionOn: time.Now().Add(-time.Hour).Unix()},
		{Hash: "stale-complete", Name: "Stale Complete S01E01", Progress: 1, State: "stalledUP", CompletionOn: time.Now().Add(-48 * time.Hour).Unix()},
		{Hash: "no-timestamp", Name: "No Timestamp S01E01", Progress: 1, State: "stalledUP"},
	}, tasksByTorrentIdentity([]model.DownloadTask{*task}))

	// 只有补整理时间窗内、且存在本地追踪任务的种子会被补整理；无 completion_on 的保守跳过。
	if got := len(svc.organizeQueue); got != 1 {
		t.Fatalf("first poll queued %d organize jobs, want 1 (recent tracked completion only)", got)
	}
}

func TestDownloadPollDoesNotCatchUpWhenAutoOrganizeDisabled(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	if err := repos.DB.AutoMigrate(&model.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	task := &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:fresh",
		Title:    "Fresh Complete S01E01",
		SavePath: "/downloads",
		Status:   "queued",
		Progress: 0,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	torrent := QBitTorrent{
		Hash:         "fresh-complete",
		Name:         "Fresh Complete S01E01",
		Progress:     1,
		State:        "stalledUP",
		CompletionOn: time.Now().Add(-time.Hour).Unix(),
	}

	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{torrent}, tasksByTorrentIdentity([]model.DownloadTask{*task}))
	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{torrent}, tasksByTorrentIdentity([]model.DownloadTask{*task}))

	if got := len(svc.organizeQueue); got != 0 {
		t.Fatalf("auto-organize disabled queued %d completed jobs, want 0", got)
	}
}

func TestDownloadPollSkipsRecordedCompletedTorrentCatchup(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	torrent := QBitTorrent{
		Hash:         "fresh-complete",
		Name:         "Fresh Complete S01E01",
		Progress:     1,
		State:        "stalledUP",
		CompletionOn: time.Now().Add(-time.Hour).Unix(),
	}
	if err := repos.Setting.Set(t.Context(), completedTorrentCatchupSettingKey(torrent), "true"); err != nil {
		t.Fatal(err)
	}
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)

	svc.processDownloadSnapshot(t.Context(), []QBitTorrent{torrent}, nil)

	if got := len(svc.organizeQueue); got != 0 {
		t.Fatalf("recorded completed torrent queued %d organize jobs, want 0", got)
	}
}

func TestDownloadCompleteRecordsUnsupportedVideoAsHandled(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads", "Toy.Story.4.2019.iso")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, src, "iso")

	repos := newOrganizerTestRepo(t)
	if err := repos.DB.AutoMigrate(&model.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"organizer.auto_after_download": "true",
		"organize.target_dir":           dest,
		"organize.transfer_mode":        "copy",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	torrent := QBitTorrent{
		Hash:         "unsupported-iso",
		Name:         "Toy.Story.4.2019",
		Progress:     1,
		SavePath:     filepath.Dir(src),
		ContentPath:  src,
		CompletionOn: time.Now().Add(-time.Hour).Unix(),
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), org)
	svc.onTorrentComplete(t.Context(), torrent)

	if !svc.completedTorrentCatchupRecorded(t.Context(), torrent) {
		t.Fatalf("unsupported completed torrent should be marked handled to avoid repeated auto-organize retries")
	}
}

func TestAutoOrganizeSyncsVisibilityWhenTargetAlreadyExists(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads", "国产剧", "狂飙.S01E01.2023.1080p.mkv")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, src, "episode")

	repos := newOrganizerTestRepo(t)
	for key, value := range map[string]string{
		"organizer.auto_after_download": "true",
		"organize.target_dir":           dest,
		"organize.transfer_mode":        "copy",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	lib := model.Library{Name: "国产剧", Path: filepath.Join(dest, "电视剧", "国产剧"), Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	if _, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	}); err != nil {
		t.Fatalf("seed organized destination: %v", err)
	}
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), org)
	svc.SetScanner(scanner)

	svc.onTorrentComplete(t.Context(), QBitTorrent{
		Hash:        "done123",
		Name:        "狂飙.S01E01.2023.1080p",
		Progress:    1,
		SavePath:    filepath.Dir(src),
		ContentPath: src,
	})

	var count int64
	if err := repos.DB.Model(&model.Media{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("target already exists should still be scanned into DB, count=%d want 1", count)
	}
}

func TestCompletedTorrentSourceUsesConfiguredMapping(t *testing.T) {
	root := t.TempDir()
	localRoot := filepath.Join(root, "localdl")
	payload := filepath.Join(localRoot, "Show.S01")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	repos := newOrganizerTestRepo(t)
	if err := repos.Setting.Set(t.Context(), DownloadPathMappingsSettingKey, "/qb/downloads="+localRoot); err != nil {
		t.Fatal(err)
	}
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)

	got := svc.completedTorrentSource(t.Context(), QBitTorrent{ContentPath: "/qb/downloads/Show.S01"})
	if got != payload {
		t.Fatalf("completedTorrentSource = %q, want %q", got, payload)
	}
}

func TestUserPathMappingsParsing(t *testing.T) {
	repos := newOrganizerTestRepo(t)
	raw := "# comment\n/a=/b\n/c => /d\n/e:/f\nbad-line\n"
	if err := repos.Setting.Set(t.Context(), DownloadPathMappingsSettingKey, raw); err != nil {
		t.Fatal(err)
	}
	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	got := svc.userPathMappings(t.Context())
	want := map[string]string{"/a": "/b", "/c": "/d", "/e": "/f"}
	if len(got) != len(want) {
		t.Fatalf("userPathMappings = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("mapping %q = %q, want %q", k, got[k], v)
		}
	}
}
