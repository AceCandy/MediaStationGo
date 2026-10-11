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
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataProviderSnapshot{}, &model.TMDbSubject{}, &model.TMDbPersonProfile{}, &model.TMDbPayload{}, &model.TMDbCredit{}, &model.TMDbArtwork{}, &model.TMDbRefreshJob{}, &model.TMDbQueryCache{}, &model.MetadataIdentifier{}); err != nil {
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

	for _, ident := range []model.MetadataIdentifier{
		{MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "10"},
		{MetadataID: season.ID, Provider: "tmdb", EntityKind: "season", ExternalID: "11"},
		{MetadataID: item.ID, Provider: "tmdb", EntityKind: "episode", ExternalID: "123"},
	} {
		if err := db.Create(&ident).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db).Metadata
	full := `{"id":123,"season_number":0,"episode_number":1,"name":"Old","videos":{"results":[]},"unknown":true}`
	at := time.Now()
	if err := repo.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", []byte(full), at); err != nil {
		t.Fatal(err)
	}
	derived := `{"id":123,"season_number":0,"episode_number":1,"name":"New"}`
	if err := repo.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", []byte(derived), at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var detail model.TMDbPayload
	if err := db.Where("component='detail'").First(&detail).Error; err != nil {
		t.Fatal(err)
	}
	var equal bool
	if err := db.Raw("SELECT ?::jsonb = ?::jsonb", detail.Body, full).Scan(&equal).Error; err != nil || !equal {
		t.Fatalf("original detail lost: %s %v", detail.Body, err)
	}
	snapshot, err := repo.FindProviderSnapshot(t.Context(), item.ID, "tmdb")
	if err != nil || snapshot == nil {
		t.Fatalf("snapshot %v", err)
	}
	if err = db.Raw("SELECT (?::jsonb)->>'name'='New' AND (?::jsonb)->>'unknown'='true'", snapshot.Payload, snapshot.Payload).Scan(&equal).Error; err != nil || !equal {
		t.Fatalf("merged fields lost: %s %v", snapshot.Payload, err)
	}
	if err = repo.UpsertProviderSnapshot(t.Context(), item.ID, "tmdb", []byte(`{"id":124,"season_number":0,"episode_number":1}`), at); err == nil {
		t.Fatal("accepted mismatched binding")
	}
}
