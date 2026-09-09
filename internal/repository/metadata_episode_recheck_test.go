package repository

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestSaveTMDbMetadataRecheckOnlyWritesChildDisplayFields(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}); err != nil {
		t.Fatal(err)
	}
	repos := New(db)
	backend := &recordingMetadataSearchBackend{}
	repos.Media.SetSearchBackend(backend)
	now := time.Now().UTC().Truncate(time.Microsecond)
	parent := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{model.MetadataKindSeason, model.MetadataKindEpisode} {
		item := model.MetadataItem{Kind: kind, Title: "Old", Source: "local", ParentID: &parent.ID}
		if kind == model.MetadataKindSeason {
			item.SeasonNum = 1
		} else {
			item.EpisodeNum = 1
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		before := item
		item.Title, item.Overview, item.ReleaseDate, item.Year, item.Rating = "New", "Overview", "2026-09-07", 2026, 8
		item.Source = "tmdb"
		if kind == model.MetadataKindSeason {
			item.SeasonNum = 99
		} else {
			item.EpisodeNum = 99
		}
		item.TMDbSeasonCheckedAt, item.TMDbEpisodeCheckedAt = &now, &now
		if err := repos.Metadata.SaveTMDbMetadataRecheck(t.Context(), &item); err != nil {
			t.Fatal(err)
		}
		var stored model.MetadataItem
		if err := db.First(&stored, "id = ?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Title != "New" || stored.Overview != "Overview" || stored.ReleaseDate != "2026-09-07" || stored.Year != 2026 || stored.Rating != 8 || stored.Source != "local" || stored.SeasonNum != before.SeasonNum || stored.EpisodeNum != before.EpisodeNum {
			t.Fatalf("unexpected recheck projection: %+v", stored)
		}
		checked, other := stored.TMDbSeasonCheckedAt, stored.TMDbEpisodeCheckedAt
		if kind == model.MetadataKindEpisode {
			checked, other = other, checked
		}
		if checked == nil || !checked.Equal(now) || other != nil {
			t.Fatalf("wrong checkpoint: %+v", stored)
		}
		old := now.Add(-time.Hour)
		item.TMDbSeasonCheckedAt, item.TMDbEpisodeCheckedAt = &old, &old
		if err := repos.Metadata.SaveTMDbMetadataRecheck(t.Context(), &item); err != nil {
			t.Fatal(err)
		}
		var latest model.MetadataItem
		if err := db.First(&latest, "id = ?", item.ID).Error; err != nil || !reflect.DeepEqual(latest.TMDbSeasonCheckedAt, stored.TMDbSeasonCheckedAt) || !reflect.DeepEqual(latest.TMDbEpisodeCheckedAt, stored.TMDbEpisodeCheckedAt) {
			t.Fatal("older recheck rolled checkpoint back", err)
		}
		parent = before
	}
	if len(backend.upserts) != 0 || len(backend.deletes) != 0 {
		t.Fatal("child recheck refreshed top-level search documents")
	}
	if err := repos.Metadata.SaveTMDbMetadataRecheck(t.Context(), &parent); err == nil {
		t.Fatal("missing check time must be rejected")
	}
	if err := db.Delete(&model.MetadataItem{}, "id = ?", parent.ID).Error; err != nil {
		t.Fatal(err)
	}
	parent.TMDbEpisodeCheckedAt = &now
	if err := repos.Metadata.SaveTMDbMetadataRecheck(t.Context(), &parent); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted metadata must not be recreated: %v", err)
	}
	if err := repos.Metadata.SaveTMDbMetadataRecheck(t.Context(), &model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series"}); err == nil {
		t.Fatal("top-level metadata must not bypass search synchronization")
	}
}

