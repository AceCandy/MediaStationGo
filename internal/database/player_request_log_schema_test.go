package database

import (
	"os"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestEnsurePlayerRequestLogSchemaPreservesSequenceAcrossPartitions(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ensurePlayerRequestLogSchema(db); err != nil {
		t.Fatal(err)
	}

	if err := EnsurePlayerRequestLogPartitions(db, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO player_request_logs (id, requested_at, method, route, status, duration_ms)
VALUES ('january', '2026-01-15', 'GET', '/test', 200, 1),
       ('february', '2026-02-15', 'GET', '/test', 200, 1)`).Error; err != nil {
		t.Fatal(err)
	}
	var first []int64
	for i := 0; i < 2; i++ {
		if err := ensurePlayerRequestLogSchema(db); err != nil {
			t.Fatalf("schema initialization %d: %v", i+1, err)
		}
		var numbers []int64
		if err := db.Table("player_request_logs").Order("id").Pluck("serial_no", &numbers).Error; err != nil {
			t.Fatal(err)
		}
		if len(numbers) != 2 || numbers[0] <= 0 || numbers[1] <= 0 || numbers[0] == numbers[1] {
			t.Fatalf("persisted serial numbers: %v", numbers)
		}
		if i == 0 {
			first = numbers
		} else if numbers[0] != first[0] || numbers[1] != first[1] {
			t.Fatalf("schema initialization changed serial numbers: %v -> %v", first, numbers)
		}
	}
	if !db.Migrator().HasColumn("player_request_logs", "response_body") {
		t.Fatal("response_body column was not added")
	}
	var next int64
	if err := db.Raw(`INSERT INTO player_request_logs (id, requested_at, method, route, status, duration_ms)
VALUES ('new', '2026-02-16', 'GET', '/test', 200, 1) RETURNING serial_no`).Scan(&next).Error; err != nil {
		t.Fatal(err)
	}
	if next <= first[0] || next <= first[1] {
		t.Fatalf("new number %d does not follow persisted numbers %v", next, first)
	}
	// 两个独立连接在事务尚未提交时取号，验证不会重复或因回滚复用。
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
	tx := db.Begin()
	defer tx.Rollback()
	var uncommitted, concurrent int64
	if err := tx.Raw(`INSERT INTO player_request_logs (id, requested_at, method, route, status, duration_ms)
VALUES ('rollback', '2026-01-16', 'GET', '/test', 200, 1) RETURNING serial_no`).Scan(&uncommitted).Error; err != nil {
		t.Fatal(err)
	}
	if err := peer.Raw(`INSERT INTO player_request_logs (id, requested_at, method, route, status, duration_ms)
VALUES ('concurrent', '2026-02-17', 'GET', '/test', 200, 1) RETURNING serial_no`).Scan(&concurrent).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	var following int64
	if err := db.Raw(`SELECT nextval('player_request_logs_serial_no_seq')`).Scan(&following).Error; err != nil {
		t.Fatal(err)
	}
	if uncommitted <= next || concurrent <= uncommitted || following <= concurrent {
		t.Fatalf("sequence did not advance: %d, %d, %d, %d", next, uncommitted, concurrent, following)
	}
}
