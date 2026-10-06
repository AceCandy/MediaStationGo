package service

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func newHuangGuoSupplementTestService(t *testing.T) *HuangGuoAIDownloadService {
	t.Helper()
	s := newHuangGuoDownloadTaskTestService(t)
	if err := s.repo.DB.AutoMigrate(&model.HuangGuoAIDiscovery{}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHuangGuoAIDownloadSupplement(t *testing.T) {
	s := newHuangGuoSupplementTestService(t)
	db, ctx := s.repo.DB, t.Context()
	for i := 1; i <= 17; i++ {
		w := model.HuangGuoAIWork{SourceID: fmt.Sprint(i), Title: "合成作品", Kind: "series", SourceCategory: "ai-duanju"}
		switch i {
		case 10:
			w.ProjectionError = "category_conflict"
		case 11:
			w.SourceCategory = ""
		case 12, 13:
			w.Kind, w.SourceCategory = "movie", "ai-mogai"
		case 14:
			w.Kind, w.SourceCategory = "movie", "ai-huanlian"
		}
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if i == 9 {
			continue // 无确认分集。
		}
		numbers := []int{1}
		if i == 12 {
			numbers = []int{1, 2}
		} else if i == 13 {
			numbers = []int{2}
		} else if i == 15 {
			numbers = []int{1, 2, 3}
		}
		for _, n := range numbers {
			if err := db.Create(&model.HuangGuoAIEpisode{WorkID: w.ID, Number: n}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if i == 16 {
			// 即使历史分集已移除，曾使用的下载位置也不能自动重下。
			if err := db.Create(&model.HuangGuoAIDownloadWork{SourceID: w.SourceID, Root: "synthetic", Directory: "synthetic"}).Error; err != nil {
				t.Fatal(err)
			}
		} else if i == 17 {
			// 兼容只有分集历史、没有下载位置的记录。
			if err := db.Create(&model.HuangGuoAIDownload{SourceID: w.SourceID, Episode: 1, Status: "completed", Root: "synthetic", RelativePath: "synthetic"}).Error; err != nil {
				t.Fatal(err)
			}
			if n, err := s.enqueue(ctx, w.SourceID, true); err != nil || n != 0 {
				t.Fatal("ignored legacy episode history", n, err)
			}
		}
		if i <= 8 {
			if _, err := s.Enqueue(ctx, w.SourceID); err != nil {
				t.Fatal(err)
			}
			statuses := []string{"completed", "queued", "downloading", "verifying", "waiting_verify", "publishing", "failed", "cancelled"}
			if err := db.Model(&model.HuangGuoAIDownload{}).Where("source_id = ?", w.SourceID).Update("status", statuses[i-1]).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, count := range []int{0, -1, 101} {
		if _, err := s.Supplement(ctx, count); !errors.Is(err, ErrSchedulerCountInvalid) {
			t.Fatal("invalid count", count, err)
		}
	}
	result, err := s.Supplement(ctx, 100)
	if err != nil || result.Candidates != 2 || result.Works != 2 || result.Episodes != 4 || result.Failed != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result, err = s.Supplement(ctx, 100); err != nil || result.Candidates != 0 {
		t.Fatalf("repeat=%+v err=%v", result, err)
	}
	var failed int64
	if err := db.Model(&model.HuangGuoAIDownload{}).Where("status IN ?", []string{"failed", "cancelled"}).Count(&failed).Error; err != nil || failed != 2 {
		t.Fatal("changed old states", failed, err)
	}
	if err := s.repo.Setting.Set(ctx, "huangguoai.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Supplement(ctx, 1); !errors.Is(err, ErrHuangGuoAIDisabled) {
		t.Fatal(err)
	}
	if err := s.repo.Setting.Set(ctx, "huangguoai.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Setting.Set(ctx, "huangguoai.download_root", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Supplement(ctx, 1); err == nil {
		t.Fatal("accepted missing root")
	}
}

func TestHuangGuoAIDownloadSupplementOrderAndConcurrent(t *testing.T) {
	s := newHuangGuoSupplementTestService(t)
	db, ctx := s.repo.DB, t.Context()
	for i := 1; i <= 4; i++ {
		w := model.HuangGuoAIWork{SourceID: fmt.Sprint(i), Title: "合成作品", Kind: "series", SourceCategory: "ai-manju"}
		w.CreatedAt = time.Date(2026, 9, 5-i, 0, 0, 0, 0, time.UTC)
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.HuangGuoAIEpisode{WorkID: w.ID, Number: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if i < 4 {
			if err := db.Create(&model.HuangGuoAIDiscovery{SourceID: w.SourceID, CreatedAt: time.Date(2026, 9, 10+i, 0, 0, 0, 0, time.UTC)}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if result, err := s.Supplement(ctx, 1); err != nil || result.Works != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	var row model.HuangGuoAIDownload
	if err := db.First(&row).Error; err != nil || row.SourceID != "3" {
		t.Fatalf("first discovery order: %s %v", row.SourceID, err)
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
		t.Fatalf("duplicate enqueue: %d", total)
	}
	var work model.HuangGuoAIWork
	if err := db.Where("source_id = ?", "2").Take(&work).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HuangGuoAIEpisode{WorkID: work.ID, Number: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := s.enqueue(ctx, "2", true); err != nil || n != 0 {
		t.Fatal("onlyNew supplemented an old work", n, err)
	}
	if n, err := s.Enqueue(ctx, "2"); err != nil || n != 1 {
		t.Fatal("manual episode enqueue changed", n, err)
	}
	if result, err := s.Supplement(ctx, 100); err != nil || result.Works != 2 {
		t.Fatalf("remaining=%+v %v", result, err)
	}
}
