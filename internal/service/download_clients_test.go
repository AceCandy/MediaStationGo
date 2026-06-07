package service

import (
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestDownloadClientCreateNormalizesHostAndClearsDefault(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadClient{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	svc := NewDownloadClientService(zap.NewNop(), repos)

	first, err := svc.Create(t.Context(), DownloadClientInput{
		Name:      "qB old",
		Type:      "qbittorrent",
		Host:      "http://127.0.0.1:8080/",
		IsDefault: true,
		Enabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(t.Context(), DownloadClientInput{
		Name:      "qB NAS",
		Type:      "qbittorrent",
		Host:      "172.17.0.1:8085",
		IsDefault: true,
		Enabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Host != "http://172.17.0.1:8085" {
		t.Fatalf("host = %q, want normalized http URL", second.Host)
	}
	refreshedFirst, err := repos.DownloadClient.FindByID(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshedFirst == nil || refreshedFirst.IsDefault {
		t.Fatalf("old default should be cleared, got %#v", refreshedFirst)
	}
	refreshedSecond, err := repos.DownloadClient.FindByID(t.Context(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshedSecond == nil || !refreshedSecond.IsDefault {
		t.Fatalf("new default should be active, got %#v", refreshedSecond)
	}
}

func TestDownloadClientRejectsUnsupportedHostScheme(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadClient{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	svc := NewDownloadClientService(zap.NewNop(), repository.New(db))

	if _, err := svc.Create(t.Context(), DownloadClientInput{
		Name:    "bad",
		Type:    "qbittorrent",
		Host:    "ftp://127.0.0.1:8080",
		Enabled: true,
	}); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}

func TestDownloadClientDeleteClearsLegacyQBitConnectionWhenNoDefault(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadClient{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	for key, value := range map[string]string{
		"qbittorrent.url":      "http://127.0.0.1:8080",
		"qbittorrent.username": "admin",
		"qbittorrent.password": "admin",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewDownloadClientService(zap.NewNop(), repos)
	row, err := svc.Create(t.Context(), DownloadClientInput{
		Name:      "qB",
		Type:      "qbittorrent",
		Host:      "http://127.0.0.1:8080",
		IsDefault: true,
		Enabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(t.Context(), row.ID); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"qbittorrent.url", "qbittorrent.username", "qbittorrent.password"} {
		value, err := repos.Setting.Get(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		if value != "" {
			t.Fatalf("%s = %q, want cleared", key, value)
		}
	}
}

func TestDownloadClientUpdateClearsLegacyQBitConnectionWhenDefaultDisabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadClient{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), "qbittorrent.url", "http://127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	svc := NewDownloadClientService(zap.NewNop(), repos)
	row, err := svc.Create(t.Context(), DownloadClientInput{
		Name:      "qB",
		Type:      "qbittorrent",
		Host:      "http://127.0.0.1:8080",
		IsDefault: true,
		Enabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Update(t.Context(), row.ID, DownloadClientInput{
		Name:      "qB",
		Type:      "qbittorrent",
		Host:      "http://127.0.0.1:8080",
		IsDefault: false,
		Enabled:   false,
	}); err != nil {
		t.Fatal(err)
	}

	value, err := repos.Setting.Get(t.Context(), "qbittorrent.url")
	if err != nil {
		t.Fatal(err)
	}
	if value != "" {
		t.Fatalf("qbittorrent.url = %q, want cleared", value)
	}
}
