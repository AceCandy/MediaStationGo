package service

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestSubtitleDiscoverNoTracksReturnsEmptySlice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	media := model.Media{
		Title: "No Subtitles",
		Path:  filepath.Join(dir, "No Subtitles.mkv"),
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewSubtitleService(zap.NewNop(), repository.New(db))
	tracks, err := svc.Discover(t.Context(), media.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tracks == nil {
		t.Fatal("tracks is nil, want empty slice")
	}
	if len(tracks) != 0 {
		t.Fatalf("len(tracks) = %d, want 0", len(tracks))
	}
}
