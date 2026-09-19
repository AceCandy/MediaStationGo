package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestHongGuoDownloadClaimOrderAndPlan(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
	}
	r := New(db).HongGuo
	ctx := context.Background()
	for _, verify := range []bool{false, true} {
		if row, err := r.claimHongGuoDownload(ctx, verify); err != nil || row != nil {
			t.Fatalf("empty queue: row=%v err=%v", row, err)
		}
	}
	// 同时包含大量已结束任务和待下载任务，暴露先扫描候选再排序的计划。
	if err := db.Exec(`INSERT INTO hong_guo_downloads (id, source_id, episode, status, raw_size, sha256, created_at)
		SELECT 'bulk-' || lpad(i::text, 6, '0'), 'bulk', i,
		CASE WHEN i <= 20000 THEN 'completed' ELSE 'queued' END, 0, '', now()
		FROM generate_series(1, 40000) AS i`).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	rows := []model.HongGuoDownload{
		{Status: "failed"},
		{Status: "cancelled", RawSize: 10},
		{Status: "downloading", LeaseUntil: &future},
		{Status: "verifying", LeaseUntil: &future, RawSize: 10},
		{Status: "downloading"},
		{Status: "queued"},
		{Status: "downloading", LeaseUntil: &past},
		{Status: "waiting_verify", RawSize: 10},
		{Status: "publishing", LeaseUntil: &past, SHA256: "checkpoint"},
		{Status: "queued", RawSize: 10},
	}
	for i := range rows {
		rows[i].ID = fmt.Sprintf("candidate-%02d", i)
		rows[i].SourceID = "candidate"
		rows[i].Episode = i + 1
		rows[i].CreatedAt = past
		rows[i].Attempts = 2
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&rows[5]).Update("sha256", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ANALYZE hong_guo_downloads").Error; err != nil {
		t.Fatal(err)
	}
	var claimSQL string
	var claimVars []interface{}
	if err := db.Callback().Query().After("gorm:query").Register("test:capture_claim", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "FOR UPDATE SKIP LOCKED") {
			claimSQL = tx.Statement.SQL.String()
			claimVars = append([]interface{}(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		verify bool
		index  string
		want   []int
	}{
		{false, "idx_hg_download_transfer_claim", []int{5, 6}},
		{true, "idx_hg_download_verification_claim", []int{7, 8, 9}},
	} {
		for _, i := range tt.want {
			row, err := r.claimHongGuoDownload(ctx, tt.verify)
			if err != nil || row == nil || row.ID != rows[i].ID {
				t.Fatalf("claim verify=%t want=%s row=%v err=%v", tt.verify, rows[i].ID, row, err)
			}
			wantStatus, attempts := "downloading", 3
			if tt.verify {
				wantStatus, attempts = "verifying", 2
				if rows[i].SHA256 != "" {
					wantStatus = "publishing"
				}
			}
			if row.Status != wantStatus || row.Attempts != attempts || row.LeaseToken == "" || row.LeaseUntil == nil || !row.LeaseUntil.After(now) {
				t.Fatalf("claim state: %+v", row)
			}
		}
		// 使用真实仓储生成的参数化 SQL，防止测试副本与线上查询漂移。
		name := "claim_transfer"
		if tt.verify {
			name = "claim_verification"
		}
		if err := db.Exec("SET plan_cache_mode = force_generic_plan").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("PREPARE " + name + " AS " + claimSQL).Error; err != nil {
			t.Fatal(err)
		}
		var plan []string
		execute := "EXPLAIN (ANALYZE, BUFFERS) EXECUTE " + name + "("
		for i := range claimVars {
			if i > 0 {
				execute += ", "
			}
			execute += fmt.Sprintf("$%d", i+1)
		}
		execute = db.Dialector.Explain(execute+")", claimVars...)
		if err := db.Raw(execute).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(plan, "\n")
		if !strings.Contains(joined, tt.index) || strings.Contains(joined, "Sort") || strings.Contains(joined, "Seq Scan") {
			t.Fatalf("claim must use ordered partial index:\n%s", joined)
		}
		t.Log(joined)
	}
}
