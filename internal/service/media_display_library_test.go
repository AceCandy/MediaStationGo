package service

import (
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestAttachLibraryMetadataKeepsOwnedDisplayLibrary(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.LibraryRoot{}, &model.Media{})
	repos := repository.New(db)

	source := model.Library{Name: "下载目录", Path: "/media/downloads", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &source); err != nil {
		t.Fatal(err)
	}

	adult := model.Library{Name: "成人", Path: "/media/adult", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &adult); err != nil {
		t.Fatal(err)
	}
	mediaPath := "/media/downloads/Some.Movie.2024/Some.Movie.2024.mp4"
	if err := repos.Media.Upsert(t.Context(), &model.Media{
		LibraryID: adult.ID,
		Title:     "Some Movie",
		Path:      mediaPath,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	items := []model.Media{{LibraryID: adult.ID, Title: "Some Movie", Path: mediaPath}}
	svc.attachLibraryMetadata(t.Context(), items)

	got := items[0]
	if got.DisplayLibraryID != adult.ID {
		t.Fatalf("display_library_id = %s (%s), want auto-category library %s (成人)",
			got.DisplayLibraryID, got.DisplayLibraryName, adult.ID)
	}
	if got.DisplayLibraryName != "成人" {
		t.Fatalf("display_library_name = %q, want 成人", got.DisplayLibraryName)
	}
	if got.LibraryName != "成人" {
		t.Fatalf("library_name = %q, want 成人 (not the source path library)", got.LibraryName)
	}
}

func TestMediaViewsAsMediaKeepsParentSeriesTitle(t *testing.T) {
	rows := mediaViewsAsMedia([]model.MediaView{{
		Media:       model.Media{Title: "raw title"},
		Title:       "Episode title",
		SeriesTitle: "Series title",
	}})
	if len(rows) != 1 || rows[0].Title != "Episode title" || rows[0].SeriesTitle != "Series title" {
		t.Fatalf("media projection = %#v", rows)
	}
}
