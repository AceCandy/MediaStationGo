package database

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"
)

func TestTaskSystemHistoryReclassification(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	rows := []model.TaskExecution{}
	for _, kind := range []string{"scan", "artwork", "people", "cleanup", "tmdb_data_refresh", "tmdb_data_supplement", "douban_data_refresh", "douban_data_supplement", "hongguo_refresh", "huangguoai_refresh"} {
		rows = append(rows, model.TaskExecution{System: "catalog", Kind: kind, Name: kind, Trigger: "event", Status: "completed", StartedAt: time.Now()})
	}
	rows = append(rows, model.TaskExecution{System: "media", Kind: "people", Name: "人物翻译", Trigger: "scheduled", Status: "completed", StartedAt: time.Now()})
	if err = db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = reclassifyTaskSystems(db); err != nil {
			t.Fatal(err)
		}
		for _, old := range rows {
			var current model.TaskExecution
			if err = db.First(&current, "id = ?", old.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.System != model.TaskSystemForExecution(old.Kind, old.Name) || current.Name != old.Name || current.Status != old.Status || !current.UpdatedAt.Equal(old.UpdatedAt.Truncate(time.Microsecond)) {
				t.Fatalf("history changed: %+v", current)
			}
		}
	}
}
