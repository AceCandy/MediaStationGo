package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestHongGuoRefreshCatchesUpEpisodes(t *testing.T) {
	for _, target := range []string{"51", ""} {
		t.Run("target="+target, func(t *testing.T) {
			s := newDownloadTestService(t)
			ctx, db := t.Context(), s.repo.DB
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			enableDownloadTaskPersistence(t, s)
			s.catalog.downloads = s
			w, _, err := s.repo.HongGuo.SaveDetailWithChange(ctx, hongguo.Work{SourceID: "51", Title: "Synthetic", Snapshot: []byte(`{}`), VideoIDs: []string{"456"}, EpisodeCount: 1})
			if err != nil {
				t.Fatal(err)
			}
			if n, err := s.Enqueue(ctx, "51"); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			if err := db.Model(&model.HongGuoDownload{}).Where("source_id = ?", "51").Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			var old model.HongGuoDownload
			if err := db.Where("source_id = ?", "51").Take(&old).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(w).Update("refreshed_at", time.Now().Add(-25*time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			requests := 0
			s.catalog.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Path != "/detail" || req.URL.Query().Get("series_id") != "51" {
					t.Error("unexpected detail request")
				}
				body := `_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"51","series_name":"Renamed","vid_list":["456","457"],"episode_cnt":2,"episode_right_text":"全2集"}}}}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
			})})
			if err := s.catalog.Run(ctx, TaskKindHongGuoRefresh, target); err != nil {
				t.Fatal(err)
			}
			var rows []model.HongGuoDownload
			if err := db.Where("source_id = ?", "51").Order("episode").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 || requests != 1 || !reflect.DeepEqual(rows[0], old) || rows[1].Status != "queued" || rows[1].VideoID != "457" {
				t.Fatal("refresh did not enqueue new episode and preserve history")
			}
			if rows[1].Root != old.Root || rows[1].Title != old.Title || filepath.Dir(rows[1].RelativePath) != filepath.Dir(old.RelativePath) {
				t.Fatal("placement changed")
			}
			if err := db.Where("source_id = ?", "51").Take(w).Error; err != nil || !w.Completed {
				t.Fatal("final completion not persisted", err)
			}
			if err := s.catalog.Run(ctx, TaskKindHongGuoRefresh, target); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&model.HongGuoDownload{}).Count(&count).Error; err != nil || count != 2 {
				t.Fatal("duplicate catchup", count, err)
			}
			if err := s.repo.Setting.Set(ctx, "hongguo.enabled", "false"); err != nil {
				t.Fatal(err)
			}
			before := requests
			if err := s.catalog.Run(ctx, TaskKindHongGuoRefresh, target); !errors.Is(err, ErrHongGuoDisabled) || requests != before {
				t.Fatal("disabled source refreshed", err)
			}
		})
	}
}

func TestHongGuoCatchUpPreservesStatesAndConfirmedIDs(t *testing.T) {
	s := newDownloadTestService(t)
	db, ctx := s.repo.DB, t.Context()
	states := []string{"completed", "queued", "downloading", "waiting_verify", "verifying", "publishing", "failed", "cancelled"}
	var old []model.HongGuoDownload
	for i, status := range states {
		w := model.HongGuoWork{SourceID: fmt.Sprint(i + 1), Title: "Synthetic", Kind: "series"}
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HongGuoEpisode{WorkID: w.ID, Number: 1, SourceVideoID: "456"}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.Enqueue(ctx, w.SourceID); err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.HongGuoDownload{}).Where("source_id = ?", w.SourceID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		eps := []model.HongGuoEpisode{{WorkID: w.ID, Number: 2, SourceVideoID: ""}, {WorkID: w.ID, Number: 3, SourceVideoID: "invalid"}, {WorkID: w.ID, Number: 4, SourceVideoID: "457"}}
		if err := db.Create(&eps).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Order("source_id").Find(&old).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"20", "21"} {
		w := model.HongGuoWork{SourceID: id, Title: "Excluded", Kind: "series"}
		if id == "21" {
			w.SourceCategory = "comic"
		}
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HongGuoEpisode{WorkID: w.ID, Number: 1, SourceVideoID: "458"}).Error; err != nil {
			t.Fatal(err)
		}
		if id == "21" {
			if err := db.Create(&model.HongGuoDownloadWork{SourceID: id, Root: "synthetic", Directory: "synthetic"}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.repo.Setting.Set(ctx, hongGuoDownloadRootKey, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	reports := 0
	if err := s.catchUp(ctx, "", func(_ string, err error) {
		reports++
		if err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if reports != len(states) {
		t.Fatal("eligibility", reports)
	}
	for _, row := range old {
		var saved, next model.HongGuoDownload
		if err := db.Where("id = ?", row.ID).Take(&saved).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(row, saved) {
			t.Fatal("old download changed", row.Status)
		}
		if err := db.Where("source_id = ? AND episode = 4", row.SourceID).Take(&next).Error; err != nil {
			t.Fatal(err)
		}
		if next.Root != row.Root || filepath.Dir(next.RelativePath) != filepath.Dir(row.RelativePath) || next.VideoID != "457" {
			t.Fatal("placement or video changed")
		}
	}
	var count int64
	if err := db.Model(&model.HongGuoDownload{}).Count(&count).Error; err != nil || count != 16 {
		t.Fatal("unconfirmed episodes queued", count, err)
	}
	if err := s.catchUp(ctx, "", func(string, error) { t.Error("duplicate catchup") }); err != nil {
		t.Fatal(err)
	}
	// 普通手动入队仍允许无视频 ID；自动补集不能改变原行为。
	if n, err := s.Enqueue(ctx, "1"); err != nil || n != 2 {
		t.Fatal("manual enqueue changed", n, err)
	}
	// 事务内复核位置，不能根据查询前的候选重建已清理位置。
	if err := db.Where("source_id = ?", "20").Delete(&model.HongGuoDownloadWork{}).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := s.enqueueWork(ctx, "20", false, true); err != nil || n != 0 {
		t.Fatal("recreated removed placement", n, err)
	}
	if n, err := s.enqueueWork(ctx, "21", false, true); err != nil || n != 0 {
		t.Fatal("comic queued", n, err)
	}
}

func TestHongGuoCatchUpRetryCompletedAndPartialFailure(t *testing.T) {
	s := newDownloadTestService(t)
	db, ctx := s.repo.DB, t.Context()
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	enableDownloadTaskPersistence(t, s)
	s.catalog.downloads = s
	for _, id := range []string{"51", "52"} {
		w := model.HongGuoWork{SourceID: id, Title: "Synthetic", Kind: "series", Completed: true, RefreshedAt: time.Now()}
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HongGuoEpisode{WorkID: w.ID, Number: 1, SourceVideoID: "456"}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.Enqueue(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HongGuoEpisode{WorkID: w.ID, Number: 2, SourceVideoID: "457"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.HongGuoWork{SourceID: "73", Title: "Unavailable", Kind: "series", RefreshedAt: time.Now().Add(-25 * time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	s.catalog.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("series_id") != "73" {
			t.Error("completed work refetched")
		}
		return nil, errors.New("synthetic detail failure")
	})})
	if err := db.Exec(`CREATE FUNCTION reject_hg_catchup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.source_id = '51' AND NEW.episode = 2 THEN RAISE EXCEPTION 'private-database-fixture'; END IF; RETURN NEW; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_hg_catchup BEFORE INSERT ON hongguo_downloads FOR EACH ROW EXECUTE FUNCTION reject_hg_catchup()`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.catalog.Run(ctx, TaskKindHongGuoRefresh, ""); err == nil {
		t.Fatal("failure ignored")
	}
	var count int64
	if err := db.Model(&model.HongGuoDownload{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatal("independent success lost", count, err)
	}
	if err := db.Model(&model.HongGuoSyncFailure{}).Where("source_id IN ?", []string{"51", "52"}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("enqueue recorded as detail failure", count, err)
	}
	history, err := s.tasks.DefinitionHistory(TaskKindHongGuoRefresh, 1, 10)
	if err != nil || history.Total != 1 || history.Items[0].Status != TaskStatusFailed || strings.Contains(history.Items[0].Error, "private-database-fixture") {
		t.Fatal("unsafe or missing task failure", err)
	}
	if err := db.Exec(`DROP TRIGGER reject_hg_catchup ON hongguo_downloads`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.catalog.Run(ctx, TaskKindHongGuoRefresh, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.HongGuoDownload{}).Count(&count).Error; err != nil || count != 4 {
		t.Fatal("completed gap lost", count, err)
	}
}

func TestHongGuoCatchUpPagingConcurrentAndCancel(t *testing.T) {
	s := newDownloadTestService(t)
	db, ctx := s.repo.DB, t.Context()
	for i := 1; i <= 102; i++ {
		w := model.HongGuoWork{SourceID: fmt.Sprint(i), Title: "Synthetic", Kind: "series"}
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HongGuoEpisode{WorkID: w.ID, Number: 1, SourceVideoID: "456"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HongGuoDownloadWork{SourceID: w.SourceID, Title: w.Title, Root: t.TempDir(), Directory: w.SourceID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	inFlight, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := s.catchUp(inFlight, "", func(string, error) { cancel() }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.HongGuoDownload{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("cancel failed", count, err)
	}
	if err := s.repo.Setting.Set(ctx, "hongguo.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if err := s.catchUp(ctx, "2", func(_ string, err error) {
		if err == nil {
			t.Error("disabled admitted")
		}
	}); err == nil {
		t.Fatal("disabled accepted")
	}
	if err := s.repo.Setting.Set(ctx, "hongguo.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.catchUp(ctx, "", func(_ string, err error) {
				if err != nil {
					t.Error(err)
				}
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := db.Model(&model.HongGuoDownload{}).Count(&count).Error; err != nil || count != 102 {
		t.Fatal("paging/concurrency", count, err)
	}
}

func TestHongGuoCatchUpDoesNotRestoreConfirmedUnavailableEpisodes(t *testing.T) {
	s, row := newReconcileTestService(t)
	db, ctx := s.repo.DB, t.Context()
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	work, err := s.repo.HongGuo.FindBySourceID(ctx, row.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: 2, SourceVideoID: "457"}).Error; err != nil {
		t.Fatal(err)
	}
	protected := model.HongGuoDownload{SourceID: row.SourceID, Episode: 2, VideoID: "457", Status: "completed", Root: row.Root, RelativePath: "protected.mp4"}
	if err := db.Create(&protected).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.removeUnavailableDownloads(ctx, row, nil); !errors.Is(err, errHongGuoDownloadRemoved) {
		t.Fatal(err)
	}
	if err := s.catchUp(ctx, "", func(string, error) { t.Error("unavailable episodes restored") }); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.HongGuoDownload{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("completed history changed", count, err)
	}
	var unavailable model.HongGuoEpisode
	if err := db.Where("work_id = ? AND number = 1", work.ID).Take(&unavailable).Error; err != nil || unavailable.SourceVideoID != "" {
		t.Fatal("unavailable episode remained eligible", err)
	}
	var episode model.HongGuoEpisode
	if err := db.Where("work_id = ? AND number = 2", work.ID).Take(&episode).Error; err != nil || episode.SourceVideoID != "457" {
		t.Fatal("protected episode changed", err)
	}
}

func TestHongGuoCatchUpDoesNotRestoreUnavailableWorkRetainedByMedia(t *testing.T) {
	s, row := newReconcileTestService(t)
	db, ctx := s.repo.DB, t.Context()
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	media := model.Media{Path: "/synthetic-retained.mp4", CatalogSource: "hongguo", LookupCatalogID: row.SourceID}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	work, err := s.repo.HongGuo.FindBySourceID(ctx, row.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	var episode model.HongGuoEpisode
	if err := db.Where("work_id = ? AND number = 1", work.ID).Take(&episode).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.HongGuoMediaBinding{MediaID: media.ID, WorkID: work.ID, EpisodeID: &episode.ID}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.removeUnavailableDownloads(ctx, row, nil); !errors.Is(err, errHongGuoDownloadRemoved) {
		t.Fatal(err)
	}
	if err := s.catchUp(ctx, "", func(string, error) { t.Error("unavailable episode revived from retained work") }); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.HongGuoDownload{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("unavailable queue revived", count, err)
	}
	if err := db.Model(&model.HongGuoWork{}).Where("source_id = ?", row.SourceID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("media work removed", count, err)
	}
	if err := db.First(&episode, "id = ?", episode.ID).Error; err != nil || episode.SourceVideoID != "" {
		t.Fatal("bound episode identity lost or still eligible", err)
	}
	if err := db.First(&binding, "media_id = ?", media.ID).Error; err != nil {
		t.Fatal("binding lost", err)
	}
}
