package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestEmbyWorkBatchContinuesAndCounts(t *testing.T) {
	for _, source := range []string{"movie", "series", "nfo", "hongguo"} {
		t.Run(source, func(t *testing.T) {
			e := newTestEmbyService(t)
			db, ctx := e.repo.DB, t.Context()
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {visibility: MediaVisibility{IncludeNSFW: true}, expiresAt: time.Now().Add(time.Hour)}}
			lib := model.Library{Name: "Batch", Type: source, Path: "/fixture/batch"}
			if source == "series" {
				lib.Type = "tv"
			}
			if source == "nfo" {
				lib.Type = model.LibraryTypeNFOTV
			}
			if source == "hongguo" {
				lib.Type = model.LibraryTypeHongGuo
			}
			lib.ID = "batch-library"
			if err := db.Create(&lib).Error; err != nil {
				t.Fatal(err)
			}
			var setup []string
			prefix := "work-"
			switch source {
			case "movie", "series":
				setup = []string{
					`INSERT INTO metadata_items(id,kind,title,source) SELECT 'work-'||n,'movie','Title '||n,'local' FROM generate_series(1,155) n`,
					`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'file-'||n,'work-'||n,'batch-library','/fixture/batch/'||n,TIMESTAMP '2026-01-01'-n*INTERVAL '1 minute' FROM generate_series(1,155) n`,
					`INSERT INTO playback_histories(id,user_id,metadata_id,media_id,completed) SELECT 'state-'||n,'viewer','work-'||n,'file-'||n,TRUE FROM generate_series(1,155) n WHERE n NOT IN (51,103,155)`,
				}
				if source == "series" {
					prefix = "show-"
					setup = append(setup,
						`INSERT INTO metadata_items(id,kind,title,source) SELECT 'show-'||n,'series','Show '||n,'local' FROM generate_series(1,155) n`,
						`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num) SELECT 'season-'||n,'season','Season','local','show-'||n,1 FROM generate_series(1,155) n`,
						`UPDATE metadata_items SET kind='episode',parent_id='season-'||substring(id FROM 6),episode_num=1 WHERE id LIKE 'work-%'`,
						`UPDATE media SET season_num=1,episode_num=1`)
				}
			case "nfo":
				prefix = "nfo-work-"
				setup = []string{
					`INSERT INTO nfo_items(id,library_id,local_key,kind,title) SELECT 'work-'||n,'batch-library','work-'||n,'series','Title '||n FROM generate_series(1,155) n`,
					`INSERT INTO nfo_items(id,library_id,local_key,kind,title,parent_id,season_num) SELECT 'season-'||n,'batch-library','season-'||n,'season','Season','work-'||n,1 FROM generate_series(1,155) n`,
					`INSERT INTO nfo_items(id,library_id,local_key,kind,title,parent_id,episode_num) SELECT 'ep-'||n,'batch-library','ep-'||n,'episode','Episode','season-'||n,1 FROM generate_series(1,155) n`,
					`INSERT INTO media(id,library_id,path,catalog_source,created_at) SELECT 'file-'||n,'batch-library','/fixture/batch/'||n,'nfo',TIMESTAMP '2026-01-01'-n*INTERVAL '1 minute' FROM generate_series(1,155) n`,
					`INSERT INTO nfo_media_bindings(media_id,item_id,title,fingerprint) SELECT 'file-'||n,'ep-'||n,'Episode','fixture' FROM generate_series(1,155) n`,
					`INSERT INTO nfo_user_states(user_id,item_id,media_id,completed) SELECT 'viewer','ep-'||n,'file-'||n,TRUE FROM generate_series(1,155) n WHERE n NOT IN (51,103,155)`,
				}
			case "hongguo":
				prefix = "hg-group-"
				// 每个合集有两季，批次必须按 155 张卡片而非 310 个源作品计算。
				setup = []string{
					`INSERT INTO hongguo_works(id,source_id,kind,title,related_album_id,season_index,refreshed_at) SELECT 'work-'||n||'-'||s,n||'-'||s,'series','Title '||n,n::text,s,now() FROM generate_series(1,155) n CROSS JOIN generate_series(1,2) s`,
					`INSERT INTO hongguo_episodes(id,work_id,number) SELECT 'ep-'||id,id,1 FROM hongguo_works`,
					`INSERT INTO media(id,library_id,path,catalog_source,created_at) SELECT 'file-'||id,'batch-library','/fixture/batch/'||id,'hongguo',TIMESTAMP '2026-01-01'-related_album_id::int*INTERVAL '1 minute' FROM hongguo_works`,
					`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) SELECT 'file-'||id,id,'ep-'||id FROM hongguo_works`,
					`INSERT INTO hongguo_user_states(user_id,source_id,episode_number,completed) SELECT 'viewer',source_id,1,TRUE FROM hongguo_works WHERE related_album_id::int NOT IN (51,103,155)`,
				}
			}
			for _, query := range setup {
				if err := db.Exec(query).Error; err != nil {
					t.Fatal(err)
				}
			}
			batches := 0
			randomBatches := 0
			capture := func(tx *gorm.DB) {
				if strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") && strings.Contains(tx.Statement.SQL.String(), "LEFT JOIN qualified") {
					batches++
					if strings.Contains(tx.Statement.SQL.String(), "md5(") && strings.Contains(tx.Statement.SQL.String(), "'42'") {
						randomBatches++
					}
				}
			}
			if err := db.Callback().Row().After("gorm:row").Register("test:batch-count", capture); err != nil {
				t.Fatal(err)
			}
			latest, err := e.LatestItems(ctx, "viewer", lib.ID, 2, false, "BasicSyncInfo")
			want := []string{prefix + "51", prefix + "103"}
			ids := func(items []map[string]any) []string {
				out := []string{}
				for _, item := range items {
					out = append(out, item["Id"].(string))
				}
				return out
			}
			if err != nil || !reflect.DeepEqual(ids(latest), want) || batches != 3 {
				t.Fatalf("latest=%v want=%v batches=%d err=%v", ids(latest), want, batches, err)
			}
			p := ItemsParams{UserID: "viewer", ParentID: lib.ID, SortBy: "DateLastContentAdded", SortOrder: "Descending", Filters: []string{"IsUnplayed"}, Limit: 2, Fields: []string{"BasicSyncInfo"}}
			for _, skip := range []bool{false, true} {
				p.SkipTotalRecordCount = skip
				for _, start := range []int{0, 1, 2, 3, 1000} {
					p.StartIndex = start
					var got []string
					var total int64
					if source == "nfo" || source == "hongguo" {
						result, queryErr := e.Items(ctx, p)
						err = queryErr
						if err == nil {
							got = ids(result["Items"].([]map[string]any))
							total = result["TotalRecordCount"].(int64)
						}
					} else {
						files := e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}).Where("media.library_id=?", lib.ID), p.UserID)
						files = e.applyLatestPlayedFilter(ctx, files, p.UserID, false)
						if source == "movie" {
							views, n, queryErr := e.metadataWorkPage(ctx, files, p, false)
							total, err = n, queryErr
							for _, v := range views {
								got = append(got, v.MetadataID)
							}
						} else {
							groups, n, queryErr := e.seriesWorkPage(ctx, files, p, start, 2)
							total, err = n, queryErr
							for _, v := range groups {
								got = append(got, v.ID)
							}
						}
					}
					all := []string{prefix + "51", prefix + "103", prefix + "155"}
					want = all[min(start, 3):min(start+2, 3)]
					wantTotal := int64(3)
					if skip {
						wantTotal = 0
						if (source == "nfo" || source == "hongguo") && len(want) > 0 {
							wantTotal = int64(start + len(pageSlice(all, start, p.Limit+1)))
						}
					}
					if err != nil || total != wantTotal || strings.Join(got, ",") != strings.Join(want, ",") {
						t.Fatalf("start=%d got=%v total=%d want=%v err=%v", start, got, total, want, err)
					}
				}
			}
			// 第一批全部未看时够数就停，不继续扫完整库。
			batches = 0
			latest, err = e.LatestItems(ctx, "other-user", lib.ID, 2, false, "BasicSyncInfo")
			if err != nil || len(latest) != 2 || batches != 1 {
				t.Fatalf("early stop items=%d batches=%d err=%v", len(latest), batches, err)
			}
			p.SortBy, p.SortOrder, p.randomSeed = "Random,SortName", "", 42
			p.IncludeItemTypes = []string{"Series"}
			if source == "movie" {
				p.IncludeItemTypes = []string{"Movie"}
			}
			p.StartIndex, p.Limit = 0, 50
			page, err := e.Items(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			randomIDs := ids(page["Items"].([]map[string]any))
			if len(randomIDs) != 3 || page["TotalRecordCount"] != int64(3) {
				t.Fatalf("random unplayed=%v total=%v", randomIDs, page["TotalRecordCount"])
			}
			if randomBatches < 2 {
				t.Fatalf("random refill did not retain its seed across batches: %d", randomBatches)
			}
			allowed := map[string]bool{prefix + "51": true, prefix + "103": true, prefix + "155": true}
			for _, id := range randomIDs {
				if !allowed[id] {
					t.Fatalf("random returned played/duplicate work %s", id)
				}
				delete(allowed, id)
			}
			p.Limit = 1
			for _, start := range []int{0, 1, 2, 3} {
				p.StartIndex = start
				page, err = e.Items(ctx, p)
				if err != nil {
					t.Fatal(err)
				}
				got := ids(page["Items"].([]map[string]any))
				if strings.Join(got, ",") != strings.Join(pageSlice(randomIDs, start, 1), ",") {
					t.Fatalf("random refill shifted: start=%d got=%v all=%v", start, got, randomIDs)
				}
			}
		})
	}
}

