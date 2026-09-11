package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTMDbRecheckPassGroupsSeasonsAndExcludesLaterRetries(t *testing.T) {
	db := recheckQueueDB(t)
	repo := New(db).Metadata
	now, err := repo.TMDbRecheckPassBoundary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// 截止之后、当前时间之前到期的任务，也必须留给下一轮。
	cutoff := now.Add(-time.Hour)
	series := model.MetadataItem{Kind: "series", Source: "test"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	seasons := []model.MetadataItem{{Kind: "season", ParentID: &series.ID, SeasonNum: 1}, {Kind: "season", ParentID: &series.ID, SeasonNum: 2}}
	if err := db.Create(&seasons).Error; err != nil {
		t.Fatal(err)
	}
	episodes := []model.MetadataItem{
		{Kind: "episode", ParentID: &seasons[0].ID, EpisodeNum: 1},
		{Kind: "episode", ParentID: &seasons[1].ID, EpisodeNum: 1},
		{Kind: "episode", ParentID: &seasons[0].ID, EpisodeNum: 2},
	}
	if err := db.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	jobs := []model.TMDbRecheckJob{
		{MetadataID: seasons[0].ID, DueAt: ptrRecheckTime(cutoff.Add(-3 * time.Minute))},
		{MetadataID: episodes[1].ID, DueAt: ptrRecheckTime(cutoff.Add(-2 * time.Minute))},
		{MetadataID: episodes[0].ID, DueAt: ptrRecheckTime(cutoff.Add(-time.Minute))},
		{MetadataID: episodes[2].ID, DueAt: ptrRecheckTime(cutoff.Add(time.Minute))},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := repo.CountTMDbRechecksDue(t.Context(), cutoff); err != nil || count != 3 {
		t.Fatalf("initial count=%d err=%v", count, err)
	}
	for i, want := range seasons {
		lease, err := repo.ClaimTMDbRecheckSeason(t.Context(), cutoff)
		if err != nil || lease == nil || lease.MetadataID != want.ID {
			t.Fatalf("lease=%+v err=%v", lease, err)
		}
		page, err := repo.ClaimTMDbRecheckSeasonPage(t.Context(), lease, cutoff)
		if err != nil || len(page) != 2-i {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		for j := range page {
			if page[j].MetadataID == episodes[2].ID {
				t.Fatal("claimed future target")
			}
			if err := repo.FinishTMDbRecheck(t.Context(), &page[j], "retry", "", ptrRecheckTime(cutoff.Add(time.Minute)), 1); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.ReleaseTMDbRecheckSeason(t.Context(), lease); err != nil {
			t.Fatal(err)
		}
	}
	if job, err := repo.ClaimTMDbRecheckSeason(t.Context(), cutoff); err != nil || job != nil {
		t.Fatalf("same pass reclaimed a retry: %+v %v", job, err)
	}
	if count, err := repo.CountTMDbRechecksDue(t.Context(), cutoff); err != nil || count != 0 {
		t.Fatalf("remaining=%d err=%v", count, err)
	}
	if job, err := repo.ClaimTMDbRecheckSeason(t.Context(), now); err != nil || job == nil {
		t.Fatalf("next pass lost retry: %+v %v", job, err)
	}
}

func TestTMDbRecheckSeasonClaimDoesNotScanOtherJobs(t *testing.T) {
	db := recheckQueueDB(t)
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source,season_num,episode_num) VALUES ('plan-series','series','','test',0,0)`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num)
SELECT 'season-' || i::text,'season','','test','plan-series',i,0 FROM generate_series(0,999) i`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num) VALUES ('target-season','season','','test','plan-series',2000,0)`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num)
SELECT lpad(i::text,8,'0'),'episode','','test',CASE WHEN i>29970 THEN 'target-season' ELSE 'season-' || (i/30)::text END,0,i FROM generate_series(1,30000) i`,
		`INSERT INTO tm_db_recheck_jobs(metadata_id,due_at) SELECT lpad(i::text,8,'0'),clock_timestamp()-interval '1 day' FROM generate_series(1,30000) i`,
		`INSERT INTO tm_db_recheck_jobs(metadata_id,due_at) VALUES ('target-season',clock_timestamp()-interval '1 hour')`,
		`ANALYZE tm_db_recheck_jobs`,
		`ANALYZE metadata_items`,
		`SET plan_cache_mode = force_generic_plan`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var plan []struct {
		Line string `gorm:"column:QUERY PLAN"`
	}
	query := tmdbRecheckSeasonPageSQL
	for _, parameter := range []string{"$1", "$2", "$3"} {
		query = strings.Replace(query, "?", parameter, 1)
	}
	if err := db.Exec("PREPARE recheck_season_plan(text,text,timestamptz) AS " + query).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) EXECUTE recheck_season_plan('target-season','target-season',statement_timestamp())").Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, row := range plan {
		lines = append(lines, row.Line)
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "tm_db_recheck_jobs_pkey") || strings.Contains(text, "Seq Scan on tm_db_recheck_jobs") || strings.Contains(text, "idx_tmdb_recheck_due") {
		t.Fatal(text)
	}
	t.Log(text)
	query = strings.Replace(tmdbRecheckSeasonLookupSQL, "?", "$1", 1)
	if err := db.Exec("PREPARE recheck_season_lookup(timestamptz) AS " + query).Error; err != nil {
		t.Fatal(err)
	}
	plan = nil
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) EXECUTE recheck_season_lookup(statement_timestamp())").Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	lines = nil
	for _, row := range plan {
		lines = append(lines, row.Line)
	}
	text = strings.Join(lines, "\n")
	if !strings.Contains(text, "idx_tmdb_recheck_due") || strings.Contains(text, "Seq Scan on tm_db_recheck_jobs") {
		t.Fatal(text)
	}
	t.Log(text)
}
