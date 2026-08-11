package database

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

type legacyStorageConfig struct {
	model.Base
	Type      string `gorm:"uniqueIndex;size:16;not null"`
	Config    string `gorm:"type:text;not null"`
	Enabled   bool
	LastError string
}

func (legacyStorageConfig) TableName() string { return "storage_configs" }

func TestRemoveCloudStorageSchemaPreservesLocalAndSharedData(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Library{}, &model.LibraryRoot{}, &model.MetadataItem{}, &model.Media{},
		&model.MediaProbeMetadata{}, &model.STRMRecord{}, &model.Setting{},
		&model.Favorite{}, &model.PlaybackHistory{}, &model.Playlist{}, &model.PlaylistItem{},
		&model.Person{}, &model.MetadataCredit{}, &model.ArtworkAsset{}, &model.MetadataArtwork{},
		&legacyStorageConfig{},
	); err != nil {
		t.Fatal(err)
	}

	metadata := model.MetadataItem{Base: model.Base{ID: "metadata-1"}, Kind: model.MetadataKindMovie, Title: "Movie"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	libraries := []model.Library{
		{Base: model.Base{ID: "cloud-lib"}, Name: "Cloud", Path: " cloud://OPENLIST/movies ", Type: "movie", Enabled: true},
		{Base: model.Base{ID: "local-lib"}, Name: "Local", Path: "/media", Type: "movie", Enabled: true},
		{Base: model.Base{ID: "mixed-lib"}, Name: "Mixed", Path: "cloud://webdav/mixed", Type: "movie", Enabled: true},
	}
	for i := range libraries {
		if err := db.Create(&libraries[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	rootOrderTime := time.Now()
	roots := []model.LibraryRoot{
		{Base: model.Base{ID: "cloud-root"}, LibraryID: "cloud-lib", Path: "cloud://openlist/movies", Enabled: true},
		{Base: model.Base{ID: "local-root"}, LibraryID: "local-lib", Path: "/media", Enabled: true},
		{Base: model.Base{ID: "mixed-cloud-root"}, LibraryID: "mixed-lib", Path: "cloud://webdav/mixed", Enabled: true},
		{Base: model.Base{ID: "mixed-local-root-z", CreatedAt: rootOrderTime}, LibraryID: "mixed-lib", Path: "/mixed-z", Enabled: true, SortOrder: 0},
		{Base: model.Base{ID: "mixed-local-root-a", CreatedAt: rootOrderTime}, LibraryID: "mixed-lib", Path: "/mixed-a", Enabled: true, SortOrder: 0},
		{Base: model.Base{ID: "mixed-local-root-later", CreatedAt: rootOrderTime.Add(time.Minute)}, LibraryID: "mixed-lib", Path: "/mixed-later", Enabled: true, SortOrder: 1},
	}
	for i := range roots {
		if err := db.Create(&roots[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	media := []model.Media{
		{Base: model.Base{ID: "cloud-media"}, LibraryID: "cloud-lib", LibraryRootID: "cloud-root", MetadataID: metadata.ID, Path: "cloud://openlist/movies/Movie.mkv"},
		{Base: model.Base{ID: "local-media"}, LibraryID: "local-lib", LibraryRootID: "local-root", MetadataID: metadata.ID, Path: "/media/Movie.mkv", STRMURL: "https://cdn.example.test/Movie.mkv?token=keep"},
		{Base: model.Base{ID: "legacy-provider-media"}, LibraryID: "local-lib", LibraryRootID: "local-root", MetadataID: metadata.ID, Path: "/media/LegacyProvider.mkv"},
		{Base: model.Base{ID: "mixed-cloud-media"}, LibraryID: "mixed-lib", LibraryRootID: "mixed-cloud-root", MetadataID: metadata.ID, Path: " CLOUD://WEBDAV/mixed/Movie.mkv "},
		{Base: model.Base{ID: "mixed-local-media"}, LibraryID: "mixed-lib", LibraryRootID: "mixed-local-root-a", MetadataID: metadata.ID, Path: "/mixed-a/Movie.mkv"},
	}
	for i := range media {
		if err := db.Create(&media[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&model.Media{}, "id = ?", "legacy-provider-media").Error; err != nil {
		t.Fatal(err)
	}
	for _, mediaID := range []string{"cloud-media", "local-media", "mixed-cloud-media"} {
		if err := db.Create(&model.MediaProbeMetadata{MediaID: mediaID, ProbeJSON: `{}`, SchemaVersion: 1, ProbedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	strmRows := []model.STRMRecord{
		{Base: model.Base{ID: "provider-strm"}, Title: "Cloud", URL: "https://provider.invalid/file", FilePath: "/strm/cloud.strm", Protocol: "OpenList", MediaID: "legacy-provider-media"},
		{Base: model.Base{ID: "http-strm"}, Title: "HTTP", URL: "https://cdn.example.test/file", FilePath: "/strm/http.strm", Protocol: "https", MediaID: "local-media"},
	}
	for i := range strmRows {
		if err := db.Create(&strmRows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.Favorite{Base: model.Base{ID: "favorite-1"}, UserID: "user-1", MetadataID: metadata.ID, MediaID: "cloud-media"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.PlaybackHistory{Base: model.Base{ID: "history-1"}, UserID: "user-1", MetadataID: metadata.ID, MediaID: "cloud-media"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Playlist{Base: model.Base{ID: "playlist-1"}, UserID: "user-1", Name: "Keep"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.PlaylistItem{Base: model.Base{ID: "playlist-item-1"}, PlaylistID: "playlist-1", MetadataID: metadata.ID, MediaID: "cloud-media"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ArtworkAsset{Base: model.Base{ID: "artwork-1"}, SHA256: "artwork-1", StorageKey: "test/artwork.jpg", MimeType: "image/jpeg"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataArtwork{Base: model.Base{ID: "metadata-artwork-1"}, MetadataID: metadata.ID, AssetID: "artwork-1", ArtworkType: "poster"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Person{Base: model.Base{ID: "person-1"}, Name: "Actor", OriginalName: "Actor", NormalizedName: "actor", Source: "local"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataCredit{Base: model.Base{ID: "credit-1"}, MetadataID: metadata.ID, PersonID: "person-1", Type: model.CreditTypeActor}).Error; err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"cloud.auto_sync_enabled": "true", "app.cloud_scan_max_concurrent": "4",
		"transcode.enabled": "true", "app.ffmpeg_path": "/usr/bin/ffmpeg",
		"strm.enabled": "true", "playback.path_mappings": "/media => https://cdn.example.test",
		"ffprobe.path": "/usr/bin/ffprobe",
	} {
		if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&legacyStorageConfig{Type: "openlist", Config: "encrypted", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}

	if err := removeCloudStorageSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := removeCloudStorageSchema(db); err != nil {
		t.Fatalf("second migration: %v", err)
	}

	assertUnscopedCount(t, db, &model.Media{}, "id IN ?", []string{"cloud-media", "legacy-provider-media", "mixed-cloud-media"}, 0)
	assertUnscopedCount(t, db, &model.Media{}, "id IN ?", []string{"local-media", "mixed-local-media"}, 2)
	assertUnscopedCount(t, db, &model.Library{}, "id = ?", "cloud-lib", 0)
	assertUnscopedCount(t, db, &model.LibraryRoot{}, "id IN ?", []string{"cloud-root", "mixed-cloud-root"}, 0)
	assertUnscopedCount(t, db, &model.MediaProbeMetadata{}, "media_id = ?", "cloud-media", 0)
	assertUnscopedCount(t, db, &model.MediaProbeMetadata{}, "media_id = ?", "local-media", 1)
	assertUnscopedCount(t, db, &model.STRMRecord{}, "id = ?", "provider-strm", 0)
	assertUnscopedCount(t, db, &model.STRMRecord{}, "id = ?", "http-strm", 1)
	assertUnscopedCount(t, db, &model.Favorite{}, "id = ?", "favorite-1", 1)
	assertUnscopedCount(t, db, &model.PlaybackHistory{}, "id = ?", "history-1", 1)
	assertUnscopedCount(t, db, &model.Playlist{}, "id = ?", "playlist-1", 1)
	assertUnscopedCount(t, db, &model.PlaylistItem{}, "id = ?", "playlist-item-1", 1)
	assertUnscopedCount(t, db, &model.ArtworkAsset{}, "id = ?", "artwork-1", 1)
	assertUnscopedCount(t, db, &model.MetadataArtwork{}, "id = ?", "metadata-artwork-1", 1)
	assertUnscopedCount(t, db, &model.Person{}, "id = ?", "person-1", 1)
	assertUnscopedCount(t, db, &model.MetadataCredit{}, "id = ?", "credit-1", 1)
	assertUnscopedCount(t, db, &model.MetadataItem{}, "id = ?", metadata.ID, 1)

	var mixed model.Library
	if err := db.Unscoped().First(&mixed, "id = ?", "mixed-lib").Error; err != nil {
		t.Fatal(err)
	}
	if mixed.Path != "/mixed-a" {
		t.Fatalf("mixed library path = %q", mixed.Path)
	}
	for _, key := range []string{"cloud.auto_sync_enabled", "app.cloud_scan_max_concurrent", "transcode.enabled", "app.ffmpeg_path"} {
		assertUnscopedCount(t, db, &model.Setting{}, "key = ?", key, 0)
	}
	for _, key := range []string{"strm.enabled", "playback.path_mappings", "ffprobe.path"} {
		assertUnscopedCount(t, db, &model.Setting{}, "key = ?", key, 1)
	}
	if db.Migrator().HasTable("storage_configs") {
		t.Fatal("storage_configs table still exists")
	}
}

func TestRemoveCloudStorageSchemaRollsBackUnsafeMixedLibrary(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.LibraryRoot{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.STRMRecord{}, &model.Setting{}, &legacyStorageConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Library{Base: model.Base{ID: "unsafe-lib"}, Name: "Unsafe", Path: "cloud://openlist", Type: "movie"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.LibraryRoot{Base: model.Base{ID: "unsafe-cloud-root"}, LibraryID: "unsafe-lib", Path: "cloud://openlist"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{Base: model.Base{ID: "cloud-media"}, LibraryID: "unsafe-lib", LibraryRootID: "unsafe-cloud-root", Path: "cloud://openlist/Movie.mkv"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Media{Base: model.Base{ID: "local-media"}, LibraryID: "unsafe-lib", Path: "/local/Movie.mkv"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: "cloud.auto_sync_enabled", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacyStorageConfig{Type: "openlist", Config: "encrypted"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := removeCloudStorageSchema(db); err == nil {
		t.Fatal("expected unsafe mixed library migration to fail")
	}
	assertUnscopedCount(t, db, &model.Media{}, "id IN ?", []string{"cloud-media", "local-media"}, 2)
	assertUnscopedCount(t, db, &model.LibraryRoot{}, "id = ?", "unsafe-cloud-root", 1)
	assertUnscopedCount(t, db, &model.Setting{}, "key = ?", "cloud.auto_sync_enabled", 1)
	if !db.Migrator().HasTable("storage_configs") {
		t.Fatal("storage_configs drop should roll back")
	}
}

func assertUnscopedCount(t *testing.T, db *gorm.DB, value any, query string, arg any, want int64) {
	t.Helper()
	var got int64
	if err := db.Unscoped().Model(value).Where(query, arg).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count for %T where %q = %d, want %d", value, query, got, want)
	}
}
