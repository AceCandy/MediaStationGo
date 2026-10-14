package repository

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/gorm"
)

func TestMetadataSearchPeopleScopeAndPlan(t *testing.T) {
	repos := newMetadataSearchTestRepositories(t)
	db := repos.DB
	// 无关来源和人物占绝大多数，防止小夹具掩盖全库姓名关联。
	for _, sql := range []string{
		`INSERT INTO people(id,name,original_name,normalized_name,source) SELECT 'p'||n,'Actor '||n,'Actor '||n,'actor '||n,'tmdb' FROM generate_series(1,20000) n`,
		`INSERT INTO tmdb_person_profiles(id,tmdb_id,person_id,name,original_name) SELECT 'p'||n,n,'p'||n,'Actor '||n,'Actor '||n FROM generate_series(1,20000) n`,
		`INSERT INTO tmdb_subjects(id,kind,tmdb_id) SELECT 's'||n,'movie',n FROM generate_series(1,20000) n`,
		`INSERT INTO tmdb_credits(id,subject_id,person_profile_id,origin,credit_key,category) SELECT 'c'||n,'s'||n,'p'||n,'subject','cast','cast' FROM generate_series(1,20000) n`,
		`INSERT INTO metadata_items(id,kind,source) VALUES ('movie','movie','tmdb'),('series','series','tmdb'),('ambiguous','movie','tmdb'),('invalid','movie','tmdb')`,
		`INSERT INTO metadata_items(id,kind,parent_id,season_num,source) VALUES ('season','season','series',1,'tmdb')`,
		`INSERT INTO tmdb_subjects(id,kind,tmdb_id,parent_id,number) VALUES ('series','series',1,NULL,NULL),('season','season',1,'series',1)`,
		`INSERT INTO metadata_identifiers(id,metadata_id,provider,entity_kind,external_id) VALUES
   ('movie','movie','tmdb','movie','1'),('series','series','tmdb','series','1'),('season','season','tmdb','season','1'),
   ('a1','ambiguous','tmdb','movie','2'),('a2','ambiguous','tmdb','movie','3'),('invalid','invalid','tmdb','movie','not-numeric')`,
		`INSERT INTO tmdb_credits(id,subject_id,person_profile_id,origin,credit_key,category,job,department) VALUES
   ('duplicate','s1','p1','subject','second-role','cast','',''),
   ('series-actor','series','p2','subject','cast','cast','',''),
   ('season-director','season','p3','subject','director','crew','Director',''),
   ('season-writer','season','p4','subject','writer','crew','','Writing'),
   ('season-staff','season','p5','subject','staff','crew','Producer','Production'),
   ('filmography','season','p6','person','actor','cast','',''),
   ('deleted','season','p7','subject','actor','cast','','')`,
		`UPDATE people SET deleted_at=now() WHERE id='p7'`,
		`INSERT INTO translation_caches(id,kind,context_key,source_text,target_language,prompt_version,translated_text) VALUES ('name','person_name','p1','Actor 1','zh-CN','test','译名')`,
		`ANALYZE metadata_items`, `ANALYZE metadata_identifiers`, `ANALYZE tmdb_subjects`, `ANALYZE tmdb_credits`, `ANALYZE people`, `ANALYZE tmdb_person_profiles`, `ANALYZE translation_caches`,
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
		if strings.HasPrefix(sql, "SELECT") {
			queries = append(queries, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:search-people", capture); err != nil {
		t.Fatal(err)
	}
	ids, names, err := repos.MediaView.metadataSearchPeople(t.Context(), []string{"movie", "series", "ambiguous", "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	for id, values := range ids {
		ids[id] = uniqueNonEmptyStrings(values)
		sort.Strings(ids[id])
	}
	if !reflect.DeepEqual(ids, map[string][]string{"movie": {"p1"}, "series": {"p2", "p3", "p4"}}) {
		t.Fatalf("unexpected relationships: %v", ids)
	}
	if !containsStringValue(names["movie"], "译名") || !containsStringValue(names["movie"], "actor 1") {
		t.Fatalf("source names lost: %v", names)
	}
	if len(queries) == 0 {
		t.Fatal("no queries captured")
	}
	// 统计真实读取量，而非仅验证 SQL 文本或查询次数；同时检查通用预编译计划。
	for i, q := range queries {
		var raw []byte
		if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+q.sql, q.vars...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plans []struct{ Plan map[string]any }
		if err := json.Unmarshal(raw, &plans); err != nil {
			t.Fatal(err)
		}
		visits := map[string]float64{}
		var inspect func(map[string]any)
		inspect = func(n map[string]any) {
			relation, _ := n["Relation Name"].(string)
			rows, _ := n["Actual Rows"].(float64)
			removed, _ := n["Rows Removed by Filter"].(float64)
			loops, _ := n["Actual Loops"].(float64)
			visits[relation] += (rows + removed) * loops
			children, _ := n["Plans"].([]any)
			for _, child := range children {
				inspect(child.(map[string]any))
			}
		}
		inspect(plans[0].Plan)
		for _, table := range []string{"people", "tmdb_person_profiles", "tmdb_subjects", "tmdb_credits"} {
			if visits[table] > 100 {
				t.Fatalf("query %d scanned unrelated %s rows: %.0f", i, table, visits[table])
			}
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		conn, err := pool.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		err = conn.Raw(func(driverConn any) error {
			results, e := driverConn.(*stdlib.Conn).Conn().PgConn().Exec(t.Context(), "EXPLAIN (GENERIC_PLAN, FORMAT JSON) "+q.sql).ReadAll()
			if e == nil {
				raw = results[0].Rows[0][0]
			}
			return e
		})
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &plans); err != nil {
			t.Fatal(err)
		}
		var inspectGeneric func(map[string]any)
		inspectGeneric = func(n map[string]any) {
			relation, _ := n["Relation Name"].(string)
			rows, _ := n["Plan Rows"].(float64)
			if (relation == "people" || relation == "tmdb_person_profiles" || relation == "tmdb_subjects") && rows > 100 {
				t.Fatalf("generic query %d expands %s: %.0f", i, relation, rows)
			}
			condition, _ := n["Index Cond"].(string)
			if relation == "tmdb_subjects" && !strings.Contains(condition, "tmdb_id") {
				t.Fatal("source lookup misses ID index")
			}
			children, _ := n["Plans"].([]any)
			for _, child := range children {
				inspectGeneric(child.(map[string]any))
			}
		}
		inspectGeneric(plans[0].Plan)

	}

	library := model.Library{Name: "索引测试", Type: "movie", Path: t.TempDir(), Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, MetadataID: "movie", Path: library.Path + "/movie.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	backend := &recordingMetadataSearchBackend{}
	repos.MediaView.SetSearchBackend(backend)
	refresh := func() MetadataSearchDocument {
		t.Helper()
		backend.upserts = nil
		if err := repos.MediaView.RefreshMetadataIDs(t.Context(), "movie"); err != nil {
			t.Fatal(err)
		}
		if len(backend.upserts) != 1 {
			t.Fatalf("expected movie document, got %d", len(backend.upserts))
		}
		return backend.upserts[0]
	}
	if document := refresh(); !reflect.DeepEqual(document.PersonIDs, []string{"p1"}) || !containsStringValue(document.PersonNames, "译名") {
		t.Fatalf("missing indexed person: %+v", document)
	}
	// 改名后旧译文不再匹配；解绑后旧人物必须从索引文档移除。
	if err := db.Exec(`UPDATE tmdb_person_profiles SET name='New Name',original_name='New Name' WHERE id='p1'`).Error; err != nil {
		t.Fatal(err)
	}
	_, names, err = repos.MediaView.metadataSearchPeople(t.Context(), []string{"movie"})
	if err != nil || containsStringValue(names["movie"], "译名") || !containsStringValue(names["movie"], "new name") {
		t.Fatalf("stale name: %v %v", names, err)
	}
	if document := refresh(); containsStringValue(document.PersonNames, "译名") || !containsStringValue(document.PersonNames, "new name") {
		t.Fatal("indexed name did not refresh")
	}
	if err := db.Exec(`DELETE FROM metadata_identifiers WHERE id='movie'`).Error; err != nil {
		t.Fatal(err)
	}
	ids, _, err = repos.MediaView.metadataSearchPeople(t.Context(), []string{"movie"})
	if err != nil || len(ids) != 0 {
		t.Fatalf("stale binding: %v %v", ids, err)
	}
	if document := refresh(); len(document.PersonIDs) != 0 || len(document.PersonNames) != 0 {
		t.Fatal("unbound people remained indexed")
	}
	ids, names, err = repos.MediaView.metadataSearchPeople(t.Context(), nil)
	if err != nil || len(ids) != 0 || len(names) != 0 {
		t.Fatal("empty scope queried unrelated people")
	}
}
