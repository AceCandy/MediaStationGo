package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type catalogMigrationQueryCounter struct {
	logger.Interface
	statements int
}

func (l *catalogMigrationQueryCounter) Trace(context.Context, time.Time, func() (string, int64), error) {
	l.statements++
}

func TestCatalogImageMigrationBatchesArtwork(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	base := testArtworkPNG(t, 12, 18)
	var assets []model.ArtworkAsset
	for i := range 105 {
		data := append(append([]byte{}, base...), byte(i))
		stored, _ := prepareStoredImage(data)
		oldKey := filepath.Join("sha256", stored.SHA256[:2], stored.SHA256[2:4], filepath.Base(stored.StorageKey))
		if err := writeStoredImage(filepath.Join(dir, "artwork", oldKey), data, ".test-*"); err != nil {
			t.Fatal(err)
		}
		assets = append(assets, model.ArtworkAsset{SHA256: stored.SHA256, StorageKey: oldKey, MimeType: stored.MimeType})
	}
	if err := db.Create(&assets).Error; err != nil {
		t.Fatal(err)
	}
	counter := &catalogMigrationQueryCounter{Interface: db.Logger}
	m := catalogImageMigration{db: db.Session(&gorm.Session{Logger: counter}).WithContext(t.Context()), dataDir: dir}
	started := time.Now()
	if err := m.artwork(); err != nil || m.result.Migrated != len(assets) {
		t.Fatalf("batch migration: %+v %v", m.result, err)
	}
	t.Logf("105 assets: %d SQL statements, %s", counter.statements, time.Since(started))
	if counter.statements > 8 {
		t.Fatalf("expected bounded batch queries, got %d", counter.statements)
	}
	var remaining int64
	if err := db.Model(&model.ArtworkAsset{}).Where("storage_key NOT LIKE 'catalogs/%'").Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("unmigrated assets: %d %v", remaining, err)
	}
}

