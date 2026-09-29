package database

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestMigrateHongGuoFavorites(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, work := range []model.HongGuoWork{
		{SourceID: "101", Kind: "series", RelatedAlbumID: "999", SeasonIndex: 1},
		{SourceID: "102", Kind: "series", RelatedAlbumID: "999", SeasonIndex: 2},
		{SourceID: "103", Kind: "movie"},
		{SourceID: "104", Kind: "series"},
	} {
		if err := db.Create(&work).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []model.HongGuoUserState{
		{UserID: "viewer", SourceID: "101", Favorite: true, UpdatedAt: now.Add(-time.Hour)},
		{UserID: "viewer", SourceID: "102", Favorite: false, UpdatedAt: now},
		{UserID: "viewer", SourceID: "103", Favorite: true, UpdatedAt: now},
		{UserID: "viewer", SourceID: "104", Favorite: true, UpdatedAt: now},
		{UserID: "viewer", SourceID: "105", Favorite: true, UpdatedAt: now}, // 已卸载的资料仍保留。
		{UserID: "other", SourceID: "101", Favorite: true, UpdatedAt: now},
		{UserID: "viewer", SourceID: "101", EpisodeNumber: 1, PositionMs: 12345, UpdatedAt: now},
	} {
		if err := db.Create(&state).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.HongGuoFavorite{UserID: "other", ItemID: "hg-group-999", Favorite: false, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrateHongGuoFavorites(db); err != nil {
			t.Fatal(err)
		}
	}
	var favorites []model.HongGuoFavorite
	if err := db.Order("user_id,item_id").Find(&favorites).Error; err != nil || len(favorites) != 5 {
		t.Fatalf("favorites=%+v err=%v", favorites, err)
	}
	for _, favorite := range favorites {
		if favorite.Favorite != (favorite.UserID == "viewer") || !favorite.UpdatedAt.Equal(now) {
			t.Fatalf("lost state or newer cancellation: %+v", favorite)
		}
	}
	var states []model.HongGuoUserState
	if err := db.Find(&states).Error; err != nil || len(states) != 1 || states[0].EpisodeNumber != 1 || states[0].PositionMs != 12345 {
		t.Fatalf("progress changed or legacy favorites remain: %+v %v", states, err)
	}
	if err := db.Model(&model.HongGuoFavorite{}).Where("user_id = ?", "viewer").Update("favorite", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateHongGuoFavorites(db); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.HongGuoFavorite{}).Where("favorite").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("canceled favorite revived: %d %v", count, err)
	}
}
