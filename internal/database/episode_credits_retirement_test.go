package database

import (
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"testing"
)

func TestRetireEpisodeCreditsPreservesPeopleAndOtherWorks(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.Person{}, &model.MetadataCredit{}, &model.MetadataProviderSnapshot{}); err != nil {
		t.Fatal(err)
	}
	person := model.Person{Name: "Shared", OriginalName: "Shared", NormalizedName: "shared", Source: "tmdb"}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	parentID := ""
	for _, kind := range []string{"movie", "series", "season", "episode"} {
		item := model.MetadataItem{Kind: kind, Title: kind, Source: "tmdb"}
		if kind == "season" || kind == "episode" {
			id := parentID
			item.ParentID = &id
		}
		if kind == "episode" {
			item.EpisodeNum = 1
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		parentID = item.ID
		if err := db.Create(&model.MetadataCredit{MetadataID: item.ID, PersonID: person.ID, Type: model.CreditTypeActor}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.MetadataProviderSnapshot{MetadataID: item.ID, Provider: "tmdb", Payload: `{"credits":{"cast":[]}}`}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := retireEpisodeCredits(db); err != nil {
			t.Fatal(err)
		}
	}
	for table, want := range map[string]int64{"metadata_credits": 3, "people": 1, "metadata_provider_snapshots": 4, "metadata_items": 4} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s count=%d want=%d", table, count, want)
		}
	}
}
