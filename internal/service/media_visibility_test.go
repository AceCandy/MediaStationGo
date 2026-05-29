package service

import (
	"slices"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestMediaVisibilityFiltersNSFWAndLibraries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	libA := model.Library{Name: "电影", Path: "/media/movies", Type: "movie", Enabled: true}
	libB := model.Library{Name: "成人", Path: "/media/adult", Type: "movie", Enabled: true}
	if err := db.Create(&libA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&libB).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: libA.ID, Title: "普通电影", Path: "/media/movies/a.mkv"},
		{LibraryID: libA.ID, Title: "成人电影", Path: "/media/movies/b.mkv", NSFW: true},
		{LibraryID: libB.ID, Title: "限制媒体库电影", Path: "/media/adult/c.mkv"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	items, err := svc.SearchMediaVisible(t.Context(), "电影", 20, MediaVisibility{IncludeNSFW: false})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedMediaTitles(items); !slices.Equal(got, []string{"普通电影", "限制媒体库电影"}) {
		t.Fatalf("NSFW-filtered search = %#v", got)
	}

	items, err = svc.SearchMediaVisible(t.Context(), "电影", 20, MediaVisibility{
		IncludeNSFW:       true,
		AllowedLibraryIDs: []string{libA.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedMediaTitles(items); !slices.Equal(got, []string{"成人电影", "普通电影"}) {
		t.Fatalf("library-filtered search = %#v", got)
	}

	listed, total, err := svc.ListMediaVisible(t.Context(), libA.ID, 1, 20, MediaVisibility{IncludeNSFW: false})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(listed) != 1 || listed[0].Title != "普通电影" {
		t.Fatalf("NSFW-filtered list total=%d rows=%#v", total, sortedMediaTitles(listed))
	}
}

func sortedMediaTitles(rows []model.Media) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Title)
	}
	slices.Sort(out)
	return out
}
