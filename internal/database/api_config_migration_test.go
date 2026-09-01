package database

import (
	"strings"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type legacyAPIConfig struct {
	ID       string `gorm:"primaryKey"`
	Provider string
	APIKey   string `gorm:"size:512"`
	BaseURL  string
	Extra    string
	Enabled  bool
}

func (legacyAPIConfig) TableName() string { return "api_configs" }

func TestEnsureAPIConfigColumnsUpgradesLegacyTable(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyAPIConfig{}); err != nil {
		t.Fatal(err)
	}
	const existingKey = "existing-encrypted-value"
	if err := db.Exec(
		`INSERT INTO api_configs (id, provider, api_key) VALUES (?, ?, ?)`,
		"existing-key", "tmdb", existingKey,
	).Error; err != nil {
		t.Fatal(err)
	}

	if err := ensureAPIConfigColumns(db); err != nil {
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
	if !db.Migrator().HasColumn(&model.APIConfig{}, "ImageDirect") {
		t.Fatal("image_direct column was not added")
	}
	if !db.Migrator().HasColumn(&model.APIConfig{}, "UseProxyPool") {
		t.Fatal("use_proxy_pool column was not added")
	}
	var useProxyPool bool
	if err := db.Raw(`SELECT use_proxy_pool FROM api_configs WHERE id = ?`, "existing-key").Scan(&useProxyPool).Error; err != nil {
		t.Fatal(err)
	}
	if useProxyPool {
		t.Fatal("legacy API config unexpectedly enabled proxy pool")
	}
	columns, err := db.Migrator().ColumnTypes(&model.APIConfig{})
	if err != nil {
		t.Fatal(err)
	}
	apiKeyType := ""
	for _, column := range columns {
		if column.Name() == "api_key" {
			apiKeyType = column.DatabaseTypeName()
			break
		}
	}
	if !strings.EqualFold(apiKeyType, "text") {
		t.Fatalf("api_key type = %q, want text", apiKeyType)
	}
	var persistedKey string
	if err := db.Raw(`SELECT api_key FROM api_configs WHERE id = ?`, "existing-key").Scan(&persistedKey).Error; err != nil {
		t.Fatal(err)
	}
	if persistedKey != existingKey {
		t.Fatal("existing api_key changed during migration")
	}
	if err := db.Exec(
		`INSERT INTO api_configs (id, provider, api_key) VALUES (?, ?, ?)`,
		"long-key", "douban", strings.Repeat("x", 4096),
	).Error; err != nil {
		t.Fatal("api_key longer than 512 characters was rejected")
	}
}
