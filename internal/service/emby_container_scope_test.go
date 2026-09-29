package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestEmbyContainerPlaybackBoundsHierarchy(t *testing.T) {
	svc := newTestEmbyService(t)
	db := svc.repo.DB
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'show-'||n,'series','Show','local' FROM generate_series(1,6000) n`,
		`INSERT INTO metadata_items(id,kind,parent_id,title,source,season_num) SELECT 'season-'||n||'-'||s,'season','show-'||n,'Season','local',s FROM generate_series(1,6000) n CROSS JOIN generate_series(0,1) s`,
		`INSERT INTO metadata_items(id,kind,parent_id,title,source,episode_num) SELECT 'ep-'||n||'-'||s,'episode','season-'||n||'-'||s,'Episode','local',1 FROM generate_series(1,6000) n CROSS JOIN generate_series(0,1) s`,
		`INSERT INTO media(id,metadata_id,library_id,path,season_num,episode_num) SELECT 'file-'||i.id||'-'||v,i.id,CASE WHEN v=1 THEN 'visible' ELSE 'hidden' END,'/fixture/'||i.id||'-'||v,1,1 FROM metadata_items i CROSS JOIN generate_series(1,2) v WHERE i.kind='episode'`,
		`INSERT INTO playback_histories(id,user_id,metadata_id,media_id,completed) VALUES ('watched','viewer','ep-1-0','file-ep-1-0-1',true),('other','other','ep-1-1','file-ep-1-1-1',true)`,
		`ANALYZE metadata_items`, `ANALYZE media`, `ANALYZE playback_histories`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc.visibilityCache = map[string]embyVisibilityCacheEntry{svc.repo.ReadCacheKey() + "viewer": {
		visibility: MediaVisibility{AllowedLibraryIDs: []string{"visible"}}, expiresAt: time.Now().Add(time.Hour),
	}}
	var query string
	var vars []any
	if err := db.Callback().Row().After("gorm:row").Register("test:container-scope", func(tx *gorm.DB) {
		if sql := tx.Statement.SQL.String(); !tx.DryRun && strings.Contains(sql, "BOOL_AND") {
			query, vars = sql, append([]any(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{"show-1"}, {"season-1-0"}, {"show-1", "season-1-0", "season-2-1", "show-1"}} {
		// 与原父子 OR 范围对照，包含重叠父子、跨剧、特别篇和受限版本。
		old := seriesScopeQuery(svc.applyUserMediaVisibility(t.Context(), db.Model(&model.Media{}), "viewer")).
			Where("emby_metadata.kind='episode'").Where("scope_season.id IN ? OR scope_series.id IN ?", ids, ids)
		var want, got []string
		if err := old.Order("media.id").Pluck("media.id", &want).Error; err != nil {
			t.Fatal(err)
		}
		if err := svc.containerEpisodeScope(t.Context(), "viewer", ids).Order("media.id").Pluck("media.id", &got).Error; err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("scope ids=%v got=%v want=%v err=%v", ids, got, want, err)
		}
		query = ""
		states := svc.playbackForContainers(t.Context(), "viewer", ids)
		for _, id := range ids {
			want := embyContainerPlayback{ID: id, UnplayedItemCount: 1}
			if id == "season-1-0" {
				want.Played, want.UnplayedItemCount = true, 0
			}
			if states[id] != want {
				t.Fatalf("state %s=%+v want=%+v", id, states[id], want)
			}
		}
		if query == "" {
			t.Fatal("did not capture container playback SQL")
		}
		var raw string
		if err := db.Statement.ConnPool.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+query, vars...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		type node struct {
			Relation string  `json:"Relation Name"`
			Rows     float64 `json:"Actual Rows"`
			Filtered float64 `json:"Rows Removed by Filter"`
			Loops    float64 `json:"Actual Loops"`
			Plans    []node  `json:"Plans"`
		}
		var plans []struct {
			Plan node
			Time float64 `json:"Execution Time"`
		}
		if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
			t.Fatalf("invalid plan: %v", err)
		}
		visits := map[string]float64{}
		var inspect func(node)
		inspect = func(n node) {
			if n.Relation != "" {
				visits[n.Relation] += (n.Rows + n.Filtered) * n.Loops
			}
			for _, child := range n.Plans {
				inspect(child)
			}
		}
		inspect(plans[0].Plan)
		for _, table := range []string{"metadata_items", "media", "playback_histories"} {
			if count, ok := visits[table]; !ok || count > 100 {
				t.Errorf("ids=%v %s visits=%v inspected=%v; must stay within page hierarchy", ids, table, count, ok)
			}
		}
		t.Logf("ids=%v playback %.3f ms visits=%v", ids, plans[0].Time, visits)
	}
}