func TestEmbyWorkBatchError(t *testing.T) {
	e := newTestEmbyService(t)
	db := e.repo.DB
	var initialJIT string
	if err := db.Raw("SHOW jit").Scan(&initialJIT).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("SET jit = on").Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec("SELECT set_config('jit',?,false)", initialJIT)
	assertJITRestored := func() {
		t.Helper()
		var value string
		if err := db.Raw("SHOW jit").Scan(&value).Error; err != nil || value != "on" {
			t.Fatalf("batch transaction leaked jit=%s err=%v", value, err)
		}
	}
	candidates := db.Table("generate_series(1,155) n").Select("n::text AS id,n AS ordinal").Order("n")
	eligible := db.Table("work_batch").Select("ordinal").Where("1 / (CASE WHEN ordinal>50 THEN 0 ELSE 1 END) > 0 AND ordinal>50")
	if _, _, err := e.filteredWorkBatchPage(t.Context(), candidates, eligible, 0, 2, false); err == nil {
		t.Fatal("batch query error was swallowed")
	}
	assertJITRestored()
	for _, start := range []int{0, 1, 4} {
		eligible = db.Table("work_batch").Select("ordinal").Where("ordinal IN (51,103,155)").
			Where("current_setting('transaction_isolation')='repeatable read' AND current_setting('transaction_read_only')='on' AND current_setting('jit')='off'")
		ids, total, err := e.filteredWorkBatchPage(t.Context(), candidates, eligible, start, 2, true)
		want := []string{"51", "103", "155"}[min(start, 3):min(start+2, 3)]
		if err != nil || total != 3 || fmt.Sprint(ids) != fmt.Sprint(want) {
			t.Fatalf("ids=%v total=%d err=%v", ids, total, err)
		}
		assertJITRestored()
	}
}

