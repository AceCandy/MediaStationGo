package database

import (
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMediaProbeMetadataPostgresSchemaAndOptionalRoundTrip(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN"))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{DisableAutomaticPing: dsn == ""})
	if err != nil {
		t.Fatal(err)
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	if stmt.Schema.Table != "media_probe_metadata" || !stmt.Schema.LookUpField("MediaID").PrimaryKey {
		t.Fatalf("unexpected postgres schema: table=%q primary=%v", stmt.Schema.Table, stmt.Schema.LookUpField("MediaID").PrimaryKey)
	}
	if dataType := db.Migrator().FullDataTypeOf(stmt.Schema.LookUpField("ProbeJSON")).SQL; !strings.Contains(strings.ToLower(dataType), "text") {
		t.Fatalf("probe_json postgres type = %q", dataType)
	}
	constraint := stmt.Schema.Relationships.Relations["Media"].ParseConstraint()
	if constraint == nil || constraint.OnDelete != "CASCADE" {
		t.Fatalf("media foreign key constraint = %#v", constraint)
	}
	if dsn == "" {
		return
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.AutoMigrate(&model.MetadataItem{}, &model.Media{}, &model.MediaProbeMetadata{}); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Probe Round Trip", Source: "local"}
	if err := tx.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: "probe-test", MetadataID: metadata.ID, Title: metadata.Title, Path: "/probe-postgres-test.mkv"}
	if err := tx.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	want := model.MediaProbeMetadata{MediaID: media.ID, ProbeJSON: `{"schema_version":1}`, SchemaVersion: 1, ProbedAt: time.Now().UTC()}
	if err := tx.Create(&want).Error; err != nil {
		t.Fatal(err)
	}
	var got model.MediaProbeMetadata
	if err := tx.First(&got, "media_id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ProbeJSON != want.ProbeJSON || got.SchemaVersion != want.SchemaVersion || got.ProbedAt.IsZero() {
		t.Fatalf("postgres round trip = %#v", got)
	}
}
