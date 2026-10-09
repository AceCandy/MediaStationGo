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

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestHuangGuoAIRefreshCatchesUpEpisodes(t *testing.T) {
	for _, target := range []string{"51", ""} {
		t.Run("target="+target, func(t *testing.T) {
			s := newHuangGuoSupplementTestService(t)
			ctx, db := t.Context(), s.repo.DB
			if err := database.AutoMigrate(db); err != nil {
				t.Fatal(err)
			}
			s.catalog.downloads = s
			complete := false
			summary := huangguoai.Summary{SourceID: "51", Category: "ai-duanju", Title: "Synthetic", Finished: &complete}
			if err := s.repo.HuangGuoAI.RegisterSummaries(ctx, []huangguoai.Summary{summary}); err != nil {
				t.Fatal(err)
			}
			w, _, err := s.repo.HuangGuoAI.SaveDetail(ctx, huangguoai.Work{Summary: summary, Episodes: []huangguoai.Episode{{Number: 1, PagePath: "/video/51/"}}})
			if err != nil {
				t.Fatal(err)
			}
			if n, err := s.Enqueue(ctx, "51"); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			if err := db.Model(&model.HuangGuoAIDownload{}).Where("source_id = ?", "51").Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			var old model.HuangGuoAIDownload
			if err := db.Where("source_id = ?", "51").Take(&old).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(w).Update("refreshed_at", time.Now().Add(-25*time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			// 本轮发现最终更新为完结，最后一集仍须入队。
			if err := db.Model(&model.HuangGuoAIDiscovery{}).Where("source_id = ?", "51").Update("completed", true).Error; err != nil {
				t.Fatal(err)
			}
			var requests int
			s.catalog.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Path != "/video/51/" {
					t.Errorf("unexpected request path")
				}
				body := `<script id="videoInitialData">{"id":"51","title":"Renamed","ep":1,"videoSrc":"https://example.com/full.m3u8"}</script><a href="/video/51/ep-2/">2</a>`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
			})})
			if err := s.catalog.Run(ctx, TaskKindHuangGuoAIRefresh, target); err != nil {
				t.Fatal(err)
			}
			var rows []model.HuangGuoAIDownload
			if err := db.Where("source_id = ?", "51").Order("episode").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if requests != 1 || len(rows) != 2 || !reflect.DeepEqual(old, rows[0]) || rows[1].Status != "queued" {
				t.Fatal("refresh did not preserve old row and enqueue new episode")
			}
			if err := db.Where("source_id = ?", "51").Take(w).Error; err != nil || w.Completed == nil || !*w.Completed {
				t.Fatal("final update not completed", err)
			}
			if rows[1].Root != old.Root || rows[1].Title != old.Title || filepath.Dir(rows[1].RelativePath) != filepath.Dir(old.RelativePath) {
				t.Fatal("placement changed after rename")
			}
			if err := s.catalog.Run(ctx, TaskKindHuangGuoAIRefresh, target); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&model.HuangGuoAIDownload{}).Count(&count).Error; err != nil || count != 2 {
				t.Fatal("duplicate queue", count, err)
			}
			if err := s.repo.Setting.Set(ctx, "huangguoai.enabled", "false"); err != nil {
				t.Fatal(err)
			}
			before := requests
			if err := s.catalog.Run(ctx, TaskKindHuangGuoAIRefresh, target); !errors.Is(err, ErrHuangGuoAIDisabled) || requests != before {
				t.Fatal("disabled source refreshed", err)
			}
		})
	}
}

