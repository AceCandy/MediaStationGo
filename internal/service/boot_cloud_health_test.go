package service

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestCloudStorageMissingConfigReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("115: missing cookie"), "missing_cookie"},
		{errors.New("clouddrive2: missing WebDAV URL"), "missing_webdav_url"},
		{errors.New("quark: token expired"), ""},
	}
	for _, tc := range cases {
		if got := cloudStorageMissingConfigReason(tc.err); got != tc.want {
			t.Fatalf("reason(%q) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestWarnMissingCloudStorageConfigOncePersistsMarker(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	core, observed := observer.New(zap.WarnLevel)
	c := &Container{
		Log:  zap.New(core),
		Repo: repository.New(db),
	}
	err = errors.New("115: missing cookie")

	if !c.warnMissingCloudStorageConfigOnce(context.Background(), "cloud115", err) {
		t.Fatal("missing config should be handled")
	}
	if !c.warnMissingCloudStorageConfigOnce(context.Background(), "cloud115", err) {
		t.Fatal("missing config should still be classified on second call")
	}
	if observed.FilterMessage("boot: cloud storage config incomplete; skipping health check").Len() != 1 {
		t.Fatalf("warn count = %d, want 1", observed.Len())
	}
	if c.warnMissingCloudStorageConfigOnce(context.Background(), "cloud115", errors.New("network timeout")) {
		t.Fatal("non-missing config error should not be swallowed")
	}
}
