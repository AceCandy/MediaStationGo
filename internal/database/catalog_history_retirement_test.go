package database

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestCatalogHistoryRetirementPreservesCentersAndBindings(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	item := model.MetadataItem{Kind: "movie", Title: "本地资料", Source: "tmdb"}
	subject := model.TMDbSubject{Kind: "movie", TMDbID: 12, Title: "中心资料", Fields: "{}"}
	if err = db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&subject).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.MetadataIdentifier{MetadataID: item.ID, Provider: "tmdb", EntityKind: "movie", ExternalID: "12"}
	payload := model.TMDbPayload{SubjectID: &subject.ID, Component: "detail", Language: "und", Body: `{"id":12,"title":"中心资料"}`, Origin: "api", Quality: 3, FetchedAt: time.Now()}
	raw := `{"id":"99","title":"豆瓣"}`
	at := time.Now()
	douban := model.DoubanSubject{DoubanID: "99", EntityKind: "movie", Payload: &raw, FetchedAt: &at}
	for _, row := range []any{&binding, &payload, &douban} {
		if err = db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE metadata_provider_snapshots(id text PRIMARY KEY,metadata_id text REFERENCES metadata_items(id),payload jsonb)`,
		`CREATE TABLE tmdb_migration_failures(stage text,source_id text)`,
		`CREATE TRIGGER tmdb_recheck_snapshots AFTER INSERT ON metadata_provider_snapshots FOR EACH ROW EXECUTE FUNCTION tmdb_recheck_changed()`,
		`INSERT INTO metadata_provider_snapshots VALUES ('old','` + item.ID + `','{}')`,
		`INSERT INTO tmdb_migration_failures VALUES ('snapshots','old')`,
		`INSERT INTO settings(key,value) VALUES ('metadata.tmdb_center_migration_v1','complete'),('internal.tmdb_snapshot_backfill_completed','true')`,
	} {
		if err = db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err = AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"metadata_provider_snapshots", "tmdb_migration_failures", "provider_data"} {
			if db.Migrator().HasTable(table) {
				t.Fatalf("retired table rebuilt: %s", table)
			}
		}
	}
	for _, row := range []any{&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.TMDbSubject{}, &model.TMDbPayload{}, &model.DoubanSubject{}} {
		var n int64
		if err = db.Model(row).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("center/local rows lost: %T count=%d err=%v", row, n, err)
		}
	}
	var n int64
	if err = db.Model(&model.Setting{}).Where("key IN ?", []string{"metadata.tmdb_center_migration_v1", "internal.tmdb_snapshot_backfill_completed"}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("retired settings remain: %d %v", n, err)
	}
}
