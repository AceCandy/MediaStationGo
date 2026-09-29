package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type seriesPageReadLog struct {
	logger.Interface
	queries       []string
	playedQueries []string
}

func (l *seriesPageReadLog) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.Contains(sql, "BOOL_AND") {
		l.playedQueries = append(l.playedQueries, sql)
	}
	if strings.Contains(sql, "AS scoped_series") || strings.Contains(sql, "AS series_id") || strings.Contains(sql, "ARRAY_AGG(m.id") || strings.Contains(sql, "FROM metadata_items recent") || strings.HasPrefix(sql, "WITH scoped AS MATERIALIZED") || strings.HasPrefix(sql, "WITH work_candidates AS MATERIALIZED") || strings.HasPrefix(sql, "WITH work_batch AS MATERIALIZED") {
		l.queries = append(l.queries, sql)
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func TestEmbyLatestCandidatePlans(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	for _, sql := range []string{
		`INSERT INTO metadata_items (id,kind,title,source) SELECT 'show-'||n,'series','Show','local' FROM generate_series(1,500) n`,
		`INSERT INTO metadata_items (id,kind,title,source,parent_id,season_num) SELECT 'season-'||s||'-'||n,'season','Season','local','show-'||s,n FROM generate_series(1,500) s CROSS JOIN generate_series(1,2) n`,
		`INSERT INTO metadata_items (id,kind,title,source,parent_id,episode_num) SELECT 'ep-'||s||'-'||n||'-'||e,'episode','Episode','local','season-'||s||'-'||n,e FROM generate_series(1,500) s CROSS JOIN generate_series(1,2) n CROSS JOIN generate_series(1,20) e`,
		`INSERT INTO metadata_items (id,kind,title,source) SELECT 'movie-'||n,'movie','Movie','local' FROM generate_series(1,500) n`,
		`INSERT INTO media (id,library_id,path,metadata_id,season_num,episode_num,created_at)
SELECT 'file-'||i.id||'-'||v,'tv','/fixture/'||i.id||'-'||v,i.id,1,i.episode_num,TIMESTAMP '2026-01-01' + i.episode_num * INTERVAL '1 hour'
FROM metadata_items i CROSS JOIN generate_series(1,2) v WHERE i.kind='episode'`,
		`INSERT INTO media (id,library_id,path,metadata_id,created_at)
SELECT 'file-'||i.id||'-'||v,'movie','/fixture/'||i.id||'-'||v,i.id,TIMESTAMP '2026-02-01' + v * INTERVAL '1 hour'
FROM metadata_items i CROSS JOIN generate_series(1,10) v WHERE i.kind='movie' AND i.id LIKE 'movie-%'`,
		`INSERT INTO playback_histories (id,user_id,metadata_id,media_id,completed)
SELECT 'state-'||id,'viewer',id,'file-'||id||'-1',TRUE FROM metadata_items WHERE kind='episode' AND episode_num % 2=0 OR kind='movie' AND id ~ '^movie-[0-9]*[02468]$'`,
		`UPDATE media SET library_id='small-movie' WHERE metadata_id ~ '^movie-9[0-9]$' AND id LIKE '%-1'`,
		`UPDATE media SET library_id='small-tv' WHERE metadata_id ~ '^ep-9[0-9]-1-[12]$' AND id LIKE '%-1'`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var unknown int64
	if err := db.Table("metadata_items").Where("latest_media_added_at IS NOT NULL AND library_ids IS NULL").Count(&unknown).Error; err != nil || unknown != 0 {
		t.Fatalf("file-backed fixture has unknown membership: %d, err=%v", unknown, err)
	}
	for _, table := range []string{"media", "metadata_items", "playback_histories"} {
		if err := db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	capture := func(tx *gorm.DB) {
		if !tx.DryRun && strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") {
			queries = append(queries, statement{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
		}
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:latest-candidates", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:latest-candidates", capture); err != nil {
		t.Fatal(err)
	}
	for _, movieDate := range []string{"2026-02-01", "2025-12-01"} {
		// 同时覆盖目标库位于时间索引前端和远落后于其它库；后者不能逐个探测其它库分集。
		if err := db.Exec("UPDATE media SET created_at = ?::timestamp WHERE library_id = 'movie'", movieDate).Error; err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"movie", "tv", "small-movie", "small-tv"} {
			for _, user := range []string{"empty-history", "viewer"} {
				for _, played := range []bool{false, true} {
					queries = nil
					q := svc.applyUserMediaVisibility(t.Context(), db.Model(&model.Media{}).Where("media.library_id = ?", kind), user)
					q = svc.applyLatestPlayedFilter(t.Context(), q, user, played)
					var count int
					var firstID string
					ids := []string{}
					if strings.HasSuffix(kind, "movie") {
						items, err := svc.latestMetadataViews(t.Context(), q, user, []string{kind}, 3)
						if err != nil {
							t.Fatal(err)
						}
						count = len(items)
						for _, item := range items {
							ids = append(ids, item.MetadataID)
						}
						if count > 0 {
							firstID = items[0].MetadataID
						}
					} else {
						groups, err := svc.latestSeriesGroups(t.Context(), q, []string{kind}, 3)
						if err != nil {
							t.Fatal(err)
						}
						count = len(groups)
						for _, group := range groups {
							ids = append(ids, group.ID)
						}
						if count > 0 {
							firstID = groups[0].ID
						}
					}
					want := 3
					if user == "empty-history" && played {
						want = 0
					}
					if count != want {
						t.Fatalf("%s user=%s played=%v items=%d want=%d", kind, user, played, count, want)
					}
					wantID := "show-99"
					if strings.HasSuffix(kind, "movie") {
						wantID = "movie-99"
						if played {
							wantID = "movie-98"
						}
					}
					if count > 0 && firstID != wantID {
						t.Fatalf("%s user=%s played=%v first=%s want=%s", kind, user, played, firstID, wantID)
					}
					if len(queries) == 0 || count > 0 && len(queries) != 1 {
						t.Fatalf("candidate queries=%d", len(queries))
					}
					candidates := append([]statement(nil), queries...)
					old := db.Table("metadata_items recent").Where("recent.latest_media_added_at IS NOT NULL")
					if strings.HasSuffix(kind, "movie") {
						old = old.Where("EXISTS (?)", q.Session(&gorm.Session{}).Select("1").Where("media.metadata_id=recent.id"))
					} else {
						old = old.Where("recent.kind='series'").Where("EXISTS (? OFFSET 0)", seriesScopeQuery(q.Session(&gorm.Session{})).Select("1").Where("scope_series.id=recent.id"))
					}
					wantIDs := []string{}
					if err := old.Order("recent.latest_media_added_at DESC NULLS LAST, recent.id DESC").Limit(3).Pluck("recent.id", &wantIDs).Error; err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(ids, wantIDs) {
						t.Fatalf("%s user=%s played=%v ids=%v old=%v", kind, user, played, ids, wantIDs)
					}
					for _, candidate := range candidates {
						var raw []byte
						// 已编译的 PostgreSQL 占位符直接交给驱动；GORM Raw 会把 @> 当作命名参数 SQL。
						if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+candidate.sql, candidate.vars...).Scan(&raw); err != nil {
							t.Fatal(err)
						}
						type planNode struct {
							Subplan  string     `json:"Subplan Name"`
							Relation string     `json:"Relation Name"`
							Alias    string     `json:"Alias"`
							Index    string     `json:"Index Name"`
							Type     string     `json:"Node Type"`
							Rows     float64    `json:"Actual Rows"`
							Removed  float64    `json:"Rows Removed by Filter"`
							Loops    float64    `json:"Actual Loops"`
							Plans    []planNode `json:"Plans"`
						}
						var plans []struct {
							Plan          planNode
							ExecutionTime float64 `json:"Execution Time"`
						}
						if err := json.Unmarshal(raw, &plans); err != nil {
							t.Fatal(err)
						}
						candidateNodes := 0
						candidateRows := 0.0
						var inspect func(planNode, bool)
						inspect = func(n planNode, candidateScope bool) {
							candidateScope = candidateScope || n.Subplan == "CTE work_batch"
							candidateScan := candidateScope && n.Relation == "metadata_items"
							if candidateScan {
								candidateNodes++
								candidateRows += (n.Rows + n.Removed) * n.Loops
							}
							if candidateScan && user == "empty-history" && !played {
								t.Logf("%s membership %s index=%s visited=%.0f", kind, n.Type, n.Index, (n.Rows+n.Removed)*n.Loops)
							}
							// 无已看记录时允许穷尽候选，但不能整表加载文件；命中页则应提前结束。
							maxRows := 1500.0
							if user == "empty-history" && played {
								maxRows = 45000
							}
							// 库归属已持久化，不能再枚举全库文件提取成员身份。
							if n.Relation == "media" && n.Alias == "member" {
								t.Errorf("unexpected library membership scan: %+v", n)
							} else if n.Relation == "media" && ((n.Rows+n.Removed)*n.Loops > maxRows || n.Loops > maxRows) {
								t.Errorf("%s user=%s played=%v candidate expanded unrelated files: rows=%v loops=%v", kind, user, played, n.Rows+n.Removed, n.Loops)
							}
							for _, child := range n.Plans {
								inspect(child, candidateScope)
							}
						}
						inspect(plans[0].Plan, false)
						if candidateNodes == 0 {
							t.Fatal("candidate metadata scan not inspected")
						}
						if candidateRows > 1500 {
							t.Errorf("%s candidate scanned unrelated metadata: %.0f", kind, candidateRows)
						}
						t.Logf("%s user=%s played=%v %.3f ms", kind, user, played, plans[0].ExecutionTime)
					}
				}
			}
		}
	}
	// 在已有四万余文件的库上测增量写入，回滚诊断事务，不把样本耗时当生产 SLA。
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	for i, sql := range []string{
		`INSERT INTO media(id,path,metadata_id,library_id,created_at) VALUES ('write-cost','/fixture/write-cost','ep-99-1-1','write-cost',now())`,
		`UPDATE media SET library_id='moved-cost' WHERE id='write-cost'`,
		`DELETE FROM media WHERE id='write-cost'`,
	} {
		var raw []byte
		if err := tx.Raw("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) " + sql).Row().Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plans []struct {
			ExecutionTime float64 `json:"Execution Time"`
		}
		if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
			t.Fatalf("write plan: %v", err)
		}
		var libraries string
		if err := tx.Raw("SELECT library_ids FROM metadata_items WHERE id='show-99'").Scan(&libraries).Error; err != nil {
			t.Fatal(err)
		}
		if strings.Contains(libraries, "write-cost") != (i == 0) || strings.Contains(libraries, "moved-cost") != (i == 1) {
			t.Fatalf("write step=%d lost membership: %s", i, libraries)
		}
		t.Logf("incremental write step=%d %.3f ms", i, plans[0].ExecutionTime)
	}
}

// 密集库的准确总数只需为每部剧找到一个合格文件，不能展开所有版本。
func TestEmbySeriesDenseCountPlan(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'show-'||n,'series','Show','local' FROM generate_series(1,502) n`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id) SELECT 'season-'||n,'season','Season','local','show-'||n FROM generate_series(1,502) n`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,episode_num) SELECT 'ep-'||s||'-'||e,'episode','Episode','local','season-'||s,e FROM generate_series(1,502) s CROSS JOIN generate_series(1,40) e`,
		`INSERT INTO media(id,library_id,path,metadata_id,season_num,episode_num,created_at)
SELECT 'file-'||s||'-'||e||'-'||v,'tv','/fixture/'||s||'-'||e||'-'||v,'ep-'||s||'-'||e,1,e,TIMESTAMP '2026-01-01'
FROM generate_series(1,500) s CROSS JOIN generate_series(1,40) e CROSS JOIN generate_series(1,2) v`,
		`INSERT INTO media(id,library_id,path,metadata_id,season_num,episode_num,created_at) VALUES
('direct-show','tv','/fixture/direct-show','show-501',1,1,now()),
('direct-season','tv','/fixture/direct-season','season-502',1,1,now()),
('invalid-episode','tv','/fixture/invalid-episode','ep-502-1',0,0,now())`,
		`ANALYZE metadata_items`, `ANALYZE media`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var countSQL string
	var countVars []any
	if err := db.Callback().Row().After("gorm:row").Register("test:dense-count", func(tx *gorm.DB) {
		if sql := tx.Statement.SQL.String(); !tx.DryRun && strings.HasPrefix(sql, "WITH work_batch AS MATERIALIZED") && strings.Contains(sql, "SELECT COUNT(*) FROM") {
			countSQL, countVars = sql, append([]any(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	p := ItemsParams{UserID: "viewer", ParentID: "tv", SortBy: "DateLastContentAdded", SortOrder: "Descending", Limit: 3}
	files := svc.applyUserMediaVisibility(t.Context(), db.Model(&model.Media{}).
		Where("media.library_id='tv'").Where("media.season_num > 0 OR media.episode_num > 0"), p.UserID)
	got, total, err := svc.seriesWorkPage(t.Context(), files, p, 0, 3)
	if err != nil || total != 500 || len(got) != 3 {
		t.Fatalf("dense page: groups=%v total=%d err=%v", got, total, err)
	}
	if countSQL == "" {
		t.Fatal("count query not captured")
	}
	var raw []byte
	if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+countSQL, countVars...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type planNode struct {
		Relation string     `json:"Relation Name"`
		Rows     float64    `json:"Actual Rows"`
		Removed  float64    `json:"Rows Removed by Filter"`
		Loops    float64    `json:"Actual Loops"`
		Plans    []planNode `json:"Plans"`
	}
	var plans []struct {
		Plan          planNode
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("invalid plan: %v", err)
	}
	visited := map[string]float64{}
	var inspect func(planNode)
	inspect = func(n planNode) {
		if n.Relation != "" {
			visited[n.Relation] += (n.Rows + n.Removed) * n.Loops
		}
		if (n.Relation == "media" || n.Relation == "metadata_items") && ((n.Rows+n.Removed)*n.Loops > 5000 || n.Loops > 5000) {
			t.Errorf("dense count expanded files/catalog: relation=%s rows=%v removed=%v loops=%v", n.Relation, n.Rows, n.Removed, n.Loops)
		}
		for _, child := range n.Plans {
			inspect(child)
		}
	}
	inspect(plans[0].Plan)
	for _, table := range []string{"media", "metadata_items"} {
		if rows, ok := visited[table]; !ok || rows == 0 || rows > 5000 {
			t.Errorf("dense count %s total visits=%v inspected=%v", table, rows, ok)
		}
	}
	t.Logf("dense count %.3f ms", plans[0].ExecutionTime)
	// 未初始化汇总必须与已知作品一起精确计数，直接绑定整剧/季的文件不能冒充有效分集。
	if err := db.Exec("UPDATE metadata_items SET latest_media_added_at=NULL, library_ids=NULL WHERE id IN ('show-1','show-2','show-501')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO playback_histories(id,user_id,metadata_id,media_id,completed)
VALUES ('played-episode','viewer','ep-1-1','file-1-1-1',true)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, played := range []bool{false, true} {
		q := svc.applyLatestPlayedFilter(t.Context(), files.Session(&gorm.Session{}), p.UserID, played)
		want, wantTotal, err := svc.originalSeriesMetadataPageWithCount(t.Context(), q, p.UserID, p, 0, 3, true)
		if err != nil {
			t.Fatal(err)
		}
		got, total, err = svc.seriesWorkPage(t.Context(), q, p, 0, 3)
		if err != nil || total != wantTotal || played && total != 1 || !reflect.DeepEqual(got, want) {
			t.Fatalf("dense oracle played=%v: got=%v want=%v totals=%d/%d err=%v", played, got, want, total, wantTotal, err)
		}
	}
}

func TestEmbySeriesPaginationDoesNotProbeFilesForWholeCatalog(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	lib := model.Library{Name: "Series", Path: "/fixture/series", Type: "tv"}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	// 目录包含 2 万集，只有其中 20 部剧的前 100 集各有两个文件版本。
	for _, sql := range []string{
		`INSERT INTO metadata_items (id, kind, title, source, season_num, episode_num)
		 SELECT 'series-' || n, 'series', LPAD(n::text, 3, '0'), 'local', 0, 0 FROM generate_series(1,100) n`,
		`INSERT INTO metadata_items (id, kind, parent_id, title, source, season_num, episode_num)
		 SELECT 'season-' || n, 'season', 'series-' || n, 'Season', 'local', 1, 0 FROM generate_series(1,100) n`,
		`INSERT INTO metadata_items (id, kind, parent_id, title, source, season_num, episode_num)
		 SELECT 'episode-' || s || '-' || n, 'episode', 'season-' || s, 'Episode', 'local', 0, n
		 FROM generate_series(1,100) s CROSS JOIN generate_series(1,200) n`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, metadata_id, path, season_num, episode_num, created_at, updated_at)
		SELECT 'file-' || s || '-' || n || '-' || v, ?, 'episode-' || s || '-' || n,
		'/fixture/' || s || '/' || n || '-' || v || '.mkv', 1, n, NOW(), NOW()
		FROM generate_series(1,20) s CROSS JOIN generate_series(1,100) n CROSS JOIN generate_series(1,2) v`, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"media", "metadata_items"} {
		if err := db.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	reads := &seriesPageReadLog{Interface: db.Logger}
	db.Logger = reads
	var plansSQL []struct {
		sql  string
		vars []any
	}
	capture := func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if !tx.DryRun && (strings.HasPrefix(sql, "WITH work_batch AS MATERIALIZED") || strings.HasPrefix(sql, "WITH work_candidates AS MATERIALIZED") || strings.HasPrefix(sql, "WITH scoped AS MATERIALIZED") || strings.Contains(sql, "BOOL_AND")) {
			plansSQL = append(plansSQL, struct {
				sql  string
				vars []any
			}{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:series-page-plan", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:series-page-plan", capture); err != nil {
		t.Fatal(err)
	}
	p := ItemsParams{UserID: "page-user", ParentID: lib.ID, IncludeItemTypes: []string{"Series"}, Recursive: true,
		SortBy: "DateLastContentAdded,SortName", SortOrder: "Descending", Limit: 3, Fields: []string{"BasicSyncInfo"}}
	result, err := svc.Items(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	items := result["Items"].([]map[string]any)
	if result["TotalRecordCount"] != 20 || len(items) != 3 || items[0]["Id"] != "series-9" || items[2]["Id"] != "series-7" || items[0]["RecursiveItemCount"] != 100 || items[0]["ChildCount"] != 1 {
		t.Fatalf("unexpected series page: %#v", result)
	}
	queries := append([]string(nil), reads.queries...)
	if len(queries) != 2 {
		t.Fatalf("count/page queries = %d", len(queries))
	}
	for _, query := range queries {
		if !strings.Contains(query, "WITH work_batch AS MATERIALIZED") || strings.Contains(query, "MAX(media.created_at)") {
			t.Fatalf("latest series candidates must use work time and file existence: %s", query)
		}
	}
	if len(reads.playedQueries) != 1 {
		t.Fatalf("played summary queries = %d, want one per page", len(reads.playedQueries))
	}
	queries = append(queries, reads.playedQueries...)
	reads.queries = nil
	web := NewMediaService(svc.cfg, svc.log, svc.repo)
	cards, total, err := web.ListLibrarySeriesCards(t.Context(), lib.ID, 1, 3, "", "", MediaVisibility{})
	if err != nil || total != 20 || len(cards) != 3 || cards[0].Count != 100 {
		t.Fatalf("unexpected web page: cards=%d total=%d err=%v", len(cards), total, err)
	}
	if len(reads.queries) != 1 {
		t.Fatalf("web page queries = %d", len(reads.queries))
	}
	queries = append(queries, reads.queries...)
	// 验证实际执行计划的文件探测次数，不用受机器负载影响的毫秒阈值。
	type planNode struct {
		Relation string     `json:"Relation Name"`
		Loops    float64    `json:"Actual Loops"`
		Plans    []planNode `json:"Plans"`
	}
	var check func(planNode)
	stage := ""
	check = func(node planNode) {
		maxLoops := 4000.0
		if stage == "batch" {
			// 50 个原始候选可能包含尚未初始化归属的空作品；最多检查本批 50×200 集，不能遍历整个 2 万集目录。
			maxLoops = 50 * 200
		}
		if node.Relation == "media" && node.Loops > maxLoops {
			t.Errorf("%s file probes expanded to catalog size: %.0f", stage, node.Loops)
		}
		for _, child := range node.Plans {
			check(child)
		}
	}
	for _, query := range plansSQL {
		stage = "details"
		if strings.Contains(query.sql, "LEFT JOIN qualified") {
			stage = "batch"
		} else if strings.Contains(query.sql, "SELECT COUNT(*) FROM") {
			stage = "count"
		}
		var raw string
		if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+query.sql, query.vars...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plans []struct{ Plan planNode }
		if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
			t.Fatalf("invalid execution plan: %v", err)
		}
		check(plans[0].Plan)
	}
	for _, sortBy := range []string{"SortName", "DateCreated", "DateLastContentAdded", "PremiereDate"} {
		p.SortBy, p.SortOrder, p.StartIndex = sortBy, "Ascending", 1
		result, err = svc.Items(t.Context(), p)
		if err != nil || result["TotalRecordCount"] != 20 || len(result["Items"].([]map[string]any)) != 3 {
			t.Fatalf("sort %s page changed: %#v, %v", sortBy, result, err)
		}
		if sortBy == "SortName" && result["Items"].([]map[string]any)[0]["Id"] != "series-2" {
			t.Fatalf("name sorting or offset changed: %#v", result)
		}
		q := svc.applyUserMediaVisibility(t.Context(), db.Model(&model.Media{}).Where("media.library_id = ?", lib.ID), p.UserID)
		want, wantTotal, err := svc.originalSeriesMetadataPageWithCount(t.Context(), q, p.UserID, p, p.StartIndex, p.Limit, true)
		if err != nil {
			t.Fatal(err)
		}
		got, gotTotal, err := svc.seriesWorkPage(t.Context(), q, p, p.StartIndex, p.Limit)
		if err != nil || wantTotal != gotTotal || !reflect.DeepEqual(got, want) {
			t.Fatalf("series oracle sort=%s: got=%+v want=%+v totals=%d/%d err=%v", sortBy, got, want, gotTotal, wantTotal, err)
		}
	}
	// 同季多个版本取最新时间；聚合压缩不能改成最早时间。
	if err := db.Exec("UPDATE media SET created_at = '2099-01-01' WHERE id = 'file-1-1-2'").Error; err != nil {
		t.Fatal(err)
	}
	p.SortBy, p.SortOrder, p.StartIndex = "DateCreated", "Descending", 0
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["Items"].([]map[string]any)[0]["Id"] != "series-1" {
		t.Fatalf("latest file timestamp lost: %#v, %v", result, err)
	}
	p.StartIndex = 20
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 20 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("empty page changed: %#v, %v", result, err)
	}
	// 同一个过滤 scope 必须同时约束计数、分页和当前页摘要。
	for _, row := range []any{
		&model.Person{Base: model.Base{ID: "page-person"}, Name: "Actor", Source: "local"},
		&model.MetadataCredit{MetadataID: "series-20", PersonID: "page-person", Type: model.CreditTypeActor},
		&model.Favorite{UserID: "page-user", MetadataID: "series-20"},
		&model.PlaybackHistory{UserID: "page-user", MetadataID: "episode-20-1", MediaID: "file-20-1-1", Completed: true},
		&model.Media{LibraryID: "hidden", MetadataID: "episode-20-101", Path: "/fixture/hidden.mkv", SeasonNum: 1, EpisodeNum: 101},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.Media{}).Where("metadata_id = ?", "episode-20-100").Update("library_id", "hidden").Error; err != nil {
		t.Fatal(err)
	}
	svc.visibilityCache[svc.repo.ReadCacheKey()+"page-user"] = embyVisibilityCacheEntry{
		visibility: MediaVisibility{AllowedLibraryIDs: []string{lib.ID}, HiddenLibraryIDs: []string{"hidden"}},
		expiresAt:  time.Now().Add(time.Minute),
	}
	p.UserID, p.ParentID, p.StartIndex = "page-user", "", 0
	p.Filters = []string{"IsFavorite"}
	reads.queries = nil
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 1 || len(result["Items"].([]map[string]any)) != 1 || result["Items"].([]map[string]any)[0]["RecursiveItemCount"] != 99 {
		t.Fatalf("favorite-only page changed: %#v, %v", result, err)
	}
	favoriteQueries := append([]string(nil), reads.queries...)
	if len(favoriteQueries) != 2 {
		t.Fatalf("favorite count/page queries = %d", len(favoriteQueries))
	}
	for _, query := range favoriteQueries {
		if !strings.Contains(query, "WITH work_batch AS MATERIALIZED") || !strings.Contains(query, "favorites.metadata_id = scope_series.id") {
			t.Fatalf("favorite filter must constrain work candidates: %s", query)
		}
	}
	p.StartIndex = 1
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 1 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("favorite empty page changed: %#v, %v", result, err)
	}
	p.StartIndex, p.UserID = 0, "other-user"
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 0 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("favorites leaked between users: %#v, %v", result, err)
	}
	p.UserID = "page-user"
	p.PersonIDs, p.Filters = []string{"page-person"}, []string{"IsFavorite"}
	result, err = svc.Items(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	items = result["Items"].([]map[string]any)
	if result["TotalRecordCount"] != 1 || len(items) != 1 || items[0]["Id"] != "series-20" || items[0]["RecursiveItemCount"] != 99 {
		t.Fatalf("filtered page changed: %#v", result)
	}
	p.PersonIDs = []string{"missing-person"}
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 0 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("person filter ignored: %#v, %v", result, err)
	}
	p.PersonIDs = []string{"page-person"}
	if err := db.Where("user_id = ?", "page-user").Delete(&model.Favorite{}).Error; err != nil {
		t.Fatal(err)
	}
	result, err = svc.Items(t.Context(), p)
	if err != nil || result["TotalRecordCount"] != 0 || len(result["Items"].([]map[string]any)) != 0 {
		t.Fatalf("deleted favorite included: %#v, %v", result, err)
	}
	items, err = svc.LatestItems(t.Context(), "page-user", lib.ID, 3, true)
	if err != nil || len(items) != 1 || items[0]["Id"] != "series-20" || items[0]["RecursiveItemCount"] != 1 {
		t.Fatalf("played series changed: %#v, %v", items, err)
	}
}