func TestCatalogImageMigrationBatchRollbackAndRetry(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var assets []model.ArtworkAsset
	var keys []string
	for i, id := range []string{"batch-ok", "batch-fail"} {
		data := testArtworkPNG(t, 10+i, 20)
		stored, _ := prepareStoredImage(data)
		old := filepath.Join("sha256", stored.SHA256[:2], stored.SHA256[2:4], filepath.Base(stored.StorageKey))
		if err := writeStoredImage(filepath.Join(dir, "artwork", old), data, ".test-*"); err != nil {
			t.Fatal(err)
		}
		assets = append(assets, model.ArtworkAsset{PermanentBase: model.PermanentBase{ID: id}, SHA256: stored.SHA256, StorageKey: old, MimeType: stored.MimeType})
		key, _ := catalogImageKey("legacy", "artwork", stored.StorageKey)
		keys = append(keys, key)
	}
	if err := db.Create(&assets).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE FUNCTION reject_image_batch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.id = 'batch-fail' THEN RAISE EXCEPTION 'injected image batch failure'; END IF; RETURN NEW; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_image_batch BEFORE UPDATE ON artwork_assets FOR EACH ROW EXECUTE FUNCTION reject_image_batch()`).Error; err != nil {
		t.Fatal(err)
	}
	m := catalogImageMigration{db: db.WithContext(t.Context()), dataDir: dir}
	if err := m.artwork(); err == nil || m.result.Migrated != 0 {
		t.Fatalf("failed batch: %+v %v", m.result, err)
	}
	for i, asset := range assets {
		var got model.ArtworkAsset
		if err := db.First(&got, "id = ?", asset.ID).Error; err != nil || got.StorageKey != asset.StorageKey {
			t.Fatal("failed statement partly updated references")
		}
		if _, err := os.Stat(filepath.Join(dir, keys[i])); err != nil {
			t.Fatal("copy was not completed before database update")
		}
	}
	if err := db.Exec("DROP TRIGGER reject_image_batch ON artwork_assets").Error; err != nil {
		t.Fatal(err)
	}
	if err := m.artwork(); err != nil || m.result.Migrated != 2 {
		t.Fatalf("retry: %+v %v", m.result, err)
	}
	// 观察值已经变化的记录不得覆盖；计数只包含真正提交的更新。
	changes := []catalogImageKeyChange{{assets[0].ID, "stale-key", "should-not-apply"}, {assets[1].ID, keys[1], keys[1] + "-changed"}}
	if err := m.updateKeys("artwork_assets", "storage_key", changes); err != nil || m.result.Migrated != 3 {
		t.Fatalf("observed keys: %+v %v", m.result, err)
	}
	var got model.ArtworkAsset
	if err := db.First(&got, "id = ?", assets[0].ID).Error; err != nil || got.StorageKey != keys[0] {
		t.Fatal("stale key overwrote newer value")
	}
}

func TestCatalogImageMigrationPreservesAssetWithoutReferenceRewrites(t *testing.T) {
	for _, tc := range []struct {
		name        string
		sources     []string
		preexisting bool
	}{
		{name: "unreferenced"},
		{name: "tmdb", sources: []string{"tmdb"}},
		{name: "local_aliases", sources: []string{"local", "local_nfo"}},
		{name: "existing_target", sources: []string{"tmdb"}, preexisting: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newServiceTestDB(t)
			if err := db.AutoMigrate(model.AllModels()...); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			data := testArtworkPNG(t, 12, 18)
			stored, _ := prepareStoredImage(data)
			oldKey := filepath.Join("sha256", stored.SHA256[:2], stored.SHA256[2:4], filepath.Base(stored.StorageKey))
			if err := writeStoredImage(filepath.Join(dir, "artwork", oldKey), data, ".test-*"); err != nil {
				t.Fatal(err)
			}
			asset := model.ArtworkAsset{SHA256: stored.SHA256, StorageKey: oldKey, MimeType: stored.MimeType}
			asset.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
			if err := db.Create(&asset).Error; err != nil {
				t.Fatal(err)
			}
			var selections []model.MetadataArtwork
			for _, source := range tc.sources {
				item := model.MetadataItem{Kind: "movie", Title: "Fixture", Source: source}
				if err := db.Create(&item).Error; err != nil {
					t.Fatal(err)
				}
				selection := model.MetadataArtwork{MetadataID: item.ID, AssetID: asset.ID, ArtworkType: "poster", SourceProvider: source}
				selection.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
				if err := db.Create(&selection).Error; err != nil {
					t.Fatal(err)
				}
				selections = append(selections, selection)
			}
			provider := "legacy"
			if len(tc.sources) > 0 {
				provider = tc.sources[0]
			}
			newKey, _ := catalogImageKey(provider, "artwork", stored.StorageKey)
			wantID := asset.ID
			if tc.preexisting {
				target := model.ArtworkAsset{SHA256: stored.SHA256, StorageKey: newKey, MimeType: stored.MimeType}
				if err := db.Create(&target).Error; err != nil {
					t.Fatal(err)
				}
				wantID = target.ID
				newKey, _ = catalogImageKey("legacy", "artwork", stored.StorageKey)
			}
			counter := &catalogMigrationQueryCounter{Interface: db.Logger}
			m := catalogImageMigration{db: db.Session(&gorm.Session{Logger: counter}).WithContext(t.Context()), dataDir: dir}
			if err := m.artwork(); err != nil || m.result.Migrated != 1 {
				t.Fatalf("migration: %+v %v", m.result, err)
			}
			t.Logf("migration SQL statements including batch SELECT: %d", counter.statements)
			if !tc.preexisting && counter.statements != 4 {
				t.Errorf("unchanged references should need one batch SELECT and three statements, got %d", counter.statements)
			}
			var got model.ArtworkAsset
			if err := db.First(&got, "id = ?", asset.ID).Error; err != nil || got.StorageKey != newKey || !got.UpdatedAt.Equal(asset.UpdatedAt) {
				t.Fatalf("asset identity or timestamp changed: %+v %v", got, err)
			}
			for _, before := range selections {
				var after model.MetadataArtwork
				if err := db.First(&after, "id = ?", before.ID).Error; err != nil || after.AssetID != wantID || !after.UpdatedAt.Equal(before.UpdatedAt) {
					t.Fatalf("reference or timestamp changed: %+v %v", after, err)
				}
			}
			if err := m.artwork(); err != nil || m.result.Migrated != 1 {
				t.Fatalf("repeat migrated an asset again: %+v %v", m.result, err)
			}
		})
	}
}

func TestCatalogImageMigrationSplitsSourcesAndPreservesImages(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	data := testArtworkPNG(t, 18, 27)
	stored, _ := prepareStoredImage(data)
	oldKey := "sha256/" + stored.SHA256[:2] + "/" + stored.SHA256[2:4] + "/" + filepath.Base(stored.StorageKey)
	write := func(root string) {
		t.Helper()
		if err := writeStoredImage(filepath.Join(dir, root, oldKey), data, ".test-*"); err != nil {
			t.Fatal(err)
		}
	}
	write("artwork")
	write("people")
	write("catalogs/hongguo/artwork")
	write("catalogs/huangguoai/artwork")
	asset := model.ArtworkAsset{SHA256: stored.SHA256, StorageKey: oldKey, MimeType: stored.MimeType, Width: stored.Width, Height: stored.Height, SizeBytes: stored.SizeBytes}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	item := model.MetadataItem{Kind: "movie", Title: "Shared", Source: "tmdb"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	selection := model.MetadataArtwork{MetadataID: item.ID, AssetID: asset.ID, ArtworkType: "poster", SourceProvider: "tmdb"}
	candidate := model.MetadataArtworkCandidate{MetadataID: item.ID, AssetID: asset.ID, ArtworkType: "poster", SourceProvider: "douban"}
	for _, v := range []any{&selection, &candidate} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	raw, _ := json.Marshal(map[string]any{"future": "preserved", "credits": []map[string]string{{"name": "Actor", "image_key": oldKey, "unknown": "kept"}}})
	rich := string(raw)
	subject := model.DoubanSubject{DoubanID: "100", EntityKind: "movie", PosterAssetID: &asset.ID, RichPayload: &rich, RichFetchedAt: &now, RichNextRetryAt: &now}
	person := model.Person{Name: "Actor", OriginalName: "Actor", NormalizedName: "actor", Source: "tmdb", ProfileImageKey: oldKey}
	hgPerson := model.HongGuoPerson{SourceID: "123", Name: "Actor"}
	lib := model.Library{Name: "Library", Type: "nfo_movie"}
	for _, v := range []any{&subject, &person, &hgPerson, &lib} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	nfo := model.NFOItem{LibraryID: lib.ID, LocalKey: "/movie", Kind: "movie", NFOFields: model.NFOFields{Title: "Local", PosterAssetID: asset.ID}}
	hg := model.HongGuoArtwork{PersonID: &hgPerson.ID, SourceURL: "https://example.com/person", LocalKey: oldKey}
	hga := model.HuangGuoAIArtwork{SourceID: "123", SourceURL: "https://example.com/poster", LocalKey: oldKey}
	for _, v := range []any{&nfo, &hg, &hga} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	mediaRow := model.Media{LibraryID: lib.ID, Path: "/movie.mkv", CatalogSource: model.CatalogSourceNFO}
	if err := db.Create(&mediaRow).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.NFOMediaBinding{MediaID: mediaRow.ID, ItemID: nfo.ID, NFOFields: model.NFOFields{Title: "Local file", PosterAssetID: asset.ID}}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{App: config.AppConfig{DataDir: dir}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	repos := repository.New(db)
	media := NewMediaService(cfg, zap.NewNop(), repos)
	legacyCover, _ := media.libraryCoverPath(lib.ID, stored.SHA256)
	if err := writeStoredImage(legacyCover, data, ".test-*"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&lib).UpdateColumn("cover_url", libraryCoverURL(lib.ID)+"?v="+stored.SHA256).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected reference failure")
	if err := db.Callback().Update().Before("gorm:update").Register("test:image-migration-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "metadata_artwork_candidates" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCatalogImages(t.Context(), db, dir); !errors.Is(err, injected) {
		t.Fatalf("reference failure: %v", err)
	}
	if err := db.Callback().Update().Remove("test:image-migration-failure"); err != nil {
		t.Fatal(err)
	}
	var original model.ArtworkAsset
	if err := db.First(&original, "id = ?", asset.ID).Error; err != nil || original.StorageKey != oldKey {
		t.Fatal("reference failure did not roll back asset")
	}
	var assetCount int64
	if err := db.Model(&model.ArtworkAsset{}).Count(&assetCount).Error; err != nil || assetCount != 1 {
		t.Fatal("reference failure left committed split assets")
	}
	result, err := MigrateCatalogImages(t.Context(), db, dir)
	if err != nil || result.Migrated != 6 || result.Missing != 0 {
		t.Fatalf("migration: %+v %v", result, err)
	}
	for _, v := range []any{&selection, &candidate, &subject, &person, &nfo, &binding, &hg, &hga, &lib} {
		if err := db.First(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	if selection.AssetID == candidate.AssetID || candidate.AssetID != *subject.PosterAssetID || nfo.PosterAssetID == selection.AssetID {
		t.Fatal("source asset references not separated")
	}
	if binding.PosterAssetID != nfo.PosterAssetID {
		t.Fatal("NFO media binding reference was not migrated")
	}
	store := NewArtworkStore(cfg, repos.Artwork, nil)
	for id, source := range map[string]string{selection.AssetID: "tmdb", candidate.AssetID: "douban", nfo.PosterAssetID: "local"} {
		got, err := repos.Artwork.FindAssetByID(t.Context(), id)
		if err != nil || !strings.HasPrefix(got.StorageKey, "catalogs/"+source+"/artwork/") {
			t.Fatalf("asset %+v %v", got, err)
		}
		w := httptest.NewRecorder()
		if err := store.Serve(t.Context(), w, httptest.NewRequest("GET", "/image", nil), id); err != nil || !bytes.Equal(w.Body.Bytes(), data) {
			t.Fatalf("serve migrated asset: %v", err)
		}
	}
	if !subject.RichFetchedAt.Equal(now) || !subject.RichNextRetryAt.Equal(now) || !strings.Contains(*subject.RichPayload, "preserved") || !strings.Contains(*subject.RichPayload, "kept") {
		t.Fatal("supplement state changed")
	}
	people := NewPeopleImageStore(cfg, repos.Person, nil)
	if !people.hasUsableImage(person.ProfileImageKey) || !people.hasUsableImage(doubanRichPayload(&subject).Credits[0].ImageKey) {
		t.Fatal("migrated avatars unusable")
	}
	for _, key := range []string{hg.LocalKey, hga.LocalKey} {
		if got, err := os.ReadFile(filepath.Join(dir, key)); err != nil || !bytes.Equal(got, data) {
			t.Fatalf("catalog image %s: %v", key, err)
		}
	}
	if !strings.HasPrefix(hg.LocalKey, "catalogs/hongguo/people/") {
		t.Fatal("Hongguo avatar was classified as artwork")
	}
	w := httptest.NewRecorder()
	if err := media.ServeLibraryCover(t.Context(), w, httptest.NewRequest("GET", "/cover", nil), lib.ID); err != nil || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("cover: %v", err)
	}
	if _, err := media.ClearLibraryCover(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	if err := store.Serve(t.Context(), w, httptest.NewRequest("GET", "/image", nil), nfo.PosterAssetID); err != nil || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("clearing cover removed shared local image")
	}
	second, err := MigrateCatalogImages(t.Context(), db, dir)
	if err != nil || second != (CatalogImageMigrationResult{}) {
		t.Fatalf("repeat: %+v %v", second, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "artwork", oldKey)); err != nil || !bytes.Equal(got, data) {
		t.Fatal("migration deleted recovery original")
	}
}

func TestCatalogImageMigrationMissingAndWriteFailure(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	data := testArtworkPNG(t, 20, 30)
	stored, _ := prepareStoredImage(data)
	old := "sha256/" + stored.SHA256[:2] + "/" + stored.SHA256[2:4] + "/" + filepath.Base(stored.StorageKey)
	asset := model.ArtworkAsset{SHA256: stored.SHA256, StorageKey: old, MimeType: stored.MimeType}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	result, err := MigrateCatalogImages(t.Context(), db, dir)
	if err != nil || result.Missing != 1 || result.Migrated != 0 {
		t.Fatalf("missing: %+v %v", result, err)
	}
	if err := writeStoredImage(filepath.Join(dir, "artwork", old), testArtworkPNG(t, 21, 30), ".test-*"); err != nil {
		t.Fatal(err)
	}
	result, err = MigrateCatalogImages(t.Context(), db, dir)
	if err != nil || result.Missing != 1 || result.Migrated != 0 {
		t.Fatalf("corrupt content: %+v %v", result, err)
	}
	if err := writeStoredImage(filepath.Join(dir, "artwork", old), data, ".test-*"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalogs"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCatalogImages(t.Context(), db, dir); err == nil {
		t.Fatal("write failure was ignored")
	}
	if err := db.First(&asset).Error; err != nil || asset.StorageKey != old {
		t.Fatal("failed copy advanced reference")
	}
	if err := os.Remove(filepath.Join(dir, "catalogs")); err != nil {
		t.Fatal(err)
	}
	result, err = MigrateCatalogImages(t.Context(), db, dir)
	if err != nil || result.Migrated != 1 {
		t.Fatalf("retry: %+v %v", result, err)
	}
}

func TestCatalogImagesDeduplicateOnlyWithinSource(t *testing.T) {
	_, repos := newDoubanCacheTestProvider(t, nil)
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}}
	store := NewArtworkStore(cfg, repos.Artwork, nil)
	data := testArtworkPNG(t, 16, 9)
	ids := map[string]string{}
	for _, source := range []string{"tmdb", "douban", "tmdb"} {
		asset, err := store.prepareImageAsset(source, data)
		if err != nil {
			t.Fatal(err)
		}
		asset, err = repos.Artwork.SaveAsset(t.Context(), asset)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(asset.StorageKey, "/")
		if len(parts) != 7 || parts[1] != source || parts[3] != asset.SHA256[:2] || parts[4] != asset.SHA256[2:4] || parts[5] != asset.SHA256[4:6] {
			t.Fatalf("key: %s", asset.StorageKey)
		}
		if old := ids[source]; old != "" && old != asset.ID {
			t.Fatal("same source duplicated")
		}
		ids[source] = asset.ID
	}
	if ids["tmdb"] == ids["douban"] {
		t.Fatal("different sources shared asset")
	}
}

func TestCatalogPeopleImageCacheIsSourceScoped(t *testing.T) {
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	proxy := NewImageProxy(cfg, zap.NewNop())
	data := testArtworkPNG(t, 12, 18)
	calls := 0
	proxy.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return posterResponse(req, 200, data), nil
	})}
	store := NewPeopleImageStore(cfg, nil, proxy)
	first, err := store.ImportCached(t.Context(), "tmdb", "https://example.com/person.png")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.ImportCached(t.Context(), "douban", "https://example.com/person.png")
	if err != nil || first == second || !store.hasUsableImage(first) || !store.hasUsableImage(second) {
		t.Fatalf("source keys: %s %s %v", first, second, err)
	}
	for _, source := range []string{"tmdb", "douban"} {
		if _, err := store.ImportCached(t.Context(), source, "https://example.com/person.png"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("source cache attempts: %d", calls)
	}
	for _, source := range []string{"../escape", "tmdb/other", "."} {
		if _, err := catalogImageKey(source, "people", "aa/bb/cc/image.png"); err == nil {
			t.Fatalf("invalid provider accepted: %s", source)
		}
	}
}

func TestCatalogImageMigrationPagesChangedKeys(t *testing.T) {
	db := newServiceTestDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	data := testArtworkPNG(t, 5, 7)
	stored, _ := prepareStoredImage(data)
	old := "sha256/" + stored.SHA256[:2] + "/" + stored.SHA256[2:4] + "/" + filepath.Base(stored.StorageKey)
	for _, root := range []string{"people", "catalogs/huangguoai/artwork"} {
		if err := writeStoredImage(filepath.Join(dir, root, old), data, ".test-*"); err != nil {
			t.Fatal(err)
		}
	}
	var people []model.Person
	var artwork []model.HuangGuoAIArtwork
	for i := range 105 {
		name := fmt.Sprintf("Person-%d", i)
		people = append(people, model.Person{Name: name, OriginalName: name, NormalizedName: name, Source: "tmdb", ProfileImageKey: old})
		artwork = append(artwork, model.HuangGuoAIArtwork{SourceID: fmt.Sprint(i), SourceURL: "https://example.com/poster", LocalKey: old})
	}
	if err := db.Create(&people).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&artwork).Error; err != nil {
		t.Fatal(err)
	}
	result, err := MigrateCatalogImages(t.Context(), db, dir)
	if err != nil || result.Migrated != 210 || result.Missing != 0 {
		t.Fatalf("pages: %+v %v", result, err)
	}
	repeated, err := MigrateCatalogImages(t.Context(), db, dir)
	if err != nil || repeated != (CatalogImageMigrationResult{}) {
		t.Fatalf("repeated pages: %+v %v", repeated, err)
	}
}
