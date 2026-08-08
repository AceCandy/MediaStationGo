// Package database wires up GORM against the configured database and exposes
// startup migration helpers.
package database

import (
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// Open initialises the configured PostgreSQL database.
func Open(cfg *config.Config, log *zap.Logger) (*gorm.DB, error) {
	return open(cfg, log, false)
}

// OpenForMigration avoids PostgreSQL prepared plans while schemas are changing.
func OpenForMigration(cfg *config.Config, log *zap.Logger) (*gorm.DB, error) {
	return open(cfg, log, true)
}

func open(cfg *config.Config, log *zap.Logger, migration bool) (*gorm.DB, error) {
	if cfg == nil {
		return nil, errors.New("database config is required")
	}
	dialector, err := databaseDialector(cfg, migration)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:                                   newGormLogger(log),
		PrepareStmt:                              !migration,
		DisableForeignKeyConstraintWhenMigrating: false,
	})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}
	if err := configureConnectionPool(db, cfg); err != nil {
		return nil, err
	}
	return db, nil
}

func newGormLogger(log *zap.Logger) logger.Interface {
	if log == nil {
		log = zap.NewNop()
	}
	return logger.New(
		zapStdLogger{log: log},
		logger.Config{
			SlowThreshold:             0,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}

func configureConnectionPool(db *gorm.DB, cfg *config.Config) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("gorm sqldb: %w", err)
	}
	if cfg.Database.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	}
	return nil
}

func databaseDialector(cfg *config.Config, migration bool) (gorm.Dialector, error) {
	databaseType := strings.ToLower(strings.TrimSpace(cfg.Database.Type))
	if databaseType != "postgres" && databaseType != "postgresql" && databaseType != "pg" {
		return nil, fmt.Errorf("unsupported database.type %q (supported: postgres)", cfg.Database.Type)
	}
	dsn := strings.TrimSpace(cfg.Database.DSN)
	if dsn == "" {
		return nil, errors.New("database.dsn is required")
	}
	return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: migration}), nil
}

// zapStdLogger adapts a *zap.Logger to GORM's tiny logger interface.
type zapStdLogger struct{ log *zap.Logger }

func (z zapStdLogger) Printf(format string, args ...interface{}) {
	if z.log == nil {
		return
	}
	z.log.Sugar().Infof(format, args...)
}
