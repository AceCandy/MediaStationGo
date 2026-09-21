package repository

import (
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestNFOHasMediaKeepsSelectivePlan(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{PrepareStmt: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"CREATE TABLE media (catalog_source text)",
		"CREATE INDEX idx_media_catalog_source ON media(catalog_source)",
		"INSERT INTO media SELECT '' FROM generate_series(1,200000)",
		"ANALYZE media",
		"SET plan_cache_mode = force_generic_plan",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := &NFORepository{db: db}
	if found, err := r.HasMedia(t.Context()); err != nil || found {
		t.Fatalf("without NFO: %v, %v", found, err)
	}
	if err := db.Exec("INSERT INTO media VALUES ('nfo')").Error; err != nil {
		t.Fatal(err)
	}
	if found, err := r.HasMedia(t.Context()); err != nil || !found {
		t.Fatalf("with NFO: %v, %v", found, err)
	}
	// 检查仓储实际准备的语句，避免测试一份与生产代码脱节的 SQL。
	var query string
	if err := db.Raw("SELECT statement FROM pg_prepared_statements WHERE statement LIKE 'SELECT EXISTS(SELECT 1 FROM media WHERE catalog_source%'").Scan(&query).Error; err != nil {
		t.Fatal(err)
	}
	if query == "" || strings.Contains(query, "$1") {
		t.Fatalf("source must be visible to planner: %s", query)
	}
	var plan []string
	if err := db.Raw("EXPLAIN (ANALYZE) " + query).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "idx_media_catalog_source") {
		t.Fatalf("missing selective index scan: %v", plan)
	}
}
