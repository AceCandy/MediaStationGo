package database

import (
	"reflect"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestHongGuoDanmuMigrationIsolation(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	// 基线已经完成旧迁移，只删除本测试的空新表来模拟旧安装升级。
	if err := db.Migrator().DropTable(&model.HongGuoDanmu{}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		&model.HongGuoWork{PermanentBase: model.PermanentBase{ID: "work"}, SourceID: "123", Kind: "series", Title: "sentinel"},
		&model.HongGuoFavorite{UserID: "u", ItemID: "123", Favorite: true},
		&model.HongGuoUserState{UserID: "u", SourceID: "123", EpisodeNumber: 1, Completed: true, PositionMs: 1000},
		&model.APIConfig{Provider: "tmdb", APIKey: "test-ciphertext", Enabled: true},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() []string {
		var result []string
		for _, table := range []string{"hongguo_works", "hongguo_favorites", "hongguo_user_states", "api_configs"} {
			var value string
			if err := db.Raw("SELECT COALESCE(jsonb_agg(to_jsonb(t))::text,'[]') FROM " + table + " t").Scan(&value).Error; err != nil {
				t.Fatal(err)
			}
			result = append(result, value)
		}
		return result
	}
	before := snapshot()
	for n := 0; n < 2; n++ {
		if err := AutoMigrate(db); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, snapshot()) {
			t.Fatal("old rows changed")
		}
	}
	for _, m := range model.HongGuoModels() {
		if _, ok := m.(*model.HongGuoDanmu); ok {
			t.Fatal("danmu joined source cleanup")
		}
	}
	row := model.HongGuoDanmu{SourceID: "123", EpisodeNumber: 1, CommentID: "1", Content: "keep"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&model.HongGuoWork{}, "id = ?", "work").Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.HongGuoDanmu{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("catalog cleanup deleted history")
	}
}
