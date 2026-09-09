package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"gorm.io/gorm/logger"
)

func TestSlowSQLLogIsolation(t *testing.T) {
	for _, debug := range []bool{false, true} {
		t.Run(map[bool]string{false: "production", true: "development"}[debug], func(t *testing.T) {
			cfg := &config.Config{}
			cfg.App.DataDir = t.TempDir()
			cfg.App.Debug = debug
			cfg.Logging.SlowSQLThresholdMS = 100
			slowLog, err := newSlowSQLLogger(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var app bytes.Buffer
			base := logger.New(log.New(&app, "", 0), logger.Config{LogLevel: logger.Warn})
			l := slowSQLLogger{Interface: base, log: slowLog, threshold: 100 * time.Millisecond}
			query := func() (string, int64) { return "SELECT * FROM media WHERE id = $1", 16 }
			l.Trace(t.Context(), time.Now().Add(-time.Second), query, nil)
			if app.Len() != 0 {
				t.Fatalf("slow query leaked to application logger: %s", app.String())
			}
			unexpected := func() (string, int64) { t.Error("unexpected SQL formatting"); return "", 0 }
			l.Trace(t.Context(), time.Now().Add(time.Hour), unexpected, nil)
			l.LogMode(logger.Silent).Trace(t.Context(), time.Now().Add(-time.Second), unexpected, nil)
			disabled := l
			disabled.threshold = 0
			disabled.Trace(t.Context(), time.Now().Add(-time.Second), unexpected, nil)
			l.Trace(t.Context(), time.Now().Add(-time.Second), query, errors.New("query failed"))
			if !strings.Contains(app.String(), "query failed") {
				t.Fatal("SQL error did not reach original logger")
			}
			sql, params := l.ParamsFilter(t.Context(), "SELECT $1", "private-value")
			if sql != "SELECT $1" || len(params) != 0 {
				t.Fatal("SQL parameters were not removed")
			}
			if err := slowLog.Sync(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(cfg.App.DataDir, "logs", "slow-sql.log"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(data), "\n") != 1 || !strings.Contains(string(data), `"rows":16`) || strings.Contains(string(data), "private-value") {
				t.Fatalf("unexpected slow log: %s", data)
			}
		})
	}
}

func TestSlowSQLDisabledDoesNotCreateFile(t *testing.T) {
	cfg := &config.Config{}
	cfg.App.DataDir = t.TempDir()
	l, err := newSlowSQLLogger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	l.Warn("disabled")
	_ = l.Sync()
	if _, err := os.Stat(filepath.Join(cfg.App.DataDir, "logs")); !os.IsNotExist(err) {
		t.Fatalf("disabled logger created log directory: %v", err)
	}
}
