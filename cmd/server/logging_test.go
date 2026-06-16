package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestProductionLoggerSplitsWarnAndErrorAndDropsInfo(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.App.DataDir = dir
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "json"
	cfg.Logging.OutputPath = filepath.Join(dir, "logs")
	cfg.Logging.EnableRotation = true
	cfg.Logging.MaxSizeMB = 1
	cfg.Logging.MaxBackups = 2

	log, err := newLogger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("info should be dropped")
	log.Warn("warning only", zap.String("kind", "warn"))
	log.Error("error only", zap.String("kind", "error"))
	_ = log.Sync()

	warnBytes, err := os.ReadFile(filepath.Join(dir, "logs", "warn.log"))
	if err != nil {
		t.Fatal(err)
	}
	errorBytes, err := os.ReadFile(filepath.Join(dir, "logs", "error.log"))
	if err != nil {
		t.Fatal(err)
	}
	warnLog := string(warnBytes)
	errorLog := string(errorBytes)
	if strings.Contains(warnLog, "info should be dropped") || strings.Contains(errorLog, "info should be dropped") {
		t.Fatal("info log should not be written in production")
	}
	if !strings.Contains(warnLog, "warning only") || strings.Contains(warnLog, "error only") {
		t.Fatalf("warn log not isolated: %s", warnLog)
	}
	if !strings.Contains(errorLog, "error only") || strings.Contains(errorLog, "warning only") {
		t.Fatalf("error log not isolated: %s", errorLog)
	}
}

func TestRotatingFileWriterCapsFileSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	writer, err := newRotatingFileWriter(path, config.LoggingConfig{
		EnableRotation: true,
		MaxSizeMB:      1,
		MaxBackups:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	chunk := strings.Repeat("x", 700*1024)
	if _, err := writer.Write([]byte(chunk)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(chunk)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated backup: %v", err)
	}
	_ = writer.Sync()
}
