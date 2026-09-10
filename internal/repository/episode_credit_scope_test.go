package repository

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestEpisodeCreditsReadSeasonWithoutWritingEpisode(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{}); err != nil {
		t.Fatal(err)
	}
	series := model.MetadataItem{Kind: "series", Title: "Series", Source: "tmdb"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, Title: "Season", Source: "tmdb"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episodes := []model.MetadataItem{{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1, Title: "First", Source: "tmdb"}, {Kind: "episode", ParentID: &season.ID, EpisodeNum: 2, Title: "Second", Source: "tmdb"}}
	if err := db.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Person
	input := []CreditInput{{Provider: "tmdb", ExternalID: "123", Name: "Season Actor", Type: model.CreditTypeActor}}
	if err := repo.ReplaceCredits(t.Context(), season.ID, []string{model.CreditTypeActor}, input); err != nil {
		t.Fatal(err)
	}
	for _, episode := range episodes {
		if err := repo.ReplaceCredits(t.Context(), episode.ID, []string{model.CreditTypeActor}, input); err != nil {
			t.Fatal(err)
		}
		own, err := repo.ListCredits(t.Context(), episode.ID)
		if err != nil || len(own) != 0 {
			t.Fatalf("episode wrote credits: %+v %v", own, err)
		}
	}
	rows, err := repo.ListCreditsWithPeopleByMetadataIDs(t.Context(), []string{episodes[0].ID, episodes[1].ID, season.ID})
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	for _, row := range rows {
		if row.Person.Name != "Season Actor" {
			t.Fatalf("row=%+v", row)
		}
	}
}

func TestSeasonDerivedSnapshotPreservesFullEpisode(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataProviderSnapshot{}); err != nil {
		t.Fatal(err)
	}
	series := model.MetadataItem{Kind: "series", Title: "Series", Source: "tmdb"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, Title: "Season", Source: "tmdb"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	item := model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1, Title: "Episode", Source: "tmdb"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Metadata
	full := `{"id":123,"episode_number":1,"name":"Old","videos":{"results":[]},"unknown":true}`
	for _, step := range []struct{ payload, want string }{
		{full, full},
		{`{"id":123,"episode_number":1,"name":"New"}`, full},
		{`{"id":124,"episode_number":1,"name":"Different"}`, `{"id":124,"episode_number":1,"name":"Different"}`},
	} {
		if err := repo.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", []byte(step.payload), time.Now()); err != nil {
			t.Fatal(err)
		}
		snapshot, err := repo.FindProviderSnapshot(t.Context(), item.ID, "tmdb")
		if err != nil || snapshot == nil {
			t.Fatalf("snapshot err=%v", err)
		}
		var equal bool
		if err := db.Raw("SELECT ?::jsonb = ?::jsonb", snapshot.Payload, step.want).Scan(&equal).Error; err != nil {
			t.Fatal(err)
		}
		if !equal {
			t.Fatalf("payload=%s want=%s", snapshot.Payload, step.want)
		}
	}
}
