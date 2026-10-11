package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func newDoubanPosterTestStore(t *testing.T, repos *repository.Container, transport roundTripFunc) *ArtworkStore {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{App: config.AppConfig{DataDir: dir}, Cache: config.CacheConfig{CacheDir: filepath.Join(dir, "cache")}}
	proxy := NewImageProxy(cfg, zap.NewNop())
	proxy.client = &http.Client{Transport: transport}
	return NewArtworkStore(cfg, repos.Artwork, proxy)
}

func saveDoubanPosterSubject(t *testing.T, repos *repository.Container, source string, at time.Time) *model.DoubanSubject {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"id": "100", "type": "movie", "title": "共享电影", "rating": 9, "pic": map[string]any{"large": source}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.Douban.Save(t.Context(), "100", "movie", payload, model.DoubanQualityComplete, at); err != nil {
		t.Fatal(err)
	}
	subject, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || subject == nil {
		t.Fatalf("subject: %#v %v", subject, err)
	}
	return subject
}

func posterResponse(req *http.Request, status int, data []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(data)), Request: req}
}

func TestDoubanPosterUnboundPersistenceRepairAndFailure(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(*http.Request) (*http.Response, error) {
		t.Error("poster read called subject API")
		return nil, errors.New("unexpected subject request")
	})
	var calls atomic.Int64
	var failed atomic.Bool
	data := testArtworkPNG(t, 120, 180)
	store := newDoubanPosterTestStore(t, repos, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if failed.Load() {
			return posterResponse(req, 404, nil), nil
		}
		return posterResponse(req, 200, data), nil
	})
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	subject := saveDoubanPosterSubject(t, repos, "https://img1.doubanio.com/view/photo/l/public/first.png", at)
	rows, _, err := provider.ListSubjects(t.Context(), "", "", 1, 30)
	if err != nil || len(rows) != 1 || rows[0].PosterURL != doubanSubjectPosterURL(subject.ID, true) || calls.Load() != 0 {
		t.Fatalf("list downloaded image: %#v %v calls=%d", rows, err, calls.Load())
	}
	asset, _, err := provider.subjectPoster(t.Context(), subject.ID, store)
	if err != nil || asset == nil || calls.Load() != 1 {
		t.Fatalf("first poster: %#v %v calls=%d", asset, err, calls.Load())
	}
	stored, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if stored.PosterAssetID == nil || *stored.PosterAssetID != asset.ID || !stored.FetchedAt.Equal(at) || !stored.LastAttemptAt.Equal(at) || stored.NextRetryAt != nil {
		t.Fatalf("poster changed subject state: %#v", stored)
	}
	for _, table := range []string{"metadata_items", "metadata_artworks", "metadata_artwork_candidates"} {
		var count int64
		if err := repos.DB.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("poster created media state in %s: %d %v", table, count, err)
		}
	}
	failed.Store(true)
	// 临时下载缓存丢失后仍只读持久原图，并验证变体与 HEAD。
	if err := os.RemoveAll(store.imageProxy.cacheDir); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/poster?maxWidth=40&format=png", nil)
		if err := provider.ServeSubjectPoster(t.Context(), w, r, subject.ID, store); err != nil || w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-cache" || calls.Load() != 1 {
			t.Fatalf("local %s: %d %v %v calls=%d", method, w.Code, w.Header(), err, calls.Load())
		}
		if method == http.MethodGet {
			cfg, _, err := image.DecodeConfig(w.Body)
			if err != nil || cfg.Width != 40 || cfg.Height != 60 {
				t.Fatalf("variant: %#v %v", cfg, err)
			}
		} else if w.Body.Len() != 0 {
			t.Fatal("HEAD returned image body")
		}
	}
	// 丢失原图时从下载缓存/上游修复，仍复用原资产 ID。
	path, _ := store.pathForStorageKey(asset.StorageKey)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	failed.Store(false)
	if err := os.MkdirAll(store.imageProxy.cacheDir, 0o750); err != nil {
		t.Fatal(err)
	}
	repaired, _, err := provider.subjectPoster(t.Context(), subject.ID, store)
	if err != nil || repaired.ID != asset.ID || calls.Load() != 2 {
		t.Fatalf("repair: %#v %v calls=%d", repaired, err, calls.Load())
	}
	failed.Store(true)
	newSubject := saveDoubanPosterSubject(t, repos, "https://img1.doubanio.com/view/photo/l/public/second.png", at.Add(time.Minute))
	for range 2 {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/poster?maxWidth=40&format=png", nil)
		if err := provider.ServeSubjectPoster(t.Context(), w, r, subject.ID, store); err != nil || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("fallback cached old variant: %v %v", w.Header(), err)
		}
	}
	afterFailure, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if !reflect.DeepEqual(newSubject, afterFailure) || *afterFailure.PosterAssetID != asset.ID || calls.Load() != 3 {
		t.Fatalf("failure changed state or retried upstream: %#v calls=%d", afterFailure, calls.Load())
	}
	// 再次更换来源并得到不同图片，只有成功图片才能推进引用。
	failed.Store(false)
	data = testArtworkPNG(t, 80, 120)
	latest := saveDoubanPosterSubject(t, repos, "https://img1.doubanio.com/view/photo/l/public/third.png", at.Add(2*time.Minute))
	changed, _, err := provider.subjectPoster(t.Context(), subject.ID, store)
	if err != nil || changed.ID == asset.ID {
		t.Fatalf("source change: %#v %v", changed, err)
	}
	current, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if !current.FetchedAt.Equal(*latest.FetchedAt) || current.PosterSourceURL != provider.posterSource(t.Context(), latest) {
		t.Fatalf("source association: %#v", current)
	}
	if err := repos.DB.Delete(changed).Error; err != nil {
		t.Fatal(err)
	}
	current, _ = repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if current.PosterAssetID != nil || current.Rating != 9 {
		t.Fatalf("asset deletion did not preserve subject: %#v", current)
	}
}

