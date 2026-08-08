// Package testdb provides PostgreSQL-backed test databases.
package testdb

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const postgresTestDSNEnv = "MEDIASTATION_TEST_POSTGRES_DSN"

// TestTB is the subset of testing.TB required by OpenPostgres.
type TestTB interface {
	Helper()
	Skipf(format string, args ...any)
	Cleanup(func())
}

// OpenPostgres opens an isolated schema in the configured PostgreSQL test database.
func OpenPostgres(t TestTB, gormConfig *gorm.Config) (*gorm.DB, error) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(postgresTestDSNEnv))
	if dsn == "" {
		t.Skipf("set %s to run PostgreSQL tests", postgresTestDSNEnv)
		return nil, errors.New("PostgreSQL test DSN is required")
	}
	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	schema := "test_" + hex.EncodeToString(random)
	if err := db.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := db.Exec(`SET search_path TO "` + schema + `"`).Error; err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		cleanupDB, cleanupErr := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if cleanupErr == nil {
			_ = cleanupDB.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error
			if cleanupSQLDB, openErr := cleanupDB.DB(); openErr == nil {
				_ = cleanupSQLDB.Close()
			}
		}
	})
	return db, nil
}
