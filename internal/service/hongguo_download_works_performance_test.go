package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestHongGuoDownloadWorkCandidatesPreserveResults(t *testing.T) {
	s := newDownloadTestService(t)
	statuses := []string{"queued", "downloading", "waiting_verify", "verifying", "publishing", "completed", "failed", "cancelled"}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 55; i++ {
		id := fmt.Sprintf("%06d", i)
		placement := model.HongGuoDownloadWork{SourceID: id, CreatedAt: at.Add(-time.Duration(i) * time.Hour)}
		if err := s.repo.DB.Create(&placement).Error; err != nil {
			t.Fatal(err)
		}
		first := i
		if i == 54 {
			first = 55 // 相同首任务时间按来源 ID 排序，与位置创建时间无关。
		}
		for episode, status := range statuses {
			row := model.HongGuoDownload{SourceID: id, Episode: episode + 1, Title: id, Status: status}
			row.CreatedAt = at.Add(time.Duration(first)*time.Hour + time.Duration(episode)*time.Second)
			if err := s.repo.DB.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.repo.DB.Create(&model.HongGuoDownloadWork{SourceID: "empty", CreatedAt: at.AddDate(1, 0, 0)}).Error; err != nil {
		t.Fatal(err)
	}
	compare := func() {
		t.Helper()
		for _, status := range append([]string{"", "absent"}, statuses...) {
			for _, page := range []int{1, 2, 3} {
				// 原分集驱动查询作为对照，覆盖计数、排序、分页与筛选后的完整统计。
				base := s.repo.DB.Model(&model.HongGuoDownload{})
				if status != "" {
					base = base.Where("status = ?", status)
				}
				var wantTotal int64
				if err := base.Distinct("source_id").Count(&wantTotal).Error; err != nil {
					t.Fatal(err)
				}
				candidates := s.repo.DB.Model(&model.HongGuoDownload{}).Select("source_id").Group("source_id")
				if status != "" {
					candidates = candidates.Having("BOOL_OR(status = ?)", status)
				}
				candidates = candidates.Order("MIN(created_at) DESC, source_id").Limit(50).Offset((page - 1) * 50)
				want := []HongGuoDownloadSummary{}
				if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("source_id IN (?)", candidates).
					Select(`source_id, MAX(title) AS title, COUNT(*) AS total,
					COUNT(*) FILTER (WHERE status = 'queued') AS queued,
					COUNT(*) FILTER (WHERE status = 'downloading') AS downloading,
					COUNT(*) FILTER (WHERE status = 'waiting_verify') AS waiting_verify,
					COUNT(*) FILTER (WHERE status = 'verifying') AS verifying,
					COUNT(*) FILTER (WHERE status = 'publishing') AS publishing,
					COUNT(*) FILTER (WHERE status = 'completed') AS completed,
					COUNT(*) FILTER (WHERE status = 'failed') AS failed,
					COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled`).
					Group("source_id").Order("MIN(created_at) DESC, source_id").Scan(&want).Error; err != nil {
					t.Fatal(err)
				}
				got, total, err := s.ListWorks(context.Background(), page, status, "")
				if err != nil || total != wantTotal || !slices.Equal(got, want) {
					t.Fatalf("page=%d status=%q total=%d want=%d rows=%v want=%v err=%v", page, status, total, wantTotal, got, want, err)
				}
			}
		}
	}
	compare()
	if err := s.repo.DB.Where("source_id = ? AND episode = 1", "000055").Delete(&model.HongGuoDownload{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Model(&model.HongGuoDownload{}).Where("status = ?", "failed").Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DB.Where("source_id = ?", "000053").Delete(&model.HongGuoDownload{}).Error; err != nil {
		t.Fatal(err)
	}
	compare()
}

func TestHongGuoDownloadWorkPagePlan(t *testing.T) {
	s := newDownloadTestService(t)
	db := s.repo.DB
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1) // 通用预编译计划必须在同一条连接上准备和执行。
	for _, sql := range []string{
		`INSERT INTO hongguo_download_works(source_id,title,root,directory,created_at)
		SELECT n::text,'Fixture','/fixture','fixture',TIMESTAMP '2026-01-01' FROM generate_series(1,3000) n`,
		`INSERT INTO hongguo_downloads(id,source_id,episode,title,status,created_at)
		SELECT 'fixture-'||n||'-'||e,n::text,e,'Fixture',
		CASE WHEN e <= 80 THEN 'completed' WHEN e <= 90 THEN 'failed' WHEN e <= 95 THEN 'cancelled'
		WHEN e = 96 THEN 'queued' WHEN e = 97 THEN 'downloading' WHEN e = 98 THEN 'verifying'
		WHEN e = 99 THEN 'waiting_verify' ELSE 'publishing' END,
		TIMESTAMP '2026-01-01'+n*INTERVAL '1 hour'+e*INTERVAL '1 second'
		FROM generate_series(1,3000) n CROSS JOIN generate_series(1,100) e`,
		`CREATE INDEX idx_hg_download_work_created_c ON hongguo_downloads(source_id COLLATE "C",created_at)`,
		`CREATE INDEX idx_hg_download_status_work_c ON hongguo_downloads(status,source_id COLLATE "C")`,
		`ANALYZE hongguo_downloads`,
		`ANALYZE hongguo_download_works`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	capture := func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if strings.HasPrefix(sql, "WITH download_candidates") {
			queries = append(queries, statement{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
		}
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:download-work-plan", capture); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "completed", "failed", "cancelled", "queued", "downloading", "verifying", "waiting_verify", "publishing", "queued-empty"} {
		t.Run(status, func(t *testing.T) {
			empty := status == "queued-empty"
			if empty {
				status = "queued"
				if err := db.Model(&model.HongGuoDownload{}).Where("status = ?", status).Update("status", "completed").Error; err != nil {
					t.Fatal(err)
				}
			}
			queries = nil
			rows, total, err := s.ListWorks(t.Context(), 1, status, "")
			wantTotal, wantRows := int64(3000), 50
			if empty {
				wantTotal, wantRows = 0, 0
			}
			if err != nil || total != wantTotal || len(rows) != wantRows || len(queries) != 1 {
				t.Fatalf("total=%d rows=%d queries=%d err=%v", total, len(rows), len(queries), err)
			}
			for _, query := range queries {
				for _, generic := range []bool{false, true} {
					explain := "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + query.sql
					args := query.vars
					if generic {
						if err := db.Exec("SET plan_cache_mode = force_generic_plan").Error; err != nil {
							t.Fatal(err)
						}
						if err := db.Exec("PREPARE download_work_plan AS " + query.sql).Error; err != nil {
							t.Fatal(err)
						}
						placeholders := make([]string, len(args))
						for i := range args {
							placeholders[i] = fmt.Sprintf("$%d", i+1)
						}
						explain = db.Dialector.Explain("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE download_work_plan("+strings.Join(placeholders, ",")+")", args...)
						args = nil
					}
					var raw []byte
					if err := db.Statement.ConnPool.QueryRowContext(t.Context(), explain, args...).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					type node struct {
						Type     string  `json:"Node Type"`
						Index    string  `json:"Index Name"`
						Relation string  `json:"Relation Name"`
						Rows     float64 `json:"Actual Rows"`
						Removed  float64 `json:"Rows Removed by Filter"`
						Loops    float64 `json:"Actual Loops"`
						Plans    []node  `json:"Plans"`
					}
					var plans []struct {
						Plan node
						MS   float64 `json:"Execution Time"`
					}
					if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
						t.Fatalf("plan=%s err=%v", raw, err)
					}
					visits := float64(0)
					usesCreatedIndex := false
					var walk func(node)
					walk = func(n node) {
						if n.Index == "idx_hg_download_work_created_c" && n.Loops > 0 {
							usesCreatedIndex = true
						}
						if n.Relation == "hongguo_downloads" {
							visits += (n.Rows + n.Removed) * n.Loops
							if n.Type == "Seq Scan" && n.Loops > 0 {
								t.Fatalf("episode full scan: %s", raw)
							}
						}
						for _, child := range n.Plans {
							walk(child)
						}
					}
					walk(plans[0].Plan)
					if !empty && !usesCreatedIndex {
						t.Fatalf("oldest task lookup missed bytewise index: %s", raw)
					}
					if empty && usesCreatedIndex {
						t.Fatalf("empty status needlessly probed work timestamps: %s", raw)
					}
					if empty && visits != 0 {
						t.Fatalf("empty status visited episode rows=%v: %s", visits, raw)
					}
					if visits > 20000 {
						t.Fatalf("episode visits=%v across 300000 tasks: %s", visits, raw)
					}
					t.Logf("status=%q generic=%v episode visits=%.0f execution=%.2fms", status, generic, visits, plans[0].MS)
					if generic {
						if err := db.Exec("DEALLOCATE download_work_plan").Error; err != nil {
							t.Fatal(err)
						}
						if err := db.Exec("RESET plan_cache_mode").Error; err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		})
	}
}
