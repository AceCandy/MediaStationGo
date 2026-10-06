package database

import (
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestTaskPageIndexesSupportGenericPlans(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.MetadataItem{}, &model.MetadataCredit{}, &model.Media{}, &model.Favorite{}, &model.PlaybackHistory{}, &model.PlayProfile{}, &model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Library{Base: model.Base{ID: "library"}, Name: "Test", Type: "movie"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO task_executions(id,kind,name,status,trigger,started_at)
 SELECT 'task-'||i, CASE WHEN i=1 THEN 'scrape' WHEN i<=30000 THEN 'artwork' ELSE 'watch' END,
 CASE WHEN i=1 THEN '发现目录刮削：测试' WHEN i=2 THEN 'TMDb 无图复查' ELSE 'other-'||(i % 100) END,
 CASE WHEN i % 20 = 0 THEN 'failed' ELSE 'completed' END,
 'manual', timestamp '2026-01-01' + i * interval '1 second' FROM generate_series(1,60000) i`,
		`INSERT INTO media(id,path,library_id,scrape_status)
 SELECT 'file-'||i, '/test/'||i, 'library', CASE WHEN i<=2 THEN 'error' ELSE 'matched' END
 FROM generate_series(1,60000) i`,
		`INSERT INTO metadata_items(id,kind,title,source,season_num,episode_num) VALUES ('series','series','Test','tmdb',0,0)`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num) VALUES ('season','season','Test','tmdb','series',1,0)`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num)
 SELECT 'metadata-'||i, CASE WHEN i<=1000 THEN 'episode' ELSE 'movie' END, repeat('Title',50), 'tmdb',
 CASE WHEN i<=1000 THEN 'season' ELSE NULL END, 0, CASE WHEN i<=1000 THEN i ELSE 0 END
 FROM generate_series(1,60000) i`,
		`VACUUM ANALYZE task_executions`,
		`VACUUM ANALYZE media`,
		`VACUUM ANALYZE metadata_items`,
		`ANALYZE libraries`,
		`SET plan_cache_mode = force_generic_plan`,
		`PREPARE latest_kind(text,text) AS SELECT * FROM task_executions
 WHERE kind=$1 AND status<>$2 AND deleted_at IS NULL
 ORDER BY started_at DESC, id DESC LIMIT 1`,
		`PREPARE latest_name(text,text,text) AS SELECT * FROM task_executions
 WHERE kind=$1 AND name=$2 AND status<>$3 AND deleted_at IS NULL
 ORDER BY started_at DESC, id DESC LIMIT 1`,
		`PREPARE scrape_count AS SELECT count(*) FROM media m
 JOIN libraries l ON l.id=m.library_id AND l.deleted_at IS NULL
 WHERE m.scrape_status IN ('error','no_match')`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ query, index string }{
		{`EXECUTE latest_kind('artwork','running')`, "idx_task_executions_kind_latest"},
		{`EXECUTE latest_name('artwork','TMDb 无图复查','running')`, "idx_task_executions_kind_name_latest"},
		{`EXECUTE scrape_count`, "idx_media_scrape_issues"},
		{`SELECT id FROM metadata_items WHERE kind='episode'`, "Index Only Scan using idx_metadata_recheck_kind_id"},
	} {
		var lines []string
		if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) " + tc.query).Scan(&lines).Error; err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(lines, "\n")
		if !strings.Contains(plan, tc.index) || strings.Contains(plan, "Seq Scan on media") || strings.Contains(plan, "Seq Scan on task_executions") || strings.Contains(plan, "Rows Removed by Filter") {
			t.Fatalf("query failed to isolate candidates with %s:\n%s", tc.index, plan)
		}
		t.Logf("%s:\n%s", tc.query, plan)
	}
}
