package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

const doubanRichFixture = `<html><head><meta property="og:url" content="https://movie.douban.com/subject/100/"></head><body><h1>电影 (2025)</h1><div id="info"><span><span class="pl">编剧</span>: <span class="attrs"><a>杨子</a> / <a>第二编剧</a></span></span><br><span class="pl">IMDb:</span> tt34610311<br></div><div class="ratings-on-weight"><div class="item"><span class="stars5 starstop">5星</span><span class="rating_per">27.3%</span></div><div class="item"><span class="starstop">4星</span><span class="rating_per">NaN%</span></div></div><div id="celebrities"><li class="celebrity"><div class="avatar" style="background-image: url(https://img3.doubanio.com/view/personage/m/public/avatar.jpg)"></div><span class="name"><a>成龙</a></span><span class="role" title="饰 黄德忠">饰 黄德忠</span></li></div><ul class="award"><li><a>第44届香港电影金像奖</a></li><li>最佳男主角</li><li><a>梁家辉</a></li></ul></body></html>`

func TestDoubanRichHTMLIdentityAndFields(t *testing.T) {
	rich, err := parseDoubanRichHTML([]byte(doubanRichFixture), "100")
	if err != nil || len(rich.Writers) != 2 || rich.IMDbID != "tt34610311" || len(rich.Distribution) != 1 || rich.Distribution[0].Percent != 27.3 || len(rich.Credits) != 1 || rich.Credits[0].Role != "饰 黄德忠" || len(rich.Awards) != 1 || rich.Awards[0].People != "梁家辉" {
		t.Fatalf("rich %#v: %v", rich, err)
	}
	for _, body := range []string{strings.ReplaceAll(doubanRichFixture, "/subject/100/", "/subject/200/"), `<html><h1>安全验证</h1></html>`, strings.ReplaceAll(doubanRichFixture, `id="info"`, `id="missing"`)} {
		if _, err := parseDoubanRichHTML([]byte(body), "100"); err == nil {
			t.Fatal("accepted invalid page")
		}
	}
	for _, source := range []string{"http://img3.doubanio.com/view/a.jpg", "https://evil.example/view/a.jpg", "https://img3.doubanio.com.evil.example/view/a.jpg", "https://img3.doubanio.com:123/view/a.jpg", "https://user@img3.doubanio.com/view/a.jpg"} {
		if validDoubanProfileURL(source) {
			t.Fatalf("accepted source %q", source)
		}
	}
}

