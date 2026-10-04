package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
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
	captured := captureLibrarySeriesSQL(t, db)
	for _, filter := range []repository.MediaQueryFilter{
		{MissingChineseTitle: true},
		{MissingPoster: true},
		{MissingPoster: true, MissingChineseTitle: true},
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
		query, vars := captured()
		var raw string
		if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, TIMING OFF, FORMAT JSON) "+query, vars...).Scan(&raw); err != nil {
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

func TestLibrarySeriesWorkFiltersBeforeFiles(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	for _, query := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'work-first-'||n,'series','Show '||n,'local' FROM generate_series(1,4) n`,
		`INSERT INTO metadata_items(id,kind,parent_id,title,source,season_num) SELECT 'work-season-'||n,'season','work-first-'||n,'Season','local',1 FROM generate_series(1,4) n`,
		`INSERT INTO metadata_items(id,kind,parent_id,title,source,episode_num) SELECT 'work-episode-'||s||'-'||n,'episode','work-season-'||s,'Episode','local',n FROM generate_series(1,4) s CROSS JOIN generate_series(1,100) n`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'work-file-'||s||'-'||n||'-'||v,'work-episode-'||s||'-'||n,'work-library','/fixture/work/'||s||'/'||n||'/'||v,NOW() FROM generate_series(1,4) s CROSS JOIN generate_series(1,100) n CROSS JOIN generate_series(1,3) v`,
		`UPDATE metadata_items SET library_ids=NULL WHERE id='work-first-2'`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	captured := captureLibrarySeriesSQL(t, db)
	for n := 1; n <= 4; n++ {
		createServiceTestArtwork(t, db, fmt.Sprintf("work-first-%d", n), model.ArtworkTypePoster, fmt.Sprintf("work-poster-%d", n))
		_, cards, total, err := svc.repo.MediaView.ListLibraryMetadataPage(t.Context(), "work-library", "series", "", 0, 50, repository.MediaQueryFilter{MissingPoster: true})
		if err != nil || total != int64(4-n) || len(cards) != 4-n {
			t.Fatalf("posters=%d cards=%+v total=%d err=%v", n, cards, total, err)
		}
		for _, card := range cards {
			if card.Count != 100 || card.VersionCount != 300 {
				t.Fatalf("versions lost: %+v", card)
			}
		}
		query, vars := captured()
		var raw []byte
		if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE,FORMAT JSON,TIMING OFF) "+query, vars...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		type node struct {
			Relation string  `json:"Relation Name"`
			Loops    float64 `json:"Actual Loops"`
			Plans    []node  `json:"Plans"`
		}
		var plans []struct{ Plan node }
		if err := json.Unmarshal(raw, &plans); err != nil {
			t.Fatal(err)
		}
		seen := false
		var check func(node)
		check = func(p node) {
			if p.Relation == "metadata_artworks" || p.Relation == "artwork_assets" {
				seen = true
				if p.Loops > 4 {
					t.Errorf("poster checked per file instead of work: %s loops=%.0f", p.Relation, p.Loops)
				}
			}
			if n == 4 && p.Relation == "media" && p.Loops != 0 {
				t.Errorf("empty work filter still reads files: %.0f loops", p.Loops)
			}
			for _, child := range p.Plans {
				check(child)
			}
		}
		check(plans[0].Plan)
		if !seen {
			t.Fatal("poster plan was not inspected")
		}
	}
	for _, filter := range []repository.MediaQueryFilter{{AllowedLibraryIDs: []string{"other"}}, {HiddenLibraryIDs: []string{"work-library"}}} {
		_, cards, total, err := svc.repo.MediaView.ListLibraryMetadataPage(t.Context(), "work-library", "series", "", 0, 50, filter)
		if err != nil || total != 0 || len(cards) != 0 {
			t.Fatalf("unknown membership leaked: cards=%+v total=%d err=%v", cards, total, err)
		}
	}
}
