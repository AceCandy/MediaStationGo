package repository

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestSearchUsesMaintainedMembership(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	db, ctx := repos.DB, t.Context()
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) SELECT id,'movie','Search target','local' FROM unnest(ARRAY['known','hidden','unknown','empty','unassigned']) id`,
		`INSERT INTO metadata_items(id,kind,title,source) SELECT id,'series','Search target','local' FROM unnest(ARRAY['series','direct','wrong-scope']) id`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num) SELECT id||'-season','season','Season','local',id,1 FROM unnest(ARRAY['series','wrong-scope']) id`,
		`INSERT INTO metadata_items(id,kind,title,source,parent_id,episode_num) SELECT id||'-episode','episode','Episode','local',id||'-season',1 FROM unnest(ARRAY['series','wrong-scope']) id`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'file-'||id,id,CASE WHEN id IN ('hidden','wrong-scope-episode') THEN 'hidden' WHEN id='unassigned' THEN NULL ELSE 'visible' END,'/fixture/search/'||id,'2026-01-01' FROM unnest(ARRAY['known','hidden','unknown','unassigned','series-episode','direct','wrong-scope','wrong-scope-episode']) id`,
		`UPDATE metadata_items SET library_ids=NULL WHERE id='unknown'`,
		`INSERT INTO nfo_items(id,library_id,local_key,kind,title) SELECT id,CASE WHEN id='hidden' THEN 'hidden' ELSE 'visible' END,id,'movie','Search target' FROM unnest(ARRAY['known','hidden','unknown','empty']) id`,
		`INSERT INTO media(id,library_id,catalog_source,path,created_at) SELECT 'nfo-file-'||id,library_id,'nfo','/fixture/nfo/'||id,'2026-01-01' FROM nfo_items WHERE id<>'empty'`,
		`INSERT INTO nfo_media_bindings(media_id,item_id,fingerprint,title) SELECT 'nfo-file-'||id,id,'test',title FROM nfo_items WHERE id<>'empty'`,
		`UPDATE nfo_items SET latest_media_added_at=NULL WHERE id='unknown'`,
		`INSERT INTO hongguo_works(id,source_id,kind,title,refreshed_at) SELECT id,id,'movie','Search target',now() FROM unnest(ARRAY['known','hidden','unknown','empty','unassigned']) id`,
		`INSERT INTO media(id,library_id,catalog_source,path,created_at) SELECT 'hg-file-'||id,CASE WHEN id='hidden' THEN 'hidden' WHEN id='unassigned' THEN NULL ELSE 'visible' END,'hongguo','/fixture/hg/'||id,'2026-01-01' FROM hongguo_works WHERE id<>'empty'`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id) SELECT 'hg-file-'||id,id FROM hongguo_works WHERE id<>'empty'`,
		`UPDATE hongguo_works SET library_ids=NULL WHERE id='unknown'`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []MediaQueryFilter{
		{}, {AllowedLibraryIDs: []string{"visible"}}, {HiddenLibraryIDs: []string{"hidden"}},
		{AllowedLibraryIDs: []string{"visible", "hidden"}, HiddenLibraryIDs: []string{"hidden"}},
		{AllowedLibraryIDs: []string{"missing"}},
	} {
		filter, err := repos.MediaView.prepareMetadataSearchFilter(ctx, MetadataSearchFilter{MediaQueryFilter: scope, Kinds: []string{"movie", "series"}})
		if err != nil {
			t.Fatal(err)
		}
		old := db.Table("metadata_items search_metadata").Where("kind IN ?", filter.Kinds).Where(metadataPlayableExistsSQL)
		if filter.LibraryRestricted {
			old = old.Where(metadataPlayableInLibrariesSQL, &filter.VisibleLibraryIDs, &filter.VisibleLibraryIDs)
		}
		compareSearchIDs(t, old, repos.MediaView.metadataSearchQuery(ctx, filter))
		files := repos.MediaView.NFOCandidateFiles(ctx, scope, "search_metadata.id").Where("COALESCE(nw.id,ni.id)=search_metadata.id").Select("1")
		old = db.Table("nfo_items search_metadata").Where("kind IN ?", filter.Kinds).Where("EXISTS (? OFFSET 0)", files)
		compareSearchIDs(t, old, repos.MediaView.nfoSearchQuery(ctx, filter, nil))
		files = repos.MediaView.hongGuoFileScope(ctx, "", scope).Select("1").Where("b.work_id=w.id")
		var want []MetadataSearchCandidate
		if err := db.Table("(?) search_metadata", repos.HongGuo.hongGuoSearchWorks(ctx).Where("EXISTS (? OFFSET 0)", files)).Order("title,id").Scan(&want).Error; err != nil {
			t.Fatal(err)
		}
		for i := range want {
			want[i].Kind = "movie"
		}
		got, err := repos.HongGuo.SearchCandidates(ctx, "Search target", MetadataSearchFilter{MediaQueryFilter: scope, Kinds: []string{"movie", "series"}})
		if err != nil || len(got) != len(want) || len(got) > 0 && !reflect.DeepEqual(got, want) {
			t.Fatalf("HongGuo scope=%+v got=%v want=%v err=%v", scope, got, want, err)
		}
	}
	filter := MetadataSearchFilter{Kinds: []string{"movie"}, LibraryRestricted: true, VisibleLibraryIDs: []string{"visible"}, MediaQueryFilter: MediaQueryFilter{AllowedLibraryIDs: []string{"visible"}}}
	for _, q := range []*gorm.DB{repos.MediaView.metadataSearchQuery(ctx, filter), repos.MediaView.nfoSearchQuery(ctx, filter, nil)} {
		stmt := q.Where("search_metadata.id='known'").Select("search_metadata.id").Session(&gorm.Session{DryRun: true}).Find(&[]string{}).Statement
		assertSearchSkipsFiles(t, db, stmt.SQL.String(), stmt.Vars)
	}
	var query string
	var args []any
	if err := db.Callback().Row().After("gorm:row").Register("test:search-membership", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "AS search_metadata") && strings.Contains(tx.Statement.SQL.String(), "hongguo_works") {
			query, args = tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	repos.HongGuo.SetSearchBackend(&fakeMediaSearchBackend{ids: []string{"hg-work-known"}})
	if _, err := repos.HongGuo.SearchCandidates(ctx, "Search target", filter); err != nil {
		t.Fatal(err)
	}
	if query == "" {
		t.Fatal("HongGuo revalidation query not captured")
	}
	assertSearchSkipsFiles(t, db, query, args)
	// 删除最后一个版本后，维护字段必须让三个来源都消失，不能保留空卡片。
	if err := db.Exec("DELETE FROM media WHERE id IN ('file-known','nfo-file-known','hg-file-known')").Error; err != nil {
		t.Fatal(err)
	}
	for _, q := range []*gorm.DB{repos.MediaView.metadataSearchQuery(ctx, filter), repos.MediaView.nfoSearchQuery(ctx, filter, nil)} {
		var n int64
		if err := q.Where("search_metadata.id='known'").Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("deleted work count=%d err=%v", n, err)
		}
	}
	rows, err := repos.HongGuo.SearchCandidates(ctx, "Search target", filter)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted indexed work=%v err=%v", rows, err)
	}
}

func compareSearchIDs(t *testing.T, old, current *gorm.DB) {
	t.Helper()
	var want, got []string
	if err := old.Order("search_metadata.id").Pluck("search_metadata.id", &want).Error; err != nil {
		t.Fatal(err)
	}
	if err := current.Order("search_metadata.id").Pluck("search_metadata.id", &got).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("search changed: got=%v want=%v", got, want)
	}
}

func assertSearchSkipsFiles(t *testing.T, db *gorm.DB, query string, args []any) {
	t.Helper()
	var raw []byte
	if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	type node struct {
		Relation string  `json:"Relation Name"`
		Loops    float64 `json:"Actual Loops"`
		Plans    []node
	}
	var plans []struct{ Plan node }
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}
	var inspect func(node)
	inspect = func(n node) {
		if (n.Relation == "media" || strings.HasSuffix(n.Relation, "media_bindings")) && n.Loops != 0 {
			t.Fatalf("known search still reads files: %+v", n)
		}
		for _, child := range n.Plans {
			inspect(child)
		}
	}
	inspect(plans[0].Plan)
}
