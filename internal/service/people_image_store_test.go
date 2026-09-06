package service

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newPeopleImageStoreTest(t *testing.T) (*PeopleImageStore, *repository.Container, *config.Config) {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Person{}, &model.PersonIdentifier{}, &model.MetadataItem{}, &model.MetadataCredit{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	return NewPeopleImageStore(cfg, repos.Person, NewImageProxy(cfg, zap.NewNop())), repos, cfg
}

func TestPeopleImageStorePersistsShardedImageAndServesPerson(t *testing.T) {
	store, repos, cfg := newPeopleImageStoreTest(t)
	source := filepath.Join(cfg.App.DataDir, "source.png")
	want := testArtworkPNG(t, 4, 3)
	if err := os.WriteFile(source, want, 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := store.Import(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(filepath.ToSlash(key), "/")
	if len(parts) != 4 || parts[0] != "sha256" || len(parts[1]) != 2 || len(parts[2]) != 2 {
		t.Fatalf("storage key = %q, want sharded sha256 key", key)
	}
	path := filepath.Join(cfg.App.DataDir, "people", filepath.FromSlash(key))
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("stored image mismatch: err=%v bytes=%d", err, len(got))
	}
	person := model.Person{Base: model.Base{ID: "person-1"}, Name: "Actor", OriginalName: "Actor", NormalizedName: "actor", ProfileURL: source, ProfileImageKey: key, Source: "local"}
	if err := repos.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handled, err := store.ServePerson(t.Context(), rec, httptest.NewRequest("GET", "/Items/person-1/Images/Primary", nil), person.ID)
	if !handled || err != nil {
		t.Fatalf("ServePerson handled=%v err=%v", handled, err)
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("served image mismatch: got %d bytes, want %d", rec.Body.Len(), len(want))
	}

	secondKey, err := store.Import(t.Context(), source)
	if err != nil || secondKey != key {
		t.Fatalf("repeat import key=%q err=%v, want %q", secondKey, err, key)
	}
}

func TestPeopleImageStoreRepairsSameSizeCorruption(t *testing.T) {
	store, _, cfg := newPeopleImageStoreTest(t)
	source := filepath.Join(cfg.App.DataDir, "source.png")
	want := testArtworkPNG(t, 4, 3)
	if err := os.WriteFile(source, want, 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := store.Import(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.App.DataDir, "people", filepath.FromSlash(key))
	corrupt := append([]byte(nil), want...)
	corrupt[len(corrupt)-1] ^= 0xff
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Import(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("same-size corruption was not repaired: err=%v", err)
	}
}

func TestPeopleImageStoreRemoteImportBypassesImageCache(t *testing.T) {
	store, _, cfg := newPeopleImageStoreTest(t)
	want := testArtworkPNG(t, 3, 2)
	var calls int32
	store.imageProxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(want)),
			Request:    req,
		}, nil
	})}
	source := "https://image.tmdb.org/t/p/w500/person.png"
	_, _, failPath := store.imageProxy.remoteImageCachePathsForValidated(source)
	if err := os.MkdirAll(filepath.Dir(failPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failPath, []byte("existing failure marker"), 0o600); err != nil {
		t.Fatal(err)
	}

	key, err := store.Import(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want direct fetch despite cache failure marker", got)
	}
	if got, err := os.ReadFile(filepath.Join(cfg.App.DataDir, "people", filepath.FromSlash(key))); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("stored people image mismatch: err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.Cache.CacheDir, "images"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(failPath) {
		t.Fatalf("cache entries changed during direct people import: %#v", entries)
	}
	if got, err := os.ReadFile(failPath); err != nil || string(got) != "existing failure marker" {
		t.Fatalf("failure marker changed: %q err=%v", got, err)
	}
}

