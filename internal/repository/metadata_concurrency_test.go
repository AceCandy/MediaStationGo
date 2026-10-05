package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCanonicalConcurrentIdentityReuse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		merge [2]bool
	}{
		{"ordinary", [2]bool{false, false}},
		{"explicit_merge", [2]bool{true, true}},
		{"mixed", [2]bool{false, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}); err != nil {
				t.Fatal(err)
			}
			var schema string
			if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(3)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			workers := make([]*MetadataRepository, 2)
			for i := range workers {
				conn, err := pool.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if _, err := conn.ExecContext(ctx, `SET search_path TO "`+schema+`"`); err != nil {
					t.Fatal(err)
				}
				worker, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
				if err != nil {
					t.Fatal(err)
				}
				workers[i] = &MetadataRepository{db: worker}
			}
			var secondPID int
			if err := workers[1].db.Raw("SELECT pg_backend_pid()").Scan(&secondPID).Error; err != nil {
				t.Fatal(err)
			}
			// 只暂停第一个创建者；第二个可等待身份锁，不能要求两者都进入创建阶段。
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			if err := workers[0].db.Callback().Create().Before("gorm:create").Register("pause_first_work", func(tx *gorm.DB) {
				if tx.Statement.Table != "metadata_items" {
					return
				}
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					tx.AddError(ctx.Err())
				}
			}); err != nil {
				t.Fatal(err)
			}
			type result struct {
				item *model.MetadataItem
				err  error
			}
			results := []chan result{make(chan result, 1), make(chan result, 1)}
			save := func(i int) {
				ids := []model.MetadataIdentifier{
					{Provider: "tmdb", EntityKind: "series", ExternalID: "334888"},
					{Provider: "imdb", EntityKind: "series", ExternalID: "tt45584297"},
					{Provider: "thetvdb", EntityKind: "series", ExternalID: "482482"},
				}
				if i == 1 {
					ids[0].Provider, ids[0].ExternalID = "TMDB", "0334888"
					ids[0], ids[2] = ids[2], ids[0]
				}
				item, err := workers[i].UpsertCanonicalWithMerge(ctx, &model.MetadataItem{Kind: "series", Title: "Shared work", Source: "tmdb"}, ids, "", tc.merge[i])
				results[i] <- result{item, err}
			}
			go save(0)
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			go save(1)
			var second result
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
		waiting:
			for {
				select {
				case second = <-results[1]:
					unblock()
					break waiting
				case <-ticker.C:
					var blocked bool
					if err := db.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = ? AND NOT granted)", secondPID).Scan(&blocked).Error; err != nil {
						t.Fatal(err)
					}
					if blocked {
						unblock()
						second = <-results[1]
						break waiting
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			first := <-results[0]
			if first.err != nil || second.err != nil {
				t.Fatalf("concurrent upsert errors: %v / %v", first.err, second.err)
			}
			if first.item.ID != second.item.ID {
				t.Fatalf("same external identity created two works: %s / %s", first.item.ID, second.item.ID)
			}
			var works int64
			if err := workers[0].db.Model(&model.MetadataItem{}).Count(&works).Error; err != nil || works != 1 {
				t.Fatalf("work count = %d, err = %v", works, err)
			}
			var ids []model.MetadataIdentifier
			if err := workers[0].db.Find(&ids).Error; err != nil || len(ids) != 3 {
				t.Fatalf("identifiers = %d, err = %v", len(ids), err)
			}
			for _, id := range ids {
				if id.MetadataID != first.item.ID {
					t.Fatalf("identifier belongs to another work: %#v", id)
				}
			}
		})
	}
}

func TestCanonicalRejectsLateIdentifierConflict(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}); err != nil {
		t.Fatal(err)
	}
	existing := model.MetadataItem{Kind: "series", Title: "Existing work", Source: "tmdb"}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	// 注入解析后才出现的冲突，验证未遵守身份锁的写入也不能被静默改绑。
	if err := db.Callback().Create().Before("gorm:create").Register("late_identifier_conflict", func(tx *gorm.DB) {
		if tx.Statement.Table == "metadata_identifiers" {
			tx.AddError(tx.Exec(`INSERT INTO metadata_identifiers (id, metadata_id, provider, entity_kind, external_id)
VALUES ('late-conflict', ?, 'tmdb', 'series', '334888')`, existing.ID).Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, err = (&MetadataRepository{db: db}).UpsertCanonical(t.Context(),
		&model.MetadataItem{Kind: "series", Title: "Incoming work", Source: "tmdb"},
		[]model.MetadataIdentifier{{Provider: "tmdb", EntityKind: "series", ExternalID: "334888"}}, "")
	if err == nil {
		t.Fatal("late identity conflict was silently accepted")
	}
	var items []model.MetadataItem
	if err := db.Find(&items).Error; err != nil || len(items) != 1 || items[0].ID != existing.ID || items[0].Title != existing.Title {
		t.Fatalf("failed upsert did not roll back: %#v, err=%v", items, err)
	}
}
