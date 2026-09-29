package database

import (
	"testing"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestAutoMigrateRemovesTranscodeSettings(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	retired := []string{"transcode.enabled", "transcoder.enabled", "ffmpeg.path", "app.ffmpeg_path"}
	retained := []string{"strm.enabled", "playback.path_mappings", "ffprobe.path"}
	for _, key := range append(retired, retained...) {
		if err := db.Create(&model.Setting{Key: key, Value: "keep"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
	}
	assertUnscopedCount(t, db, &model.Setting{}, "key IN ?", retired, 0)
	assertUnscopedCount(t, db, &model.Setting{}, "key IN ?", retained, int64(len(retained)))
}

func assertUnscopedCount(t *testing.T, db *gorm.DB, value any, query string, arg any, want int64) {
	t.Helper()
	var got int64
	if err := db.Unscoped().Model(value).Where(query, arg).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count for %T where %q = %d, want %d", value, query, got, want)
	}
}
