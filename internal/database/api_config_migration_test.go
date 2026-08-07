package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type legacyAPIConfig struct {
	ID       string `gorm:"primaryKey"`
	Provider string
	APIKey   string
	BaseURL  string
	Extra    string
	Enabled  bool
}

func (legacyAPIConfig) TableName() string { return "api_configs" }

func TestEnsureAPIConfigColumnsAddsNewFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:api-config-columns?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyAPIConfig{}); err != nil {
		t.Fatal(err)
	}

	if err := ensureAPIConfigColumns(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn(&model.APIConfig{}, "Model") {
		t.Fatal("model column was not added")
	}
	if !db.Migrator().HasColumn(&model.APIConfig{}, "WebSearchEnabled") {
		t.Fatal("web_search_enabled column was not added")
	}
}
