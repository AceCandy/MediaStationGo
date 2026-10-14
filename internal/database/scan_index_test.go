package database

import (
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestScanPagingIndexPlan(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasIndex("media", "idx_media_library_scan_path") {
		t.Fatal("scan paging index missing")
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, library_root_id, path)
 SELECT md5(i::text), 'library', 'root', '/media/' || lpad(i::text, 8, '0') || '.mkv'
 FROM generate_series(1, 20000) AS i`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ANALYZE media").Error; err != nil {
		t.Fatal(err)
	}
	var lines []string
	if err := db.Raw(`EXPLAIN (ANALYZE, FORMAT TEXT) SELECT id, path, library_root_id, part_group_key, part_index
 FROM media WHERE library_id = 'library' AND path > '/media/00019000.mkv'
 AND (library_root_id = 'root' OR library_root_id = '' OR library_root_id IS NULL)
 ORDER BY path LIMIT 500`).Scan(&lines).Error; err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(lines, "\n")
	if !strings.Contains(plan, "Index Scan") || strings.Contains(plan, "Seq Scan") || strings.Contains(plan, "Sort ") {
		t.Fatalf("unbounded page plan:\n%s", plan)
	}
	t.Log(plan)
}
