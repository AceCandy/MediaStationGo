package database

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestLatestMediaAdded(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := EnsureLatestMediaAddedTriggers(db); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	check := func(table, id, want string) {
		t.Helper()
		var got *time.Time
		if err := db.Raw("SELECT latest_media_added_at FROM "+table+" WHERE id=?", id).Row().Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want == "" && got != nil || want != "" && (got == nil || got.UTC().Format("2006-01-02") != want) {
			t.Fatalf("%s/%s latest=%v, want %s", table, id, got, want)
		}
	}
	checkLibraries := func(table, id, want string) {
		t.Helper()
		var equal bool
		predicate := "library_ids IS NULL"
		args := []any{id}
		if want != "" {
			predicate = "COALESCE(library_ids = ?::jsonb, FALSE)"
			args = []any{want, id}
		}
		if err := db.Raw("SELECT "+predicate+" FROM "+table+" WHERE id=?", args...).Row().Scan(&equal); err != nil || !equal {
			t.Fatalf("%s/%s libraries want=%s err=%v", table, id, want, err)
		}
	}
	exec(`INSERT INTO metadata_items(id,kind,title,source,season_num,episode_num) VALUES ('a','series','a','local',0,0),('b','series','b','local',0,0)`)
	exec(`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num,episode_num) VALUES ('s','season','s','local','a',1,0),('e','episode','e','local','s',0,1)`)
	checkLibraries("metadata_items", "e", "")
	exec(`INSERT INTO media(id,path,metadata_id,library_id,created_at) VALUES ('m1','/test/m1','e','a','2026-01-01'),('m2','/test/m2','e','b','2026-01-02')`)
	for _, id := range []string{"a", "s", "e"} {
		check("metadata_items", id, "2026-01-02")
		checkLibraries("metadata_items", id, `["a","b"]`)
	}
	var stale model.MetadataItem
	if err := db.First(&stale, "id='e'").Error; err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE media SET created_at='2026-01-03' WHERE id='m2'`)
	exec(`UPDATE media SET library_id='c' WHERE id='m2'`)
	if err := db.Omit(clause.Associations).Save(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Omit(clause.Associations).Clauses(clause.OnConflict{UpdateAll: true}).Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	checkLibraries("metadata_items", "e", `["a","c"]`)
	// 同库多个文件去重；移走一个文件仍保留原库，最后一个离开才退出。
	exec(`UPDATE media SET library_id='a' WHERE id='m2'`)
	checkLibraries("metadata_items", "a", `["a"]`)
	exec(`UPDATE media SET library_id='b' WHERE id='m2'`)
	check("metadata_items", "e", "2026-01-03")
	exec(`UPDATE metadata_items SET parent_id='b' WHERE id='s'`)
	check("metadata_items", "a", "")
	checkLibraries("metadata_items", "a", `[]`)
	checkLibraries("metadata_items", "b", `["a","b"]`)
	check("metadata_items", "b", "2026-01-03")
	exec(`DELETE FROM media WHERE id='m2'`)
	check("metadata_items", "b", "2026-01-01")
	checkLibraries("metadata_items", "b", `["a"]`)
	exec(`UPDATE media SET metadata_id='a' WHERE id='m1'`)
	check("metadata_items", "b", "")
	checkLibraries("metadata_items", "b", `[]`)
	check("metadata_items", "a", "2026-01-01")
	checkLibraries("metadata_items", "a", `["a"]`)
	exec(`DELETE FROM media WHERE id='m1'`)
	check("metadata_items", "a", "")
	checkLibraries("metadata_items", "a", `[]`)

	exec(`INSERT INTO nfo_items(id,library_id,local_key,kind,title) VALUES ('n','l','n','series','n'),('n2','l','n2','series','n2')`)
	exec(`INSERT INTO nfo_items(id,library_id,local_key,kind,title,parent_id) VALUES ('ne','l','ne','episode','ne','n')`)
	exec(`INSERT INTO hongguo_works(id,source_id,kind,title,refreshed_at) VALUES ('h','h','series','h',now()),('h2','h2','series','h2',now())`)
	for _, source := range []struct{ table, entity, key, id, other string }{
		{"nfo_media_bindings", "nfo_items", "item_id", "ne", "n2"},
		{"hongguo_media_bindings", "hongguo_works", "work_id", "h", "h2"},
	} {
		for i := 1; i <= 2; i++ {
			id := fmt.Sprintf("%s%d", source.id, i)
			exec(`INSERT INTO media(id,path,library_id,created_at) VALUES (?,?,?,?)`, id, "/test/"+id, fmt.Sprintf("lib%d", i), fmt.Sprintf("2026-01-0%d", i))
			columns, values := "", ""
			if source.key == "item_id" {
				columns, values = ",fingerprint,title", ",'test','test'"
			}
			exec(fmt.Sprintf("INSERT INTO %s(media_id,%s%s) VALUES (?,?%s)", source.table, source.key, columns, values), id, source.id)
		}
		check(source.entity, source.id, "2026-01-02")
		if source.key == "work_id" {
			checkLibraries(source.entity, source.id, `["lib1","lib2"]`)
			var staleWork model.HongGuoWork
			if err := db.First(&staleWork, "id=?", source.id).Error; err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE media SET library_id='lib1' WHERE id=?`, source.id+"2")
			if err := db.Omit(clause.Associations).Save(&staleWork).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Omit(clause.Associations).Clauses(clause.OnConflict{UpdateAll: true}).Create(&staleWork).Error; err != nil {
				t.Fatal(err)
			}
			checkLibraries(source.entity, source.id, `["lib1"]`)
			exec(`UPDATE media SET library_id='lib2' WHERE id=?`, source.id+"2")
		}
		exec("UPDATE "+source.table+" SET "+source.key+"=? WHERE media_id=?", source.other, source.id+"2")
		check(source.entity, source.id, "2026-01-01")
		check(source.entity, source.other, "2026-01-02")
		if source.key == "work_id" {
			checkLibraries(source.entity, source.id, `["lib1"]`)
			checkLibraries(source.entity, source.other, `["lib2"]`)
		}
		exec(`DELETE FROM media WHERE id IN (?,?)`, source.id+"1", source.id+"2")
		check(source.entity, source.id, "")
		check(source.entity, source.other, "")
		if source.key == "work_id" {
			checkLibraries(source.entity, source.id, `[]`)
			checkLibraries(source.entity, source.other, `[]`)
		}
	}
	check("nfo_items", "n", "")
	// 独立连接固定到同一隔离 schema，确认等待锁后能看见另一事务的提交。
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	peer, err := gorm.Open(postgres.Open(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := peer.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pool.SetMaxOpenConns(1)
	if err := peer.Exec(`SET search_path TO "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	overlap := func(first, second string) {
		t.Helper()
		tx := db.Begin()
		if err := tx.Exec(first).Error; err != nil {
			_ = tx.Rollback().Error
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- peer.Exec(second).Error }()
		select {
		case err := <-done:
			_ = tx.Rollback().Error
			t.Fatalf("concurrent write did not wait for work lock: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		if err := tx.Commit().Error; err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent write timed out")
		}
	}
	overlap(
		"INSERT INTO media(id,path,metadata_id,library_id,created_at) VALUES ('concurrent1','/test/concurrent1','a','a','2026-02-02')",
		"INSERT INTO media(id,path,metadata_id,library_id,created_at) VALUES ('concurrent2','/test/concurrent2','a','b','2026-02-01')",
	)
	check("metadata_items", "a", "2026-02-02")
	checkLibraries("metadata_items", "a", `["a","b"]`)
	tx := db.Begin()
	if err := tx.Exec(`DELETE FROM media WHERE id LIKE 'concurrent%'`).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatal(err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	check("metadata_items", "a", "2026-02-02")
	checkLibraries("metadata_items", "a", `["a","b"]`)
	overlap("UPDATE media SET library_id='c' WHERE id='concurrent1'", "DELETE FROM media WHERE id='concurrent2'")
	checkLibraries("metadata_items", "a", `["c"]`)
	exec(`DELETE FROM media WHERE id LIKE 'concurrent%'`)
	check("metadata_items", "a", "")
	checkLibraries("metadata_items", "a", `[]`)
	// 触发器存在时的再次迁移不能因 UPDATE OF 列依赖失败。
	overlap("UPDATE metadata_items SET parent_id='a' WHERE id='s'",
		"INSERT INTO media(id,path,metadata_id,created_at) VALUES ('moving','/test/moving','e','2026-03-02')")
	check("metadata_items", "a", "2026-03-02")
	check("metadata_items", "b", "")
	overlap("DELETE FROM media WHERE id='moving'",
		"INSERT INTO media(id,path,metadata_id,created_at) VALUES ('remaining','/test/remaining','e','2026-03-01')")
	check("metadata_items", "a", "2026-03-01")
	check("metadata_items", "s", "2026-03-01")
	// 缺库 ID 不构成任何库的成员；升级安装不能把未知归属静默当空集合。
	checkLibraries("metadata_items", "a", `[]`)
	exec(`UPDATE metadata_items SET library_ids=NULL WHERE id='a'`)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	checkLibraries("metadata_items", "a", "")
	var revisionsBefore, revisionsAfter int64
	if err := db.Raw(`SELECT COALESCE(SUM(revision),0)::bigint FROM tm_db_recheck_changes`).Scan(&revisionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE media SET library_id='migrated' WHERE id='remaining'`)
	checkLibraries("metadata_items", "a", `["migrated"]`)
	if err := db.Raw(`SELECT COALESCE(SUM(revision),0)::bigint FROM tm_db_recheck_changes`).Scan(&revisionsAfter).Error; err != nil || revisionsAfter != revisionsBefore {
		t.Fatalf("library-only change enqueued metadata recheck: before=%d after=%d err=%v", revisionsBefore, revisionsAfter, err)
	}
	// 移动分集必须沿旧、新季继续刷新两边的整剧祖先。
	exec(`INSERT INTO metadata_items(id,kind,title,source,parent_id,season_num) VALUES ('s2','season','S2','local','b',2)`)
	exec(`UPDATE metadata_items SET parent_id='s2' WHERE id='e'`)
	for _, id := range []string{"a", "s"} {
		checkLibraries("metadata_items", id, `[]`)
	}
	for _, id := range []string{"b", "s2", "e"} {
		checkLibraries("metadata_items", id, `["migrated"]`)
	}
	exec(`INSERT INTO media(id,path,library_id,catalog_source,created_at) VALUES ('hc1','/test/hc1','a','hongguo','2026-04-02'),('hc2','/test/hc2','b','hongguo','2026-04-01')`)
	overlap("INSERT INTO hongguo_media_bindings(media_id,work_id) VALUES ('hc1','h')",
		"INSERT INTO hongguo_media_bindings(media_id,work_id) VALUES ('hc2','h')")
	checkLibraries("hongguo_works", "h", `["a","b"]`)
	overlap("UPDATE media SET library_id='c' WHERE id='hc1'", "DELETE FROM media WHERE id='hc2'")
	checkLibraries("hongguo_works", "h", `["c"]`)
	check("hongguo_works", "h", "2026-04-02")
	exec(`DELETE FROM media WHERE id='hc1'`)
	checkLibraries("hongguo_works", "h", `[]`)
}
