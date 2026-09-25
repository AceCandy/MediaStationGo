package service

import (
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestMovieLibraryEpisodicExists(t *testing.T) {
	e := newTestEmbyService(t)
	var query string
	if err := e.repo.DB.Callback().Row().After("gorm:row").Register("test:movie-exists", func(tx *gorm.DB) {
		query = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatal(err)
	}
	check := func(library string, want bool) {
		t.Helper()
		got, err := e.movieLibraryHasEpisodicContent(t.Context(), library)
		if err != nil || got != want || !strings.HasPrefix(query, "SELECT EXISTS (") || strings.Contains(strings.ToUpper(query), "COUNT(") {
			t.Fatalf("library=%s got=%v want=%v err=%v query=%s", library, got, want, err, query)
		}
	}
	check("movies", false)
	for i, path := range []string{"/fixture/movie.mkv", "/fixture/Show/Season 01/episode.mkv"} {
		m := model.Media{LibraryID: "movies", Path: path, SeasonNum: 1, EpisodeNum: 1}
		if err := e.repo.DB.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
		check("movies", i == 1)
	}
	check("other", false)
}