func TestEmbyKnownMovieMembershipSkipsFileQualification(t *testing.T) {
	e := newTestEmbyService(t)
	db, ctx := e.repo.DB, t.Context()
	for _, query := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'movie-'||n,'movie','Movie','local' FROM generate_series(1,3) n`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'file-'||n,'movie-'||n,'movies','/fixture/'||n,TIMESTAMP '2026-01-01'+n*INTERVAL '1 hour' FROM generate_series(1,3) n`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	if err := db.Callback().Row().After("gorm:row").Register("test:known-membership", func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.SQL.String(), "WITH work_batch AS MATERIALIZED") {
			queries = append(queries, statement{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	p := ItemsParams{ParentID: "movies", SortBy: "DateLastContentAdded", SortOrder: "Descending", Limit: 2}
	files := e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}).Where("media.library_id=?", p.ParentID), "")
	views, total, err := e.metadataWorkPage(ctx, files, p, true)
	if err != nil || total != 3 || len(views) != 2 || views[0].MetadataID != "movie-3" || len(queries) != 2 {
		t.Fatalf("views=%v total=%d queries=%d err=%v", views, total, len(queries), err)
	}
	for _, query := range append([]statement(nil), queries...) {
		var raw []byte
		if err := db.Statement.ConnPool.QueryRowContext(ctx, "EXPLAIN (ANALYZE,FORMAT JSON) "+query.sql, query.vars...).Scan(&raw); err != nil {
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
		var check func(node)
		check = func(n node) {
			if n.Relation == "media" && n.Loops != 0 {
				t.Fatalf("known membership queried files: %s", raw)
			}
			for _, child := range n.Plans {
				check(child)
			}
		}
		check(plans[0].Plan)
	}
	if err := db.Where("id=?", "file-3").Delete(&model.Media{}).Error; err != nil {
		t.Fatal(err)
	}
	views, total, err = e.metadataWorkPage(ctx, files, p, true)
	if err != nil || total != 2 || len(views) != 2 || views[0].MetadataID != "movie-2" {
		t.Fatalf("deleted last file retained card: total=%d err=%v", total, err)
	}
	// NULL 文件库不满足 SQL 的隐藏库排除条件；空归属必须保留同一资格检查。
	if err := db.Exec("UPDATE media SET library_id=NULL WHERE id='file-1'").Error; err != nil {
		t.Fatal(err)
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "hidden-viewer": {
		visibility: MediaVisibility{HiddenLibraryIDs: []string{"movies"}}, expiresAt: time.Now().Add(time.Hour),
	}}
	p.ParentID, p.UserID = "", "hidden-viewer"
	files = e.applyUserMediaVisibility(ctx, db.Model(&model.Media{}), p.UserID)
	views, total, err = e.metadataWorkPage(ctx, files, p, true)
	if err != nil || total != 0 || len(views) != 0 {
		t.Fatalf("empty membership bypassed file visibility: total=%d views=%d err=%v", total, len(views), err)
	}
}
