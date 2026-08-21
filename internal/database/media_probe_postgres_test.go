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
	if dataType := db.Migrator().FullDataTypeOf(stmt.Schema.LookUpField("DurationMS")).SQL; !strings.Contains(strings.ToLower(dataType), "bigint") {
		t.Fatalf("duration_ms postgres type = %q", dataType)
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
	want := model.MediaProbeMetadata{
		MediaID: media.ID, ProbeJSON: `{"schema_version":1}`, SchemaVersion: 1,
		SummaryVersion: 1, DurationMS: 120_000, SizeBytes: 1_000_000,
		Container: "matroska", BitRate: 8_000_000, Width: 3840, Height: 2160,
		VideoCodec: "hevc", AudioCodec: "eac3", ProbedAt: time.Now().UTC(),
	}
	if err := tx.Create(&want).Error; err != nil {
		t.Fatal(err)
	}
	var got model.MediaProbeMetadata
	if err := tx.First(&got, "media_id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ProbeJSON != want.ProbeJSON || got.SchemaVersion != want.SchemaVersion || got.SummaryVersion != want.SummaryVersion ||
		got.DurationMS != want.DurationMS || got.SizeBytes != want.SizeBytes || got.BitRate != want.BitRate ||
		got.Container != want.Container || got.VideoCodec != want.VideoCodec || got.AudioCodec != want.AudioCodec || got.ProbedAt.IsZero() {
		t.Fatalf("postgres round trip = %#v", got)
	}
}
