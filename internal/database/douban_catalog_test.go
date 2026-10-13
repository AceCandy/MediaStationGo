package database

import (
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestDoubanCatalogProjectionExistingRowsAndWrites(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	payload := `{"subject":{"name":"已有作品","year":2026,"genres":["剧情",{"name":"恐怖"}]}}`
	summary := `{"title":"榜单摘要","year":"2025","genres":["喜剧"]}`
	row := model.DoubanSubject{DoubanID: "100", EntityKind: "movie", Payload: &payload, Summary: &summary}
	if err = db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	check := func(title, year, genres string) {
		t.Helper()
		var got struct{ Title, Year, Genres string }
		if err := db.Raw(`SELECT list_title AS title,list_year AS year,array_to_string(list_genres, ',') AS genres FROM douban_subjects WHERE id=?`, row.ID).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got.Title != title || got.Year != year || got.Genres != genres {
			t.Fatalf("projection=%+v", got)
		}
	}
	for i := 0; i < 2; i++ {
		if err = EnsureDoubanCatalogSchema(db); err != nil {
			t.Fatal(err)
		}
		check("已有作品", "2026", "剧情,恐怖")
		if err = db.AutoMigrate(&model.DoubanSubject{}); err != nil {
			t.Fatal(err)
		}
	}
	db = db.Session(&gorm.Session{PrepareStmt: true})
	if err = db.Model(&row).UpdateColumn("payload", gorm.Expr("?::jsonb", `{"data":{"title":"新资料","year":"2027","genres":[" "],"genre":"悬疑 / 犯罪"}}`)).Error; err != nil {
		t.Fatal(err)
	}
	check("新资料", "2027", "悬疑,犯罪")
	if err = db.Model(&row).UpdateColumn("payload", nil).Error; err != nil {
		t.Fatal(err)
	}
	check("榜单摘要", "2025", "喜剧")
	if err = db.Model(&row).UpdateColumn("summary", nil).Error; err != nil {
		t.Fatal(err)
	}
	check("", "", "")
	// 原始响应写入之外的管理更新也不能破坏生成字段。
	if err = db.Model(&row).UpdateColumn("supplement_pending", true).Error; err != nil {
		t.Fatal(err)
	}
	check("", "", "")
}

func TestDoubanCatalogListIndexesAndPlans(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.DoubanSubject{}); err != nil {
		t.Fatal(err)
	}
	if err = EnsureDoubanCatalogSchema(db); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`INSERT INTO douban_subjects(id,douban_id,entity_kind,payload,fetched_at)
 SELECT n::text,n::text,'movie',jsonb_build_object('title','电影 '||n,'genres',jsonb_build_array(CASE WHEN n%1000=0 THEN '恐怖' ELSE '剧情' END),'description',repeat(md5(n::text),1000)),now()-n*interval '1 second' FROM generate_series(1,10000) n`).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`ANALYZE douban_subjects`).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ query, index string }{
		{`SELECT id,list_title,list_year FROM douban_subjects WHERE entity_kind='movie' ORDER BY fetched_at DESC NULLS LAST,id DESC LIMIT 30`, "idx_douban_catalog_list"},
		{`SELECT count(*) FROM douban_subjects WHERE entity_kind='movie' AND list_genres @> ARRAY['恐怖']::text[]`, "idx_douban_catalog_genres"},
	} {
		var plan []string
		if err = db.Raw("EXPLAIN (ANALYZE, BUFFERS) " + test.query).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		text := strings.Join(plan, "\n")
		t.Log(text)
		if !strings.Contains(text, test.index) {
			t.Fatalf("missing index %s: %s", test.index, text)
		}
	}
}
