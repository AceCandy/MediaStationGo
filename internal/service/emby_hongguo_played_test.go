package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestHongGuoContainerPlayedScopeAndRollback(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	e := NewEmbyService(&config.Config{}, zap.NewNop(), repository.New(db))
	e.SetRuntimeCache(NewRuntimeCacheService(nil, e.log))
	ctx := t.Context()
	e.visibilityCache = map[string]embyVisibilityCacheEntry{
		e.repo.ReadCacheKey() + "viewer": {visibility: MediaVisibility{AllowedLibraryIDs: []string{"visible", "hidden"}, HiddenLibraryIDs: []string{"hidden"}}, expiresAt: time.Now().Add(time.Hour)},
		e.repo.ReadCacheKey() + "locked": {visibility: MediaVisibility{LibraryRestricted: true}, expiresAt: time.Now().Add(time.Hour)},
	}
	for _, sql := range []string{
		`INSERT INTO hongguo_works (id,source_id,kind,title,related_album_id,season_index,refreshed_at)
VALUES ('work-1','91001','series','Season 1','91999',1,now()),('work-2','91002','series','Season 2','91999',2,now())`,
		`INSERT INTO hongguo_episodes (id,work_id,number)
SELECT 'episode-'||w||'-'||n,'work-'||w,n FROM generate_series(1,2) w CROSS JOIN generate_series(1,530) n`,
		`INSERT INTO media (id,library_id,catalog_source,lookup_catalog_id,path,episode_num)
SELECT 'file-'||w||'-'||n||'-'||v,CASE WHEN n=529 THEN 'hidden' ELSE 'visible' END,'hongguo','9100'||w,
'/test/played/'||w||'/'||n||'-'||v,n FROM generate_series(1,2) w CROSS JOIN generate_series(1,529) n CROSS JOIN generate_series(1,2) v`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id,episode_id)
SELECT 'file-'||w||'-'||n||'-'||v,'work-'||w,'episode-'||w||'-'||n
FROM generate_series(1,2) w CROSS JOIN generate_series(1,529) n CROSS JOIN generate_series(1,2) v`,
		`INSERT INTO media_probe_metadata (media_id,duration_ms,probe_json,schema_version,probed_at) SELECT id,123456,'{}',1,now() FROM media WHERE id LIKE '%-1' AND episode_num <> 2`,
		`INSERT INTO hongguo_user_states (user_id,source_id,episode_number,completed)
VALUES ('viewer','91001',529,true),('other','91001',1,true)`,
		`INSERT INTO hongguo_favorites (user_id,item_id,favorite) VALUES ('viewer','hg-group-91999',true)`,
		`INSERT INTO hongguo_playback_events (id,user_id,session_id,source_id,episode_number,media_id,library_id,played_at)
VALUES ('event','viewer','session','91001',1,'file-1-1-1','visible',now())`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	count := func(source string, want int64) {
		t.Helper()
		var got int64
		if err := db.Model(&model.HongGuoUserState{}).Where("user_id = 'viewer' AND source_id = ? AND episode_number <= 528 AND completed", source).Count(&got).Error; err != nil || got != want {
			t.Fatalf("source=%s completed=%d want=%d err=%v", source, got, want, err)
		}
	}
	check := func(id string, played bool, unplayed int) {
		t.Helper()
		item, err := e.Item(ctx, id, "viewer")
		if err != nil || item == nil || item["UserData"].(map[string]any)["Played"] != played {
			t.Fatalf("detail %s played=%v: %v %v", id, played, item, err)
		}
		assertEmbyUnplayedCount(t, item, unplayed)
	}
	mark := func(id string, played bool) {
		t.Helper()
		key := e.embyItemsCacheKey("items", ItemsParams{UserID: "viewer"})
		e.cache.SetJSON(ctx, key, embyItemsCacheValue{}, time.Minute)
		started := time.Now()
		if err := e.MarkPlayed(ctx, "viewer", id, played); err != nil {
			t.Fatal(err)
		}
		t.Logf("mark %s played=%v: %s", id, played, time.Since(started))
		var cached embyItemsCacheValue
		if e.cache.GetJSON(ctx, key, &cached) {
			t.Fatal("manual mark retained stale item cache")
		}
	}
	for _, id := range []string{"hg-work-work-1", "hg-season-missing", "hg-group-missing"} {
		if err := e.MarkPlayed(ctx, "viewer", id, true); err == nil {
			t.Fatalf("accepted missing/obsolete identity %s", id)
		}
	}
	if err := e.MarkPlayed(ctx, "locked", "hg-group-91999", true); err == nil {
		t.Fatal("locked profile wrote state")
	}
	mark("hg-season-work-1", true)
	count("91001", 528)
	count("91002", 0)
	check("hg-season-work-1", true, 0)
	check("hg-group-91999", false, 528)
	var states []model.HongGuoUserState
	if err := db.Where("user_id = 'viewer' AND source_id = '91001' AND episode_number <= 528").Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		wantDuration := int64(123456)
		if state.EpisodeNumber == 2 {
			wantDuration = 0
		}
		if state.MediaID != fmt.Sprintf("file-1-%d-1", state.EpisodeNumber) || state.PositionMs != wantDuration || state.DurationMs != wantDuration || state.WatchedAt == nil {
			t.Fatalf("wrong representative or progress: %+v", state)
		}
	}
	mark("hg-group-91999", true)
	mark("hg-group-91999", true)
	count("91001", 528)
	count("91002", 528)
	check("hg-group-91999", true, 0)
	mark("hg-season-work-1", false)
	count("91001", 0)
	count("91002", 528)
	mark("hg-group-91999", false)
	check("hg-group-91999", false, 1056)
	// 第二批已经执行后报错，第一批及第二批都必须回滚，取消已看同样如此。
	for _, played := range []bool{true, false} {
		if !played {
			mark("hg-season-work-1", true)
		}
		writes := 0
		fail := func(tx *gorm.DB) {
			if tx.Statement.Table == "hongguo_user_states" {
				writes++
				if writes == 2 {
					tx.AddError(errors.New("injected state write failure"))
				}
			}
		}
		if played {
			err = db.Callback().Create().After("gorm:create").Register("test:fail-played", fail)
		} else {
			err = db.Callback().Delete().After("gorm:delete").Register("test:fail-played", fail)
		}
		if err != nil {
			t.Fatal(err)
		}
		err = e.MarkPlayed(ctx, "viewer", "hg-season-work-1", played)
		db.Callback().Create().Remove("test:fail-played")
		db.Callback().Delete().Remove("test:fail-played")
		if err == nil || writes != 2 {
			t.Fatalf("expected second-batch failure, writes=%d err=%v", writes, err)
		}
		want := int64(0)
		if !played {
			want = 528
		}
		count("91001", want)
	}
	var untouched int64
	if err := db.Model(&model.HongGuoUserState{}).Where("user_id = 'other' OR episode_number = 529").Count(&untouched).Error; err != nil || untouched != 2 {
		t.Fatalf("other user/hidden state changed: %d %v", untouched, err)
	}
	if err := db.Model(&model.HongGuoUserState{}).Where("episode_number = 530").Count(&untouched).Error; err != nil || untouched != 0 {
		t.Fatalf("fileless episode marked: %d %v", untouched, err)
	}
	if err := db.Model(&model.HongGuoPlaybackEvent{}).Count(&untouched).Error; err != nil || untouched != 1 {
		t.Fatalf("events changed: %d %v", untouched, err)
	}
	if err := db.Model(&model.HongGuoFavorite{}).Where("favorite").Count(&untouched).Error; err != nil || untouched != 1 {
		t.Fatalf("favorite changed: %d %v", untouched, err)
	}
	// 已标记的季新增可见文件后，新增集仍未看；单集入口与整季状态保持一致。
	for _, sql := range []string{
		`INSERT INTO media (id,library_id,catalog_source,lookup_catalog_id,path,episode_num) VALUES ('new-file','visible','hongguo','91001','/test/played/new',530)`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id,episode_id) VALUES ('new-file','work-1','episode-1-530')`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	check("hg-season-work-1", false, 1)
	mark("hg-episode-episode-1-530", true)
	check("hg-season-work-1", true, 0)
	mark("hg-episode-episode-1-530", false)
	check("hg-season-work-1", false, 1)
	// 未归组作品仍能按整剧标记；有文件但没有分集绑定的容器保持空操作。
	for _, sql := range []string{
		`UPDATE hongguo_works SET related_album_id='',season_index=0 WHERE id='work-2'`,
		`INSERT INTO hongguo_works (id,source_id,kind,title,refreshed_at) VALUES ('unbound','91003','series','Unbound',now())`,
		`INSERT INTO media (id,library_id,catalog_source,lookup_catalog_id,path) VALUES ('unbound-file','visible','hongguo','91003','/test/played/unbound')`,
		`INSERT INTO hongguo_media_bindings (media_id,work_id) VALUES ('unbound-file','unbound')`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	mark("hg-work-work-2", true)
	count("91002", 528)
	for _, id := range []string{"hg-work-unbound", "hg-season-unbound"} {
		mark(id, true)
		mark(id, false)
	}
	count("91003", 0)
}

// 使用六十万绑定文件的已有夹具，验证真实写入口只读取目标季或合集。
func assertHongGuoContainerPlayedPlan(t *testing.T, e *EmbyService) {
	t.Helper()
	db := e.repo.DB
	type statement struct {
		sql  string
		vars []any
	}
	var queries []statement
	if err := db.Callback().Row().After("gorm:row").Register("test:hg-played-plan", func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if !tx.DryRun && strings.Contains(sql, "hongguo_media_bindings") && !strings.HasPrefix(sql, "EXPLAIN") {
			queries = append(queries, statement{sql, append([]any(nil), tx.Statement.Vars...)})
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Row().Remove("test:hg-played-plan")
	for _, tc := range []struct {
		id    string
		count int
	}{{"hg-season-work-6000", 100}, {"hg-group-2000", 300}} {
		for _, played := range []bool{true, false} {
			queries = nil
			started := time.Now()
			if err := e.MarkPlayed(t.Context(), "viewer", tc.id, played); err != nil {
				t.Fatal(err)
			}
			t.Logf("600k files: mark %s played=%v: %s", tc.id, played, time.Since(started))
			if len(queries) != 1 {
				t.Fatalf("mark expanded nodes repeatedly: %d reads", len(queries))
			}
			query := queries[0]
			for _, unused := range []string{"hongguo_artworks", "hongguo_user_states", "hongguo_favorites"} {
				if strings.Contains(query.sql, unused) {
					t.Fatalf("mark reads display data: %s", unused)
				}
			}
			var raw []byte
			if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query.sql, query.vars...).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				Plan          map[string]any
				ExecutionTime float64 `json:"Execution Time"`
				JIT           map[string]any
			}
			if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 || len(plans[0].JIT) != 0 {
				t.Fatalf("invalid or JIT-heavy mark plan: %s %v", raw, err)
			}
			visits := map[string]float64{}
			var inspect func(map[string]any)
			inspect = func(node map[string]any) {
				if relation, ok := node["Relation Name"].(string); ok {
					rows, _ := node["Actual Rows"].(float64)
					removed, _ := node["Rows Removed by Filter"].(float64)
					loops, _ := node["Actual Loops"].(float64)
					visits[relation] += (rows + removed) * loops
				}
				children, _ := node["Plans"].([]any)
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
			inspect(plans[0].Plan)
			for relation, rows := range visits {
				if rows > float64(tc.count*3+10) {
					t.Fatalf("mark scanned unrelated %s: %v visits", relation, rows)
				}
			}
			t.Logf("mark target read %s: %.3f ms visits=%v", tc.id, plans[0].ExecutionTime, visits)
		}
	}
}
