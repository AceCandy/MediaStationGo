package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func newEpisodeProbeTestEnv(t *testing.T, source string) (*EmbyService, *model.MediaView) {
	t.Helper()
	e := newTestEmbyService(t)
	db := e.repo.DB
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	// 独立目录夹具不能经过普通元数据自动补齐回调。
	if err := db.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"visible", "hidden"} {
		if err := db.Create(&model.Library{Base: model.Base{ID: id}, Name: id, Type: "tv", Path: t.TempDir()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var statements []string
	switch source {
	case model.TaskSystemHongGuo:
		statements = []string{
			`INSERT INTO hongguo_works(id,source_id,kind,title,related_album_id,season_index,refreshed_at) VALUES ('season1','1001','series','First','9001',1,now()),('season2','1002','series','Second','9001',2,now())`,
			`INSERT INTO hongguo_episodes(id,work_id,number) VALUES ('ep1','season1',1),('ep2','season1',2),('ep3','season1',3),('other2','season2',2)`,
		}
	case model.CatalogSourceNFO:
		statements = []string{`INSERT INTO nfo_items(id,library_id,local_key,kind,title,parent_id,season_num,episode_num) VALUES
('series','visible','series','series','Series',NULL,0,0),('season1','visible','season1','season','First','series',1,0),('season2','visible','season2','season','Second','series',2,0),
('ep1','visible','ep1','episode','First','season1',0,1),('ep2','visible','ep2','episode','Next','season1',0,2),('ep3','visible','ep3','episode','Later','season1',0,3),('other2','visible','other2','episode','Other','season2',0,2)`}
	default:
		statements = []string{`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num) VALUES
('series','series','Series','local',NULL,0,0),('season1','season','First','local','series',1,0),('season2','season','Second','local','series',2,0),
('ep1','episode','First','local','season1',0,1),('ep2','episode','Next','local','season1',0,2),('ep3','episode','Later','local','season1',0,3),('other2','episode','Other','local','season2',0,2)`}
	}
	for _, sql := range statements {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ id, item, library string }{
		{"current", "ep1", "visible"}, {"next-a", "ep2", "visible"}, {"next-b", "ep2", "visible"},
		{"filled", "ep2", "visible"}, {"hidden-file", "ep2", "hidden"}, {"later", "ep3", "visible"}, {"cross-season", "other2", "visible"},
	} {
		path := filepath.Join(t.TempDir(), row.id+".mkv")
		if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
			t.Fatal(err)
		}
		file := model.Media{PermanentBase: model.PermanentBase{ID: row.id}, LibraryID: row.library, Path: path, CatalogSource: source}
		file.SeasonNum, file.EpisodeNum = 1, 2
		if row.item == "ep1" {
			file.EpisodeNum = 1
		} else if row.item == "ep3" {
			file.EpisodeNum = 3
		} else if row.item == "other2" {
			file.SeasonNum = 2
		}
		if source == "" {
			file.MetadataID = row.item
		}
		if err := db.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
		if source == model.TaskSystemHongGuo {
			work := "season1"
			if row.item == "other2" {
				work = "season2"
			}
			if err := db.Create(&model.HongGuoMediaBinding{MediaID: file.ID, WorkID: work, EpisodeID: &row.item}).Error; err != nil {
				t.Fatal(err)
			}
		} else if source == model.CatalogSourceNFO {
			if err := db.Create(&model.NFOMediaBinding{MediaID: file.ID, ItemID: row.item}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {
		visibility: MediaVisibility{IncludeNSFW: true, HiddenLibraryIDs: []string{"hidden"}}, expiresAt: time.Now().Add(time.Hour),
	}}
	return e, serviceTestMediaView(t, e.repo, "current")
}

func TestNextEpisodeMediaIDs(t *testing.T) {
	for _, source := range []string{"", model.TaskSystemHongGuo, model.CatalogSourceNFO} {
		t.Run(source, func(t *testing.T) {
			e, current := newEpisodeProbeTestEnv(t, source)
			filter := repository.MediaQueryFilter{HiddenLibraryIDs: []string{"hidden"}}
			ids, err := e.repo.MediaView.NextEpisodeMediaIDs(t.Context(), current, filter)
			if err != nil || !slices.Equal(ids, []string{"filled", "next-a", "next-b"}) {
				t.Fatalf("next versions=%v error=%v", ids, err)
			}
			filter.AllowedLibraryIDs = []string{"__locked__"}
			ids, err = e.repo.MediaView.NextEpisodeMediaIDs(t.Context(), current, filter)
			if err != nil || len(ids) != 0 {
				t.Fatalf("locked scope=%v error=%v", ids, err)
			}
			for _, episode := range []int{0, 3} {
				current.EpisodeNum = episode
				ids, err = e.repo.MediaView.NextEpisodeMediaIDs(t.Context(), current, repository.MediaQueryFilter{})
				if err != nil || len(ids) != 0 {
					t.Fatalf("unidentified/missing next episode=%v error=%v", ids, err)
				}
			}
			current.EpisodeNum, current.MetadataKind = 1, model.MetadataKindMovie
			ids, err = e.repo.MediaView.NextEpisodeMediaIDs(t.Context(), current, repository.MediaQueryFilter{})
			if err != nil || len(ids) != 0 {
				t.Fatalf("movie next scope=%v error=%v", ids, err)
			}
		})
	}
}

func TestEmbyDetailQueuesNextEpisodeVersions(t *testing.T) {
	testEmbyEpisodeRequestQueuesNextEpisodeVersions(t, false)
}

func TestEmbyPlaybackInfoQueuesNextEpisodeVersions(t *testing.T) {
	testEmbyEpisodeRequestQueuesNextEpisodeVersions(t, true)
}

func testEmbyEpisodeRequestQueuesNextEpisodeVersions(t *testing.T, playback bool) {
	t.Helper()
	for _, source := range []string{"", model.TaskSystemHongGuo, model.CatalogSourceNFO} {
		t.Run(source, func(t *testing.T) {
			e, current := newEpisodeProbeTestEnv(t, source)
			if source == model.TaskSystemHongGuo && embyItemID(current) != "hg-episode-ep1" {
				t.Fatalf("unexpected HongGuo detail ID: %s", embyItemID(current))
			}
			tracker := NewTaskTrackerService(zap.NewNop(), nil)
			runner := &stubMediaProbeRunner{result: probeResultFixture()}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			probe := NewMediaProbeService(e.repo, runner).SetTaskTracker(zap.NewNop(), tracker, ctx)
			for _, id := range []string{"current", "filled"} {
				if _, err := probe.ProbeMedia(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			}
			e.SetMediaProbe(probe)
			starts := make(chan time.Time, 10)
			runner.onProbe = func() { starts <- time.Now() }
			if playback {
				_, err := e.PlaybackInfoWithOptions(t.Context(), embyItemID(current), "viewer", PlaybackSelection{MediaSourceID: "cross-season"})
				if !errors.Is(err, ErrInvalidStreamIndex) {
					t.Fatalf("invalid playback selection error=%v", err)
				}
				probe.autoMu.Lock()
				queued := probe.autoRunning
				probe.autoMu.Unlock()
				if queued || len(tracker.Snapshot().Recent) != 0 {
					t.Fatal("invalid playback request queued next-episode probes")
				}
			}
			manual := tracker.StartTriggeredIfKindIdle(TaskKindProbe, TaskTriggerManual, "manual", TaskUpdate{})
			for range 2 {
				if playback {
					item, err := e.PlaybackInfo(t.Context(), embyItemID(current), "viewer")
					if err != nil || item == nil || item["PlaySessionId"] == "" {
						t.Fatalf("playback info=%v error=%v", item, err)
					}
				} else {
					item, err := e.Item(t.Context(), embyItemID(current), "viewer")
					if err != nil || item == nil || item["Type"] != "Episode" {
						t.Fatalf("detail=%v error=%v", item, err)
					}
				}
			}
			select {
			case <-starts:
				t.Fatal("detail probe overlapped the active task")
			case <-time.After(150 * time.Millisecond):
			}
			manual.Finish(nil, TaskUpdate{})
			var previous time.Time
			for range 2 {
				select {
				case started := <-starts:
					if !previous.IsZero() && started.Sub(previous) < time.Second {
						t.Fatal("next-episode probes did not retain their spacing")
					}
					previous = started
				case <-time.After(5 * time.Second):
					t.Fatal("next-episode version was not probed")
				}
			}
			deadline := time.Now().Add(5 * time.Second)
			for tracker.IsKindRunning(TaskKindProbe) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if tracker.IsKindRunning(TaskKindProbe) {
				t.Fatal("next-episode task did not finish")
			}
			for _, id := range []string{"next-a", "next-b"} {
				if _, ok := probe.Load(t.Context(), id); !ok {
					t.Fatalf("missing next version document: %s", id)
				}
			}
			for _, id := range []string{"hidden-file", "later", "cross-season"} {
				if _, ok := probe.Load(t.Context(), id); ok {
					t.Fatalf("out-of-scope file was probed: %s", id)
				}
			}
			var completed int64
			for _, task := range tracker.Snapshot().Recent {
				if task.Trigger == TaskTriggerEvent && task.Kind == TaskKindProbe {
					completed += task.Metrics["completed"]
				}
			}
			if completed != 2 {
				t.Fatalf("completed=%d; existing documents or duplicate wakes were probed", completed)
			}
		})
	}
}

func TestDetailBackfillSpacingCancelsAndPreservesFiles(t *testing.T) {
	e, _ := newEpisodeProbeTestEnv(t, model.TaskSystemHongGuo)
	runner := &stubMediaProbeRunner{result: probeResultFixture()}
	probe := NewMediaProbeService(e.repo, runner)
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := probe.backfill(ctx, "", 0, nil, false, []string{"next-a", "next-b"})
	if !errors.Is(err, context.DeadlineExceeded) || result.Completed != 1 || time.Since(started) >= time.Second {
		t.Fatalf("spacing cancellation: result=%+v error=%v", result, err)
	}
	runner.err = &exec.ExitError{Stderr: []byte("moov atom not found")}
	result, err = probe.backfill(t.Context(), "", 0, nil, false, []string{"next-b"})
	if err != nil || result.Failed != 1 {
		t.Fatalf("damaged detail probe: result=%+v error=%v", result, err)
	}
	media, err := e.repo.Media.FindByID(t.Context(), "next-b")
	if err != nil || media == nil {
		t.Fatalf("damaged media was removed: %v", err)
	}
	if _, err := os.Stat(media.Path); err != nil {
		t.Fatalf("detail-triggered work deleted the file: %v", err)
	}
}