func TestDoubanPosterReusesMediaAssetWithoutSelectingIt(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, nil)
	store := newDoubanPosterTestStore(t, repos, func(*http.Request) (*http.Response, error) {
		t.Error("available media asset was downloaded")
		return nil, errors.New("unexpected image request")
	})
	source := "https://img1.doubanio.com/view/photo/l/public/shared.png"
	subject := saveDoubanPosterSubject(t, repos, source, time.Now().UTC())
	// 本地作品为电影、豆瓣实际条目也为电影；只按明确身份及来源复用。
	local := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "本地标题", Rating: 6, Source: "tmdb"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "movie", ExternalID: "100"})
	prepared, err := store.prepareAsset(local.ID, "poster", testArtworkPNG(t, 40, 60))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := repos.Artwork.SaveAsset(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataArtworkCandidate{MetadataID: local.ID, ArtworkType: "poster", SourceProvider: "douban", SourceURL: source, AssetID: asset.ID}).Error; err != nil {
		t.Fatal(err)
	}
	before, _ := repos.Metadata.FindByID(t.Context(), local.ID)
	got, _, err := provider.subjectPoster(t.Context(), subject.ID, store)
	if err != nil || got.ID != asset.ID {
		t.Fatalf("reuse: %#v %v", got, err)
	}
	after, _ := repos.Metadata.FindByID(t.Context(), local.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("poster reuse changed canonical metadata")
	}
	var selection model.MetadataArtwork
	if err := repos.DB.First(&selection, "metadata_id = ?", local.ID).Error; err == nil {
		t.Fatal("candidate was selected by the data center")
	}
}

func TestDoubanPosterConcurrentAndLateDownloads(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, nil)
	var calls atomic.Int64
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	data := testArtworkPNG(t, 40, 60)
	store := newDoubanPosterTestStore(t, repos, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return posterResponse(req, 200, data), nil
	})
	at := time.Now().UTC().Add(-time.Minute)
	subject := saveDoubanPosterSubject(t, repos, "https://img1.doubanio.com/view/photo/l/public/old.png", at)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := provider.subjectPoster(t.Context(), subject.ID, store)
			if err != nil && !errors.Is(err, ErrArtworkNotFound) {
				t.Errorf("late download: %v", err)
			}
		}()
	}
	<-started
	newSubject := saveDoubanPosterSubject(t, repos, "", at.Add(time.Minute))
	close(release)
	wg.Wait()
	current, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if !reflect.DeepEqual(current, newSubject) || current.PosterAssetID != nil || calls.Load() != 1 {
		t.Fatalf("late image overwrote refreshed subject: %#v calls=%d", current, calls.Load())
	}
	// 当前来源多请求复用同一图片/资产/引用，数据库 CAS 只接受胜出状态。
	subject = saveDoubanPosterSubject(t, repos, "https://img1.doubanio.com/view/photo/l/public/current.png", at.Add(2*time.Minute))
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			asset, _, err := provider.subjectPoster(t.Context(), subject.ID, store)
			if err != nil || asset == nil {
				t.Errorf("concurrent image: %#v %v", asset, err)
			}
		}()
	}
	wg.Wait()
	var count int64
	repos.DB.Model(&model.ArtworkAsset{}).Count(&count)
	if count != 1 || calls.Load() != 2 {
		t.Fatalf("concurrent dedup: assets=%d calls=%d", count, calls.Load())
	}
}

func TestDoubanPosterInvalidImagesAreNotPersisted(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, nil)
	store := newDoubanPosterTestStore(t, repos, func(req *http.Request) (*http.Response, error) {
		return posterResponse(req, 200, []byte("not an image")), nil
	})
	subject := saveDoubanPosterSubject(t, repos, "https://example.com/invalid.png", time.Now().UTC())
	if _, _, err := provider.subjectPoster(t.Context(), subject.ID, store); !errors.Is(err, ErrArtworkNotFound) {
		t.Fatalf("invalid image accepted: %v", err)
	}
	var count int64
	repos.DB.Model(&model.ArtworkAsset{}).Count(&count)
	if count != 0 {
		t.Fatal("invalid image asset persisted")
	}
	stored, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if !reflect.DeepEqual(stored, subject) {
		t.Fatal("invalid image changed subject state")
	}
}