func TestPeopleImageStoreDoesNotDownloadDuringServe(t *testing.T) {
	store, repos, _ := newPeopleImageStoreTest(t)
	var calls int32
	store.imageProxy.client = &http.Client{Transport: imageRoundTripFunc(func(*http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("unexpected request")
	})}
	person := model.Person{Base: model.Base{ID: "person-missing-key"}, Name: "Actor", OriginalName: "Actor", NormalizedName: "actor-missing-key", ProfileURL: "https://image.tmdb.org/t/p/w500/missing.png", Source: "tmdb"}
	if err := repos.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handled, err := store.ServePerson(t.Context(), rec, httptest.NewRequest("GET", "/Items/person-missing-key/Images/Primary", nil), person.ID)
	if !handled || !errors.Is(err, ErrPeopleImageNotFound) {
		t.Fatalf("ServePerson handled=%v err=%v, want missing image", handled, err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("request-time upstream calls = %d, want 0", got)
	}
}

func TestPersistCreditsReusesRemoteProfile(t *testing.T) {
	store, repos, cfg := newPeopleImageStoreTest(t)
	want := testArtworkPNG(t, 3, 2)
	var calls int32
	var reject atomic.Bool
	var lastMetadataID string
	store.imageProxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		if reject.Load() {
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("not found")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(want)), Request: req}, nil
	})}
	scraper := &ScraperService{repo: repos, people: store, log: zap.NewNop()}
	credits := []PersonCredit{{Provider: "tmdb", ExternalID: "123", Name: "Actor", ProfileURL: "https://image.tmdb.org/t/p/original/actor.png", Type: model.CreditTypeActor}}
	for i := 0; i < 2; i++ {
		item := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Work", Source: "tmdb"}
		if err := repos.DB.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		if err := scraper.persistCredits(t.Context(), item.ID, []string{model.CreditTypeActor}, credits); err != nil {
			t.Fatal(err)
		}
		lastMetadataID = item.ID
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("shared profile downloads = %d, want 1", got)
	}
	var person model.Person
	if err := repos.DB.First(&person).Error; err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.App.DataDir, "people", filepath.FromSlash(person.ProfileImageKey))
	// 有效但内容不同的图片也不能冒充缓存键对应的原图。
	if err := os.WriteFile(path, testArtworkPNG(t, 4, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := store.ImportCached(t.Context(), credits[0].ProfileURL)
	if err != nil || key != person.ProfileImageKey {
		t.Fatalf("repair key=%q err=%v", key, err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("repair downloads = %d, want 2", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportCached(t.Context(), credits[0].ProfileURL); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("missing image downloads = %d, want 3", got)
	}
	if err := scraper.persistCredits(t.Context(), lastMetadataID, []string{model.CreditTypeActor}, credits, true); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("explicit import downloads = %d, want 4", got)
	}
	reject.Store(true)
	changedURL := "https://image.tmdb.org/t/p/original/changed.png"
	for i := 0; i < 2; i++ {
		if _, err := store.ImportCached(t.Context(), changedURL); err == nil {
			t.Fatal("expected upstream failure")
		}
	}
	if got := atomic.LoadInt32(&calls); got != 5 {
		t.Fatalf("failed import cooldown downloads = %d, want 5", got)
	}
	reject.Store(false)
	if _, err := store.Import(t.Context(), changedURL); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportCached(t.Context(), changedURL); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 6 {
		t.Fatalf("explicit recovery downloads = %d, want 6", got)
	}
	// 冷却到期后自动入口恢复请求，不需要重启或手动清理。
	store.sources.SetJSON(t.Context(), changedURL, peopleImageSource{Failed: true}, time.Minute)
	store.sources.mu.Lock()
	entry := store.sources.memory[changedURL]
	entry.expiresAt = time.Now().Add(-time.Second)
	store.sources.memory[changedURL] = entry
	store.sources.mu.Unlock()
	if _, err := store.ImportCached(t.Context(), changedURL); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 7 {
		t.Fatalf("expired cooldown downloads=%d, want 7", got)
	}
}

func TestEmbyPersonImageURLDoesNotExposeProfileSource(t *testing.T) {
	_, repos, cfg := newPeopleImageStoreTest(t)
	person := model.Person{Base: model.Base{ID: "person-source-only"}, Name: "Actor", OriginalName: "Actor", NormalizedName: "actor-source-only", ProfileURL: "https://image.tmdb.org/t/p/w500/source-only.png", Source: "tmdb"}
	if err := repos.DB.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	got, err := NewEmbyService(cfg, zap.NewNop(), repos).ImageURL(t.Context(), person.ID, "Primary")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("person image URL = %q, want no remote fallback", got)
	}
}

func TestPersistCreditsStoresAndPreservesPersonImageKey(t *testing.T) {
	store, repos, cfg := newPeopleImageStoreTest(t)
	source := filepath.Join(cfg.App.DataDir, "credit.png")
	if err := os.WriteFile(source, testArtworkPNG(t, 3, 3), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{PermanentBase: model.PermanentBase{ID: "metadata-people-image"}, Kind: model.MetadataKindMovie, Title: "Film", Source: "local"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	scraper := &ScraperService{repo: repos, people: store, log: zap.NewNop()}
	err := scraper.persistCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, []PersonCredit{{Provider: "local", ExternalID: "actor-1", Name: "Actor", ProfileURL: source, Type: model.CreditTypeActor}})
	if err != nil {
		t.Fatal(err)
	}
	var saved model.Person
	if err := repos.DB.Where("source = ? AND normalized_name = ?", "local", "actor").First(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ProfileImageKey == "" {
		t.Fatal("person image key is empty")
	}
	oldKey := saved.ProfileImageKey
	if _, err := os.Stat(filepath.Join(cfg.App.DataDir, "people", filepath.FromSlash(oldKey))); err != nil {
		t.Fatalf("persisted person image missing: %v", err)
	}
	missing := filepath.Join(cfg.App.DataDir, "missing.png")
	err = scraper.persistCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, []PersonCredit{{Provider: "local", ExternalID: "actor-1", Name: "Actor", ProfileURL: missing, Type: model.CreditTypeActor}})
	if err != nil {
		t.Fatalf("failed image refresh must not fail credit persistence: %v", err)
	}
	if err := repos.DB.Where("source = ? AND normalized_name = ?", "local", "actor").First(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ProfileImageKey != oldKey || saved.ProfileURL != missing {
		t.Fatalf("failed refresh key=%q source=%q, want key=%q source=%q", saved.ProfileImageKey, saved.ProfileURL, oldKey, missing)
	}
}
