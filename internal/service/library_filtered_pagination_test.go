package service

import (
	"encoding/json"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestLibraryFilteredSeriesPageWithStaleStatistics(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	// 先统计旧库，再加入新库，复现新库文件数被低估；未关联文件和无文件目录均不得进入卡片。
	for _, sql := range []string{
		`ALTER TABLE media SET (autovacuum_enabled = false)`,
		`INSERT INTO metadata_items (id,kind,title,source)
SELECT 'series-' || n,'series','Series ' || n,'local' FROM generate_series(1,100) n`,
		`INSERT INTO metadata_items (id,kind,parent_id,title,source,season_num)
SELECT 'season-' || n,'season','series-' || n,'Season','local',1 FROM generate_series(1,100) n`,
		`INSERT INTO metadata_items (id,kind,parent_id,title,source,episode_num)
SELECT 'episode-' || s || '-' || n,'episode','season-' || s,'Episode','local',n
FROM generate_series(1,100) s CROSS JOIN generate_series(1,200) n`,
		`INSERT INTO media (id,library_id,metadata_id,path,created_at,updated_at)
SELECT 'old-' || n,'old-library','episode-1-1','/fixture/old/' || n,NOW(),NOW() FROM generate_series(1,10000) n`,
		`ANALYZE media`,
		`ANALYZE metadata_items`,
		`INSERT INTO media (id,library_id,metadata_id,path,created_at,updated_at)
SELECT 'new-' || s || '-' || n,'new-library','episode-' || s || '-' || n,
'/fixture/new/' || s || '/' || n,NOW(),NOW()
FROM generate_series(1,4) s CROSS JOIN generate_series(1,100) n`,
		`INSERT INTO media (id,library_id,path,scrape_status,created_at,updated_at)
SELECT 'pending-' || n,'new-library','/fixture/pending/' || n,'pending',NOW(),NOW() FROM generate_series(1,400) n`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	reads := &paginationReadLog{Interface: db.Logger}
	db.Logger = reads
	for _, filter := range []repository.MediaQueryFilter{
		{IncludeNSFW: true, MissingChineseTitle: true},
		{IncludeNSFW: true, MissingPoster: true},
		{IncludeNSFW: true, MissingPoster: true, MissingChineseTitle: true},
	} {
		_, cards, total, err := svc.repo.MediaView.ListLibraryMetadataPage(t.Context(), "new-library", "series", "", 0, 2, filter)
		if err != nil || total != 4 || len(cards) != 2 {
			t.Fatalf("filter=%+v cards=%+v total=%d err=%v", filter, cards, total, err)
		}
		for _, card := range cards {
			if card.Count != 100 || card.VersionCount != 100 {
				t.Fatalf("unexpected file counts: %+v", card)
			}
		}
		var raw string
		if err := db.Raw("EXPLAIN (ANALYZE, TIMING OFF, FORMAT JSON) " + reads.seriesSQL).Row().Scan(&raw); err != nil {
			t.Fatal(err)
		}
		type planNode struct {
			Relation string     `json:"Relation Name"`
			Rows     float64    `json:"Actual Rows"`
			Loops    float64    `json:"Actual Loops"`
			Estimate float64    `json:"Plan Rows"`
			Plans    []planNode `json:"Plans"`
		}
		var plans []struct{ Plan planNode }
		if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
			t.Fatalf("invalid plan: %v", err)
		}
		underestimated := false
		var check func(planNode)
		check = func(node planNode) {
			if node.Relation == "media" {
				underestimated = underestimated || node.Estimate < node.Rows
				if node.Loops > 1 {
					t.Errorf("new library files repeatedly scanned: %.0f loops", node.Loops)
				}
			}
			if node.Relation == "metadata_items" && node.Rows*node.Loops > 400 {
				t.Errorf("metadata traversal exceeds linked files: %.0f rows x %.0f loops", node.Rows, node.Loops)
			}
			for _, child := range node.Plans {
				check(child)
			}
		}
		check(plans[0].Plan)
		if !underestimated {
			t.Fatal("fixture did not reproduce stale library statistics")
		}
	}
}
