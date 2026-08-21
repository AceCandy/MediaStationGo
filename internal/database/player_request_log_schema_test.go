package database

import (
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestEnsurePlayerRequestLogSchemaAddsResponseBody(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE player_request_logs (
	id varchar(36) NOT NULL,
	requested_at timestamptz NOT NULL,
	method varchar(16) NOT NULL,
	route text NOT NULL,
	status integer NOT NULL,
	duration_ms bigint NOT NULL,
	ip varchar(64) NOT NULL DEFAULT '',
	path_params jsonb NOT NULL DEFAULT '{}'::jsonb,
	headers jsonb NOT NULL DEFAULT '{}'::jsonb,
	query jsonb NOT NULL DEFAULT '{}'::jsonb,
	body text NOT NULL DEFAULT '',
	PRIMARY KEY (id, requested_at)
) PARTITION BY RANGE (requested_at)`).Error; err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := ensurePlayerRequestLogSchema(db); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	if !db.Migrator().HasColumn("player_request_logs", "response_body") {
		t.Fatal("response_body column was not added")
	}
}
