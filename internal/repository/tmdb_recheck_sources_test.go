package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sourceReadLogger struct {
	logger.Interface
	rows int64
}

func (l *sourceReadLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	if strings.Contains(sql, "FROM tmdb_subjects root") || strings.Contains(sql, "FROM targets t") {
		l.rows += rows
	}
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func TestTMDbRecheckSourcesLoadsOnlyTargetCoordinates(t *testing.T) {
	repos := tmdbTestRepo(t)
	root := model.TMDbSubject{Kind: "series", TMDbID: 42, Fields: `{}`}
	if err := repos.DB.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	for seasonNum := 1; seasonNum <= 3; seasonNum++ {
		season := model.TMDbSubject{Kind: "season", TMDbID: 100 + seasonNum, ParentID: &root.ID, Number: &seasonNum, Title: fmt.Sprintf("第%d季", seasonNum), Overview: "季简介", Fields: `{"poster_path":"/p.jpg"}`, Quality: 2}
		if err := repos.DB.Create(&season).Error; err != nil {
			t.Fatal(err)
		}
		episodes := make([]model.TMDbSubject, 0, 200)
		for episodeNum := 1; episodeNum <= 200; episodeNum++ {
			n := episodeNum
			episodes = append(episodes, model.TMDbSubject{Kind: "episode", TMDbID: seasonNum*1000 + n, ParentID: &season.ID, Number: &n, Title: fmt.Sprintf("中文标题%d", n), Overview: "集简介", ReleaseDate: "2026-01-01", Fields: `{"still_path":"/s.jpg"}`, Quality: 2})
		}
		if err := repos.DB.Create(&episodes).Error; err != nil {
			t.Fatal(err)
		}
	}
	log := &sourceReadLogger{Interface: logger.Default.LogMode(logger.Silent)}
	repo := New(repos.DB.Session(&gorm.Session{Logger: log})).Metadata
	for _, tc := range []struct {
		name   string
		states []TMDbRecheckState
		rows   int64
	}{
		{"one", []TMDbRecheckState{{TMDbMetadataRecheckCandidate: TMDbMetadataRecheckCandidate{SeriesTMDbID: "42", Kind: "episode", SeasonNum: 2, EpisodeNum: 17}}}, 1},
		{"mixed", []TMDbRecheckState{{TMDbMetadataRecheckCandidate: TMDbMetadataRecheckCandidate{SeriesTMDbID: "42", Kind: "episode", SeasonNum: 1, EpisodeNum: 3}}, {TMDbMetadataRecheckCandidate: TMDbMetadataRecheckCandidate{SeriesTMDbID: "42", Kind: "episode", SeasonNum: 3, EpisodeNum: 5}}, {TMDbMetadataRecheckCandidate: TMDbMetadataRecheckCandidate{SeriesTMDbID: "42", Kind: "season", SeasonNum: 2}}, {TMDbMetadataRecheckCandidate: TMDbMetadataRecheckCandidate{SeriesTMDbID: "42", Kind: "season", SeasonNum: 2}}}, 3},
		{"missing", []TMDbRecheckState{{TMDbMetadataRecheckCandidate: TMDbMetadataRecheckCandidate{SeriesTMDbID: "42", Kind: "episode", SeasonNum: 2, EpisodeNum: 201}}, {TMDbMetadataRecheckCandidate: TMDbMetadataRecheckCandidate{SeriesTMDbID: "invalid", Kind: "season", SeasonNum: 1}}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log.rows = 0
			if err := repo.loadTMDbRecheckSources(t.Context(), tc.states); err != nil {
				t.Fatal(err)
			}
			if log.rows != tc.rows {
				t.Fatalf("loaded %d source rows for %d coordinates, want %d", log.rows, len(tc.states), tc.rows)
			}
			for _, state := range tc.states {
				if tc.rows == 0 {
					if !state.SnapshotMissing || !state.BasicsMissing {
						t.Fatalf("missing source marked complete: %+v", state)
					}
					continue
				}
				title := fmt.Sprintf("中文标题%d", state.EpisodeNum)
				if state.Kind == "season" {
					title = fmt.Sprintf("第%d季", state.SeasonNum)
				}
				if state.Title != title || state.BasicsMissing || state.SnapshotMissing || state.ArtworkMissing {
					t.Fatalf("wrong source: %+v", state)
				}
			}
		})
	}
}
