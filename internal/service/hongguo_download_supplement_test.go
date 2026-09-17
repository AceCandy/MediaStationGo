package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestHongGuoDownloadSupplement(t *testing.T) {
	s := newDownloadTestService(t)
	ctx := context.Background()
	for i := 1; i <= 12; i++ {
		at := time.Date(2026, 9, i, 0, 0, 0, 0, time.UTC)
		work := model.HongGuoWork{SourceID: fmt.Sprint(100 + i), Title: fmt.Sprint(i), Kind: "series", FirstVisibleAt: &at}
		if i == 9 {
			work.SourceCategory = "comic"
		}
		if err := s.repo.DB.Create(&work).Error; err != nil {
			t.Fatal(err)
		}
		if i == 10 {
			continue
		}
		video := "456"
		if i == 11 {
			video = "invalid"
		}
		if err := s.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: 1, SourceVideoID: video}).Error; err != nil {
			t.Fatal(err)
		}
		if i <= 8 {
			statuses := []string{"completed", "queued", "downloading", "verifying", "waiting_verify", "publishing", "failed", "cancelled"}
			if _, err := s.Enqueue(ctx, work.SourceID); err != nil {
				t.Fatal(err)
			}
			if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id = ?", work.SourceID).Update("status", statuses[i-1]).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, count := range []int{0, -1, 101} {
		if _, err := s.Supplement(ctx, count); err == nil {
			t.Fatal("accepted invalid count", count)
		}
	}
	result, err := s.Supplement(ctx, 3)
	if err != nil || result.Candidates != 1 || result.Works != 1 || result.Episodes != 1 || result.Failed != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = s.Supplement(ctx, 3)
	if err != nil || result.Candidates != 0 || result.Works != 0 {
		t.Fatalf("repeat=%+v %v", result, err)
	}
	var rows []model.HongGuoDownload
	if err := s.repo.DB.Order("source_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 9 || rows[6].Status != "failed" || rows[7].Status != "cancelled" || rows[8].SourceID != "112" {
		t.Fatalf("unexpected queue: %+v", rows)
	}
	if err := s.repo.DB.Model(&model.Setting{}).Where("key = ?", hongGuoDownloadRootKey).Update("value", "").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Supplement(ctx, 1); err == nil {
		t.Fatal("accepted unconfigured root")
	}
}

func TestHongGuoDownloadSupplementOrderAndConcurrent(t *testing.T) {
	s := newDownloadTestService(t)
	ctx := context.Background()
	for i := 1; i <= 4; i++ {
		at := time.Date(2026, 9, i, 0, 0, 0, 0, time.UTC)
		work := model.HongGuoWork{SourceID: fmt.Sprint(i), Title: "测试", Kind: "series", FirstVisibleAt: &at}
		if i == 4 {
			work.FirstVisibleAt = nil
		}
		if err := s.repo.DB.Create(&work).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.repo.DB.Create(&model.HongGuoEpisode{WorkID: work.ID, Number: 1, SourceVideoID: "456"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.Supplement(ctx, 1)
	if err != nil || result.Works != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	var row model.HongGuoDownload
	if err := s.repo.DB.First(&row).Error; err != nil || row.SourceID != "3" {
		t.Fatalf("order=%+v %v", row, err)
	}
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := s.enqueue(ctx, "2", true)
			if err != nil {
				t.Error(err)
			}
			counts <- n
		}()
	}
	wg.Wait()
	close(counts)
	total := 0
	for n := range counts {
		total += n
	}
	if total != 1 {
		t.Fatalf("duplicate concurrent enqueue: %d", total)
	}
	result, err = s.Supplement(ctx, 100)
	if err != nil || result.Works != 2 {
		t.Fatalf("remaining=%+v %v", result, err)
	}
}

func TestHongGuoDownloadHistoryHiddenFromTaskCenter(t *testing.T) {
	s := newDownloadTestService(t)
	h := s.tasks.StartTriggered(TaskKindHongGuoDownload, TaskTriggerManual, "红果下载测试", TaskUpdate{Stage: "downloading"})
	if h == nil {
		t.Fatal("missing task")
	}
	h.Finish(nil, TaskUpdate{Stage: "waiting_verify"})
	definitions, err := s.tasks.DefinitionsForSystem(nil, "hongguo")
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if definition.Key == TaskKindHongGuoDownload {
			t.Fatal("download still visible")
		}
	}
	history, err := s.tasks.DefinitionHistory(TaskKindHongGuoDownload, 1, 20)
	if err != nil || history.Total != 1 || history.Items[0].Stage != "waiting_verify" {
		t.Fatalf("history=%+v %v", history, err)
	}
}