func TestHuangGuoAICatchUpPreservesStatesAndEligibility(t *testing.T) {
	s := newHuangGuoSupplementTestService(t)
	db, ctx := s.repo.DB, t.Context()
	states := []string{"completed", "queued", "downloading", "waiting_verify", "verifying", "publishing", "failed", "cancelled", "pending_review"}
	var old []model.HuangGuoAIDownload
	for i, status := range states {
		work := model.HuangGuoAIWork{SourceID: fmt.Sprint(i + 1), Title: "Synthetic", Kind: "series", SourceCategory: "ai-manju"}
		if err := db.Create(&work).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: work.ID, Number: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if n, err := s.Enqueue(ctx, work.SourceID); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if err := db.Model(&model.HuangGuoAIDownload{}).Where("source_id = ?", work.SourceID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: work.ID, Number: 3}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Order("source_id").Find(&old).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"20", "21", "22", "23"} {
		work := model.HuangGuoAIWork{SourceID: id, Title: "Excluded", Kind: "series", SourceCategory: "ai-duanju"}
		if id == "21" {
			work.Kind, work.SourceCategory = "movie", "ai-mogai"
		}
		if id == "22" {
			work.ProjectionError = "category_kind_conflict"
		}
		if id == "23" {
			work.SourceCategory = ""
		}
		if err := db.Create(&work).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: work.ID, Number: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if id != "20" {
			if err := db.Create(&model.HuangGuoAIDownloadWork{SourceID: id, Root: "synthetic", Directory: "synthetic"}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.repo.Setting.Set(ctx, "huangguoai.download_root", t.TempDir()); err != nil {
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
		var saved, next model.HuangGuoAIDownload
		if err := db.Where("id = ?", row.ID).Take(&saved).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(row, saved) {
			t.Fatal("old row changed", row.Status)
		}
		if err := db.Where("source_id = ? AND episode = 3", row.SourceID).Take(&next).Error; err != nil {
			t.Fatal(err)
		}
		if next.Root != row.Root || next.Status != "queued" || filepath.Dir(next.RelativePath) != filepath.Dir(row.RelativePath) {
			t.Fatal("new episode did not retain placement")
		}
	}
	if err := s.catchUp(ctx, "", func(string, error) { t.Error("repeated catchup") }); err != nil {
		t.Fatal(err)
	}
}

func TestHuangGuoAICatchUpRetriesCompletedWorkWithoutRefetch(t *testing.T) {
	s := newHuangGuoSupplementTestService(t)
	db, ctx := s.repo.DB, t.Context()
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	s.catalog.downloads = s
	complete := true
	for _, id := range []string{"51", "52"} {
		w := model.HuangGuoAIWork{SourceID: id, Title: "Synthetic", Kind: "series", SourceCategory: "ai-duanju", Completed: &complete, RefreshedAt: time.Now()}
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: w.ID, Number: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.Enqueue(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: w.ID, Number: 2}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 另一个作品资料失败不能阻止已确认的缺集入队。
	if err := s.repo.HuangGuoAI.RegisterSummaries(ctx, []huangguoai.Summary{{SourceID: "73", Category: "ai-duanju", Title: "Unavailable"}}); err != nil {
		t.Fatal(err)
	}
	s.catalog.client = huangguoai.NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/video/73/" {
			t.Error("completed work unnecessarily refetched")
		}
		return nil, errors.New("synthetic detail failure")
	})})
	if err := db.Exec(`CREATE FUNCTION reject_catchup_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.source_id = '51' AND NEW.episode = 2 THEN RAISE EXCEPTION 'synthetic failure'; END IF; RETURN NEW; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_catchup_fixture BEFORE INSERT ON huangguoai_downloads FOR EACH ROW EXECUTE FUNCTION reject_catchup_fixture()`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.catalog.Run(ctx, TaskKindHuangGuoAIRefresh, ""); err == nil {
		t.Fatal("enqueue failure ignored")
	}
	var count int64
	if err := db.Model(&model.HuangGuoAIDownload{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatal("lost independent success", count, err)
	}
	if err := db.Model(&model.HuangGuoAISyncFailure{}).Where("source_key IN ?", []string{"51", "52"}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("enqueue failure misclassified as detail failure", count, err)
	}
	if err := db.Exec(`DROP TRIGGER reject_catchup_fixture ON huangguoai_downloads`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.catalog.Run(ctx, TaskKindHuangGuoAIRefresh, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.HuangGuoAIDownload{}).Count(&count).Error; err != nil || count != 4 {
		t.Fatal("completed work gap lost", count, err)
	}
}

func TestHuangGuoAICatchUpPaginationConcurrentAndCancellation(t *testing.T) {
	s := newHuangGuoSupplementTestService(t)
	db, ctx := s.repo.DB, t.Context()
	for i := 1; i <= 102; i++ {
		w := model.HuangGuoAIWork{SourceID: fmt.Sprint(i), Title: "Synthetic", Kind: "series", SourceCategory: "ai-duanju"}
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: w.ID, Number: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIDownloadWork{SourceID: w.SourceID, Title: w.Title, Root: t.TempDir(), Directory: w.SourceID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.catchUp(cancelled, "", func(string, error) { t.Error("cancelled catchup reported work") }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	inFlight, cancelInFlight := context.WithCancel(ctx)
	defer cancelInFlight()
	if err := s.catchUp(inFlight, "", func(string, error) { cancelInFlight() }); !errors.Is(err, context.Canceled) {
		t.Fatal("in-flight cancellation ignored", err)
	}
	var admitted int64
	if err := db.Model(&model.HuangGuoAIDownload{}).Count(&admitted).Error; err != nil || admitted != 1 {
		t.Fatal("cancellation admitted more episodes", admitted, err)
	}
	if err := s.repo.Setting.Set(ctx, "huangguoai.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if err := s.catchUp(ctx, "2", func(_ string, err error) {
		if !errors.Is(err, ErrHuangGuoAIDisabled) {
			t.Error(err)
		}
	}); err == nil {
		t.Fatal("disabled catchup accepted")
	}
	if err := s.repo.Setting.Set(ctx, "huangguoai.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
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
	var count int64
	if err := db.Model(&model.HuangGuoAIDownload{}).Count(&count).Error; err != nil || count != 102 {
		t.Fatal("pagination or concurrent dedup", count, err)
	}
}
