package repository

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoDownloadedBadgeUsesCompletedHistory(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(append(model.HongGuoModels(), &model.HongGuoDownload{})...); err != nil {
		t.Fatal(err)
	}
	r, ctx := New(db).HongGuo, t.Context()
	statuses := []string{"completed", "failed", "queued", "downloading", "waiting_verify", "verifying", "publishing", "cancelled", ""}
	remote := make([]hongguo.Work, 0, len(statuses))
	for i, status := range statuses {
		id := fmt.Sprint(91000 + i)
		work := model.HongGuoWork{SourceID: id, Kind: "series", Title: "同名作品", Tags: "[]"}
		if err := db.Create(&work).Error; err != nil {
			t.Fatal(err)
		}
		remote = append(remote, hongguo.Work{SourceID: id, Title: work.Title})
		if status == "" {
			continue
		}
		// 根目录和输出文件从未存在，不能影响历史下载标识。
		row := model.HongGuoDownload{SourceID: id, Episode: 1, Status: status, Root: filepath.Join(t.TempDir(), "missing"), RelativePath: "missing.mp4"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 有一集完成即可显示，不要求该剧全部完成；同源多条完成记录不得重复海报。
	for _, row := range []model.HongGuoDownload{{SourceID: "91000", Episode: 2, Status: "failed"}, {SourceID: "91000", Episode: 3, Status: "completed"}, {SourceID: "92000", Episode: 1, Status: "completed"}} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.HongGuoDiscovery{SourceID: "92000", Title: "摘要作品"}).Error; err != nil {
		t.Fatal(err)
	}
	remote = append(remote, hongguo.Work{SourceID: "92000", Title: "摘要作品"})
	if err := db.Create(&model.HongGuoRankEntry{RankKey: "hot-drama", SourceID: "91000", Position: 1}).Error; err != nil {
		t.Fatal(err)
	}
	check := func(rows []HongGuoListWork, want int) {
		t.Helper()
		if len(rows) != want {
			t.Fatalf("rows=%d want=%d", len(rows), want)
		}
		for _, row := range rows {
			if row.Downloaded != (row.SourceID == "91000" || row.SourceID == "92000") {
				t.Fatalf("wrong badge: source=%s downloaded=%v", row.SourceID, row.Downloaded)
			}
		}
	}
	rows, total, err := r.List(ctx, "", "", "", "", 1, 50)
	if err != nil || total != int64(len(remote)) {
		t.Fatalf("list: total=%d err=%v", total, err)
	}
	check(rows, len(remote))
	rows, _, err = r.List(ctx, "", "", "", "hot-drama", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	check(rows, 1)
	rows, err = r.SearchResults(ctx, remote)
	if err != nil {
		t.Fatal(err)
	}
	check(rows, len(remote))
	rows, _, err = r.List(ctx, "91000", "", "", "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	check(rows, 1)
}
