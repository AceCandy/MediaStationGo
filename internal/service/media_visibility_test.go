package service

import (
	"fmt"
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
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}); err != nil {
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
	if err := repos.Setting.Set(t.Context(), AdultLibraryIDsSettingKey, `["`+libB.ID+`"]`); err != nil {
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

	hiddenAdultLibraries := AdultLibraryIDs(t.Context(), repos)
	items, err := svc.SearchMediaVisible(t.Context(), "电影", 20, MediaVisibility{
		IncludeNSFW:      false,
		HiddenLibraryIDs: hiddenAdultLibraries,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedMediaTitles(items); !slices.Equal(got, []string{"普通电影"}) {
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

	listed, total, err := svc.ListMediaVisible(t.Context(), libA.ID, 1, 20, MediaVisibility{
		IncludeNSFW:      false,
		HiddenLibraryIDs: hiddenAdultLibraries,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(listed) != 1 || listed[0].Title != "普通电影" {
		t.Fatalf("NSFW-filtered list total=%d rows=%#v", total, sortedMediaTitles(listed))
	}

	listed, total, err = svc.ListMediaVisible(t.Context(), libB.ID, 1, 20, MediaVisibility{
		IncludeNSFW:      false,
		HiddenLibraryIDs: hiddenAdultLibraries,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(listed) != 0 {
		t.Fatalf("adult library should be hidden total=%d rows=%#v", total, sortedMediaTitles(listed))
	}
}

func TestConfiguredAdultLibrariesDoNotHideSafeLibraryWithNSFWItems(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Library{}, &model.Media{}, &model.Setting{}, &model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)

	safe := model.Library{Name: "电影", Path: "/media/movie", Type: "movie", Enabled: true}
	adult := model.Library{Name: "9KG", Path: "/media/9KG", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &safe); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &adult); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), AdultLibraryIDsSettingKey, `["`+adult.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]model.Media{
		{LibraryID: safe.ID, Title: "普通电影", Path: "/media/movie/a.mkv"},
		{LibraryID: safe.ID, Title: "误入普通库的成人条目", Path: "/media/movie/b.mkv", NSFW: true},
		{LibraryID: adult.ID, Title: "成人影片", Path: "/media/9KG/c.mkv"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	viewer := &model.User{Username: "viewer", PasswordHash: "hash", Role: "user", HideAdult: true}
	if err := repos.User.Create(t.Context(), viewer); err != nil {
		t.Fatal(err)
	}

	visibility := UserDefaultMediaVisibility(t.Context(), repos, viewer.ID)
	if LibraryVisibleForUser(t.Context(), repos, safe, visibility) != true {
		t.Fatal("configured adult libraries should not hide a safe library just because it contains NSFW items")
	}
	if LibraryVisibleForUser(t.Context(), repos, adult, visibility) != false {
		t.Fatal("configured adult library should be hidden when the user hides adult content")
	}

	items, err := NewMediaService(&config.Config{}, zap.NewNop(), repos).
		SearchMediaVisible(t.Context(), "电影", 20, visibility)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedMediaTitles(items); !slices.Equal(got, []string{"普通电影"}) {
		t.Fatalf("safe library should stay visible while NSFW media is filtered, got %#v", got)
	}
}

func TestSearchMediaVisibleHonorsLargePosterWallLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "海报墙", Path: "/media/all", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	rows := make([]model.Media, 260)
	for i := range rows {
		rows[i] = model.Media{
			LibraryID: lib.ID,
			Title:     fmt.Sprintf("节目 %03d", i),
			Path:      fmt.Sprintf("/media/all/show-%03d.mkv", i),
		}
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	items, err := NewMediaService(&config.Config{}, zap.NewNop(), repos).
		SearchMediaVisible(t.Context(), "", 240, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 240 {
		t.Fatalf("large poster wall search returned %d rows, want 240", len(items))
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
