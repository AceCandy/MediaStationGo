package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestDownloadWorkSearchBeforePagination(t *testing.T) {
	for _, source := range []string{"hongguo", "huangguoai"} {
		t.Run(source, func(t *testing.T) {
			db, err := testdb.OpenPostgres(t, &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.HongGuoDownloadWork{}, &model.HongGuoDownload{}, &model.HuangGuoAIWork{}, &model.HuangGuoAIDownloadWork{}, &model.HuangGuoAIDownload{}); err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= 55; i++ {
				id, title := fmt.Sprint(i), fmt.Sprintf("批量作品 %d", i)
				if i == 1 {
					id = "99999"
					title = `定位作品 Special 100%_\`
				}
				created := time.Unix(int64(i), 0)
				if source == "hongguo" {
					err = db.Create(&model.HongGuoDownloadWork{SourceID: id, Title: title, CreatedAt: created}).Error
					if err == nil {
						err = db.Create(&[]model.HongGuoDownload{{PermanentBase: model.PermanentBase{CreatedAt: created}, SourceID: id, Title: title, Episode: 1, Status: "failed"}, {SourceID: id, Title: title, Episode: 2, Status: "completed"}}).Error
					}
				} else {
					err = db.Create(&model.HuangGuoAIDownloadWork{SourceID: id, Title: title, CreatedAt: created}).Error
					if err == nil {
						err = db.Create(&[]model.HuangGuoAIDownload{{SourceID: id, Title: title, Episode: 1, Status: "failed"}, {SourceID: id, Title: title, Episode: 2, Status: "completed"}}).Error
					}
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			repo := repository.New(db)
			for _, tt := range []struct {
				keyword, status string
				page, count     int
				total           int64
			}{
				{"", "", 1, 50, 55},
				{"定位", "failed", 1, 1, 1},
				{"special", "", 1, 1, 1},
				{" 99999 ", "", 1, 1, 1},
				{"%", "", 1, 1, 1},
				{"_", "", 1, 1, 1},
				{`\`, "", 1, 1, 1},
				{"定位", "downloading", 1, 0, 0},
				{"不存在", "", 1, 0, 0},
				{"批量", "completed", 1, 50, 54},
				{"批量", "completed", 2, 4, 54},
				{"批量", "completed", 3, 0, 54},
			} {
				var rows []HongGuoDownloadSummary
				var total int64
				if source == "hongguo" {
					rows, total, err = NewHongGuoDownloadService(repo, nil, nil).ListWorks(context.Background(), tt.page, tt.status, tt.keyword)
				} else {
					var items []HuangGuoAIDownloadWorkSummary
					items, total, err = NewHuangGuoAIDownloadService(repo, nil, nil).Works(context.Background(), tt.page, tt.status, tt.keyword)
					for _, item := range items {
						if item.Kind != "" {
							t.Fatal("missing work classification was invented")
						}
						rows = append(rows, HongGuoDownloadSummary{SourceID: item.SourceID, Total: item.Total, Failed: item.Failed, Completed: item.Completed})
					}
				}
				if err != nil || len(rows) != tt.count || total != tt.total {
					t.Fatalf("%+v: count=%d total=%d err=%v", tt, len(rows), total, err)
				}
				for _, row := range rows {
					if row.Total != 2 || row.Failed != 1 || row.Completed != 1 {
						t.Fatalf("summary lost episodes: %+v", row)
					}
				}
				if tt.total == 1 && rows[0].SourceID != "99999" {
					t.Fatalf("did not find work outside original first page: %+v", rows)
				}
			}
		})
	}
}