func TestDoubanRichRefreshPreservesAPICredits(t *testing.T) {
	p, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/subject/100/" {
			t.Errorf("unexpected webpage request %s", req.URL.Path)
		}
		return doubanCacheTestResponse(req, 200, doubanRichFixture), nil
	})
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"id":"100","title":"基础资料"}`), model.DoubanQualityComplete, time.Now()); err != nil {
		t.Fatal(err)
	}
	row, err := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil {
		t.Fatal(err)
	}
	rich := `{"credits":[{"douban_id":"4","name":"接口独有人员","type":"Actor","role":"饰 已知角色","source_url":"https://img3.doubanio.com/view/personage/m/public/person.jpg","image_key":"existing-key"}]}`
	if _, err = repos.Douban.SaveAPICredits(t.Context(), row, []byte(rich), time.Now()); err != nil {
		t.Fatal(err)
	}
	if saved, err := p.refreshRichSubject(t.Context(), "100", "movie", true); err != nil || !saved {
		t.Fatalf("webpage refresh %v %v", saved, err)
	}
	row, err = repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil {
		t.Fatal(err)
	}
	result := doubanRichPayload(row)
	if len(result.Credits) != 2 || result.Credits[0].DoubanID != "4" || result.Credits[0].Role != "饰 已知角色" || result.Credits[0].ImageKey != "existing-key" || result.IMDbID != "tt34610311" {
		t.Fatalf("API credits lost: %+v", result)
	}
}

func TestDoubanRichRefreshPreservesFailureAndUsesTTL(t *testing.T) {
	var calls atomic.Int64
	var blocked atomic.Bool
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Path != "/subject/100/" {
			t.Fatalf("unexpected request: %s", req.URL.Path)
		}
		body := doubanRichFixture
		if blocked.Load() {
			body = "<html>安全验证</html>"
		}
		return doubanCacheTestResponse(req, 200, body), nil
	})
	subject := saveDoubanPosterSubject(t, repos, "", time.Now().Add(-time.Hour))
	for _, force := range []bool{false, false} {
		ok, err := provider.refreshRichSubject(t.Context(), "100", "movie", force)
		if !ok || err != nil {
			t.Fatalf("refresh %v %v", ok, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("cache calls=%d", calls.Load())
	}
	stored, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	blocked.Store(true)
	ok, err := provider.refreshRichSubject(t.Context(), "100", "movie", true)
	if ok || err != nil {
		t.Fatalf("failure %v %v", ok, err)
	}
	after, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if *after.RichPayload != *stored.RichPayload || *after.Payload != *stored.Payload || !after.FetchedAt.Equal(*stored.FetchedAt) {
		t.Fatal("failed webpage changed data")
	}
	// 旧网页/头像观察值不得覆盖刚刚保存的新资料。
	raw, _ := json.Marshal(DoubanRichPayload{IMDbID: "tt1"})
	if saved, err := repos.Douban.SetRichPayload(t.Context(), subject, raw); saved || err != nil {
		t.Fatalf("stale rich CAS %v %v", saved, err)
	}
	detail, err := provider.SubjectDetail(t.Context(), subject.ID)
	if err != nil || detail.IMDbID != "tt34610311" || len(detail.Credits) != 1 || !strings.HasPrefix(detail.Credits[0].ProfileURL, "/api/catalogs/douban/subjects/") || calls.Load() != 2 {
		t.Fatalf("projection %#v %v", detail, err)
	}
}

func TestDoubanRichCreditImagePersistsAndServesVariants(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(*http.Request) (*http.Response, error) {
		t.Error("avatar requested subject API")
		return nil, errors.New("unexpected")
	})
	subject := saveDoubanPosterSubject(t, repos, "", time.Now())
	rich, _ := parseDoubanRichHTML([]byte(doubanRichFixture), "100")
	rich.FetchedAt = time.Now().UTC()
	raw, _ := json.Marshal(rich)
	if saved, err := repos.Douban.SetRichPayload(t.Context(), subject, raw); !saved || err != nil {
		t.Fatalf("save %v %v", saved, err)
	}
	observed, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	retryAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if err := repos.Douban.RecordRichFailure(t.Context(), observed, retryAt); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	data := testArtworkPNG(t, 120, 180)
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	proxy := NewImageProxy(cfg, zap.NewNop())
	proxy.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return posterResponse(req, 200, data), nil
	})}
	store := NewPeopleImageStore(cfg, repos.Person, proxy)
	serve := func(method string) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/image?maxWidth=40&format=png", nil)
		if err := provider.ServeSubjectCreditImage(t.Context(), w, r, subject.ID, 0, store); err != nil || w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-cache" {
			t.Fatalf("serve %d %v %v", w.Code, w.Header(), err)
		}
	}
	serve(http.MethodGet)
	stored, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	key := doubanRichPayload(stored).Credits[0].ImageKey
	if key == "" || !store.hasUsableImage(key) {
		t.Fatal("image not persisted")
	}
	store = NewPeopleImageStore(cfg, repos.Person, proxy)
	serve(http.MethodHead)
	if calls.Load() != 1 {
		t.Fatalf("restart redownloaded: %d", calls.Load())
	}
	if err := os.Remove(filepath.Join(store.root, key)); err != nil {
		t.Fatal(err)
	}
	serve(http.MethodGet)
	if calls.Load() != 2 {
		t.Fatal("missing image not repaired")
	}
	after, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if after.RichNextRetryAt == nil || !after.RichNextRetryAt.Equal(retryAt) {
		t.Fatal("avatar cleared webpage retry")
	}
	if !after.FetchedAt.Equal(*subject.FetchedAt) || *after.Payload != *subject.Payload {
		t.Fatal("avatar changed raw/freshness")
	}
}

func TestDoubanRichFailureDoesNotScheduleBaseRefresh(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		return doubanCacheTestResponse(req, 200, "<html>安全验证</html>"), nil
	})
	createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "本地影片"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "movie", ExternalID: "100"})
	subject := saveDoubanPosterSubject(t, repos, "", time.Now())
	now := time.Now().UTC()
	list := func(at time.Time) []model.DoubanSubject {
		t.Helper()
		rows, err := repos.Douban.ListRefreshCandidates(t.Context(), at.Add(-7*24*time.Hour), at, 100)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if len(list(now)) != 0 {
		t.Fatal("missing supplement scheduled fresh base")
	}
	if ok, err := provider.refreshRichSubject(t.Context(), "100", "movie", false); ok || err != nil {
		t.Fatalf("failure %v %v", ok, err)
	}
	stored, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if stored.RichNextRetryAt == nil || len(list(now.Add(time.Minute))) != 0 || len(list(now.Add(6*time.Minute))) != 0 {
		t.Fatal("supplement retry lost or bypassed")
	}
	rich, _ := parseDoubanRichHTML([]byte(doubanRichFixture), "100")
	rich.FetchedAt = time.Now().UTC()
	raw, _ := json.Marshal(rich)
	if saved, err := repos.Douban.SetRichPayload(t.Context(), stored, raw); !saved || err != nil {
		t.Fatalf("recovery %v %v", saved, err)
	}
	if len(list(now.Add(6*time.Minute))) != 0 {
		t.Fatal("successful supplement still due")
	}
	recovered, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if recovered.RichNextRetryAt != nil {
		t.Fatal("retry not cleared")
	}
	if err := repos.Douban.RecordRichFailure(t.Context(), stored, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	recovered, _ = repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if recovered.RichNextRetryAt != nil {
		t.Fatal("late failure covered newer success")
	}
}

func TestDoubanRichCreditLateDownloadDoesNotReplaceChangedCast(t *testing.T) {
	provider, repos := newDoubanCacheTestProvider(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected") })
	subject := saveDoubanPosterSubject(t, repos, "", time.Now())
	rich, _ := parseDoubanRichHTML([]byte(doubanRichFixture), "100")
	rich.FetchedAt = time.Now().UTC()
	raw, _ := json.Marshal(rich)
	if saved, err := repos.Douban.SetRichPayload(t.Context(), subject, raw); !saved || err != nil {
		t.Fatalf("initial %v %v", saved, err)
	}
	cfg := &config.Config{App: config.AppConfig{DataDir: t.TempDir()}, Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	proxy := NewImageProxy(cfg, zap.NewNop())
	data := testArtworkPNG(t, 120, 180)
	proxy.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		current, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
		rich.Credits[0].SourceURL = "https://img3.doubanio.com/view/personage/new.jpg"
		rich.FetchedAt = time.Now().UTC()
		raw, _ := json.Marshal(rich)
		if saved, err := repos.Douban.SetRichPayload(t.Context(), current, raw); !saved || err != nil {
			t.Fatalf("concurrent refresh %v %v", saved, err)
		}
		return posterResponse(req, 200, data), nil
	})}
	store := NewPeopleImageStore(cfg, repos.Person, proxy)
	err := provider.ServeSubjectCreditImage(t.Context(), httptest.NewRecorder(), httptest.NewRequest("GET", "/image", nil), subject.ID, 0, store)
	if !errors.Is(err, ErrPeopleImageNotFound) {
		t.Fatalf("late image result %v", err)
	}
	current, _ := repos.Douban.FindSubjectByID(t.Context(), subject.ID)
	if doubanRichPayload(current).Credits[0].ImageKey != "" {
		t.Fatal("late avatar attached to changed cast")
	}
}