func TestListTMDbEpisodeMetadataRecheckAfterFiltersAndPages(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.Media{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	series := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000001"}, Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}
	season := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000002"}, Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "tmdb"}
	generated := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000003"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode 1", ReleaseDate: "2026-08-01", Source: "tmdb"}
	missingDate := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000004"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 2, Title: "Real title", Overview: "Overview", Source: "tmdb"}
	complete := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000005"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 3, Title: "", Overview: "Overview", ReleaseDate: "2026-08-03", Source: "tmdb"}
	noMedia := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000006"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 4, Title: "No media", Source: "tmdb"}
	cooled := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000007"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 5, Title: "Cooled", Source: "tmdb", TMDbEpisodeCheckedAt: &now}
	onlyGenerated := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "00000000-0000-0000-0000-000000000008"}, Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 6, Title: "第 6 集", Overview: "Overview", ReleaseDate: "2026-08-03", Source: "tmdb"}
	if err := db.Create(&[]model.MetadataItem{series, season, generated, missingDate, complete, noMedia, cooled, onlyGenerated}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "42"}).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "still", StorageKey: "still.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&generated).Update("overview", nil).Error; err != nil {
		t.Fatal(err)
	}
	for _, episode := range []model.MetadataItem{generated, missingDate, complete, onlyGenerated} {
		if err := db.Create(&model.MetadataArtwork{MetadataID: episode.ID, AssetID: asset.ID, ArtworkType: model.ArtworkTypeStill, SourceProvider: "tmdb"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, episode := range []model.MetadataItem{generated, missingDate, complete, cooled, onlyGenerated} {
		if err := db.Create(&model.Media{MetadataID: episode.ID, Path: "/library/episode-" + string(rune('1'+i)) + ".mkv"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db).Metadata
	first, err := repo.ListTMDbEpisodeMetadataRecheckAfter(t.Context(), "", now.Add(-72*time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].MetadataID != generated.ID || first[0].SeriesTMDbID != "42" || first[0].ArtworkMissing {
		t.Fatalf("first page = %#v", first)
	}
	second, err := repo.ListTMDbEpisodeMetadataRecheckAfter(t.Context(), first[0].MetadataID, now.Add(-72*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].MetadataID != missingDate.ID {
		t.Fatalf("second page = %#v", second)
	}
}

func TestListTMDbSeasonMetadataRecheckAfterFiltersAndPages(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.MetadataProviderSnapshot{}, &model.Media{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := now.Add(-73 * time.Hour)
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "42"}).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "poster", StorageKey: "poster.jpg", MimeType: "image/jpeg"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	var want []string
	for i, scenario := range []string{"special", "generated", "overview", "date", "poster", "snapshot", "identifier", "direct", "complete", "cooldown", "no-media", "ambiguous", "empty-title"} {
		season := model.MetadataItem{PermanentBase: model.PermanentBase{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)}, Kind: model.MetadataKindSeason,
			ParentID: &series.ID, SeasonNum: i, Title: "Own title", Overview: "Own overview", ReleaseDate: "2026-09-01", Source: "tmdb", TMDbSeasonCheckedAt: &old}
		switch scenario {
		case "special":
			season.Title = "特别篇"
		case "generated":
			season.Title = "Season 1"
		case "empty-title":
			season.Title = ""
		case "overview", "direct", "no-media", "ambiguous":
			season.Overview = ""
		case "date":
			season.ReleaseDate = ""
		case "cooldown":
			season.Overview, season.TMDbSeasonCheckedAt = "", &now
		}
		if err := db.Create(&season).Error; err != nil {
			t.Fatal(err)
		}
		if scenario == "overview" {
			if err := db.Model(&season).Update("overview", nil).Error; err != nil {
				t.Fatal(err)
			}
		}
		if scenario != "identifier" {
			if err := db.Create(&model.MetadataIdentifier{MetadataID: season.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeason, ExternalID: fmt.Sprint(100 + i)}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if scenario == "ambiguous" {
			if err := db.Create(&model.MetadataIdentifier{MetadataID: season.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeason, ExternalID: "999"}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if scenario != "snapshot" {
			if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: season.ID, Provider: "tmdb", Payload: `{"id":1}`, FetchedAt: now}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if scenario != "poster" {
			if err := db.Create(&model.MetadataArtwork{MetadataID: season.ID, AssetID: asset.ID, ArtworkType: model.ArtworkTypePoster}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if scenario != "no-media" {
			metadataID := season.ID
			if scenario != "direct" {
				episode := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Complete episode", Overview: "Own overview", ReleaseDate: "2026-09-01", Source: "tmdb"}
				if err := db.Create(&episode).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.MetadataArtwork{MetadataID: episode.ID, AssetID: asset.ID, ArtworkType: model.ArtworkTypeStill}).Error; err != nil {
					t.Fatal(err)
				}
				metadataID = episode.ID
			}
			for version := 0; version < 2; version++ {
				if err := db.Create(&model.Media{MetadataID: metadataID, Path: fmt.Sprintf("/test/%d-%d.mkv", i, version)}).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
		if scenario == "no-media" {
			// 有集元数据但没有文件的季仍应排除。
			child := model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "No file", Source: "test"}
			if err := db.Create(&child).Error; err != nil {
				t.Fatal(err)
			}
		}
		if i >= 2 && i < 8 {
			want = append(want, season.ID)
		}
	}
	repo := New(db).Metadata
	var got []string
	for afterID := ""; ; {
		page, err := repo.ListTMDbSeasonMetadataRecheckAfter(t.Context(), afterID, now.Add(-72*time.Hour), 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, candidate := range page {
			if candidate.Kind != model.MetadataKindSeason || candidate.SeriesTMDbID != "42" {
				t.Fatalf("invalid season candidate: %#v", candidate)
			}
			got = append(got, candidate.MetadataID)
			afterID = candidate.MetadataID
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("seasons = %v, want %v", got, want)
	}
	episodes, err := repo.ListTMDbEpisodeMetadataRecheckAfter(t.Context(), "", now.Add(-72*time.Hour), 200)
	if err != nil || len(episodes) != 0 {
		t.Fatalf("complete episodes must not be rechecked: %v, %v", episodes, err)
	}
	if err := db.Where("metadata_id = ? AND entity_kind = ?", series.ID, model.MetadataKindSeries).Delete(&model.MetadataIdentifier{}).Error; err != nil {
		t.Fatal(err)
	}
	withoutIdentity, err := repo.ListTMDbSeasonMetadataRecheckAfter(t.Context(), "", now, 200)
	if err != nil || len(withoutIdentity) != 0 {
		t.Fatalf("seasons without Series TMDb identity = %v, %v", withoutIdentity, err)
	}
}
