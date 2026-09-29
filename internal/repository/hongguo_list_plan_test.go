package repository

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestHongGuoLibraryCandidatePlan(t *testing.T) {
	repos := newMediaViewTestRepositories(t)
	db, ctx := repos.DB, t.Context()
	if err := db.Create(&model.Library{Base: model.Base{ID: "library"}, Name: "红果", Path: "/fixture/hg-plan", Type: model.LibraryTypeHongGuo}).Error; err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO hongguo_works(id,source_id,kind,title,related_album_id,season_index,refreshed_at)
SELECT 'work-'||n,n::text,'series','Title '||n,((n-1)/3+1000)::text,(n-1)%3+1,now() FROM generate_series(1,6000) n`,
		`INSERT INTO hongguo_episodes(id,work_id,number) SELECT 'ep-'||id,id,1 FROM hongguo_works`,
		`INSERT INTO media(id,path,library_id,catalog_source) SELECT 'file-'||id,'/fixture/hg-plan/'||id,'library','hongguo' FROM hongguo_works`,
		`INSERT INTO hongguo_media_bindings(media_id,work_id,episode_id) SELECT 'file-'||id,id,'ep-'||id FROM hongguo_works`,
		`ANALYZE hongguo_works`, `ANALYZE hongguo_media_bindings`, `ANALYZE media`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var query string
	var vars []any
	if err := db.Callback().Row().After("gorm:row").Register("test:hg-web-plan", func(tx *gorm.DB) {
		if sql := tx.Statement.SQL.String(); !tx.DryRun && strings.Contains(sql, "page_works AS (") {
			query, vars = sql, append([]any(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, summaries, total, err := repos.MediaView.ListLibraryMetadataPage(ctx, "library", "series", "", 0, 3, MediaQueryFilter{})
	if err != nil || total != 2000 || len(summaries) != 3 || query == "" {
		t.Fatalf("summaries=%v total=%d captured=%v err=%v", summaries, total, query != "", err)
	}
	// 保存旧候选作结果与计划对照，不用新查询生成期望值。
	visible := repos.MediaView.hongGuoFileScope(ctx, "library", MediaQueryFilter{}).Select("1").Where("b.work_id = w.id")
	oldScope := db.Table("hongguo_works w").Joins(HongGuoAlbumJoin).
		Joins("JOIN hongguo_works primary_work ON primary_work.id = COALESCE(g.work_id,w.id)").
		Where("CASE WHEN w.library_ids IS NULL THEN EXISTS (? OFFSET 0) ELSE TRUE END", visible)
	oldScope = FilterVisibleWorkLibraries(db, oldScope, "w.library_ids", []string{"library"}, MediaQueryFilter{}).
		Select("w.id AS work_id, " + hongGuoSeriesIdentity + " AS metadata_id, primary_work.title")
	oldPage := db.Table("works").Order("title, metadata_id").Limit(3)
	old := db.Session(&gorm.Session{DryRun: true}).Raw(`WITH scoped AS MATERIALIZED (?), works AS MATERIALIZED (
SELECT metadata_id,title FROM scoped GROUP BY metadata_id,title), page AS MATERIALIZED (?), page_works AS (
SELECT scoped.work_id,page.metadata_id,page.title FROM page JOIN scoped USING(metadata_id))
SELECT page_works.work_id,totals.total FROM (SELECT COUNT(*) AS total FROM works) totals
LEFT JOIN page_works ON TRUE ORDER BY page_works.title,page_works.metadata_id`, oldScope, oldPage).Statement
	var expected []string
	for phase, statement := range []struct {
		sql  string
		vars []any
	}{{old.SQL.String(), old.Vars}, {query, vars}} {
		rows, err := db.Statement.ConnPool.QueryContext(ctx, statement.sql, statement.vars...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		var cards []string
		for rows.Next() {
			var id string
			var n int64
			if err := rows.Scan(&id, &n); err != nil {
				t.Fatal(err)
			}
			if n != total {
				t.Fatalf("phase=%d total=%d want=%d", phase, n, total)
			}
			got = append(got, id)
			number, err := strconv.Atoi(strings.TrimPrefix(id, "work-"))
			if err != nil {
				t.Fatal(err)
			}
			card := "hg-group-" + strconv.Itoa((number-1)/3+1000)
			if len(cards) == 0 || cards[len(cards)-1] != card {
				cards = append(cards, card)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(cards) != len(summaries) {
			t.Fatalf("phase=%d cards=%v summaries=%v", phase, cards, summaries)
		}
		for i, card := range cards {
			if summaries[i].MetadataID != card {
				t.Fatalf("phase=%d page order differs: cards=%v summaries=%v", phase, cards, summaries)
			}
		}
		sort.Strings(got)
		if phase == 0 {
			expected = got
		} else if !reflect.DeepEqual(got, expected) {
			t.Fatalf("page members differ: %v / %v", got, expected)
		}
		var raw string
		if err := db.Statement.ConnPool.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+statement.sql, statement.vars...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		type node struct {
			Relation string  `json:"Relation Name"`
			Rows     float64 `json:"Actual Rows"`
			Removed  float64 `json:"Rows Removed by Filter"`
			Loops    float64 `json:"Actual Loops"`
			Plans    []node
		}
		var plans []struct {
			Plan node
			Time float64 `json:"Execution Time"`
		}
		if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
			t.Fatalf("invalid plan: %v", err)
		}
		var visits float64
		var inspect func(node)
		inspect = func(n node) {
			if n.Relation == "hongguo_works" {
				visits += (n.Rows + n.Removed) * n.Loops
			}
			for _, child := range n.Plans {
				inspect(child)
			}
		}
		inspect(plans[0].Plan)
		if phase == 1 && (visits == 0 || visits > 12000) {
			t.Fatalf("candidate repeats album/primary work lookups: visits=%.0f", visits)
		}
		t.Logf("web library phase=%d execution=%.3f ms work visits=%.0f", phase, plans[0].Time, visits)
	}
}
