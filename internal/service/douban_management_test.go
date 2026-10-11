package service

import (
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
)

func TestDoubanManagementRefreshKeepsLocalDataAndOldSuccessOnFailure(t *testing.T) {
	var calls atomic.Int64
	var fail atomic.Bool
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/subject/100/" {
			return doubanCacheTestResponse(req, 200, doubanRichFixture), nil
		}
		calls.Add(1)
		if req.URL.Path != "/rexxar/api/v2/tv/100" {
			t.Errorf("wrong actual kind: %s", req.URL.Path)
		}
		if fail.Load() {
			return doubanCacheTestResponse(req, 503, "unavailable"), nil
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"tv","title":"最新共享剧集","intro":"共享简介","rating":9,"year":"2026","genres":["剧情"]}`), nil
	})
	local := createServiceTestMetadata(t, repos.DB, model.MetadataItem{Kind: "movie", Title: "本地标题", Overview: "本地简介", Rating: 6, Source: "tmdb"}, model.MetadataIdentifier{Provider: "douban", EntityKind: "movie", DoubanEntityKind: "series", ExternalID: "100"})
	before, err := repos.Metadata.FindByID(t.Context(), local.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if err := repos.Douban.Save(t.Context(), "100", "series", []byte(`{"title":"缓存标题","rating":8}`), model.DoubanQualityComplete, old); err != nil {
		t.Fatal(err)
	}
	stored, err := repos.Douban.Find(t.Context(), "100", "series")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := provider.SubjectDetail(t.Context(), stored.ID)
	if err != nil || detail.Title != "缓存标题" || calls.Load() != 0 {
		t.Fatalf("read triggered network: %#v calls=%d err=%v", detail, calls.Load(), err)
	}
	result, err := provider.RefreshSubject(t.Context(), stored.ID)
	if err != nil || result.Degraded || result.Subject.Rating != 9 || result.Subject.Title != "最新共享剧集" || result.Subject.Year != "2026" || calls.Load() != 1 {
		t.Fatalf("forced fresh refresh %#v calls=%d err=%v", result, calls.Load(), err)
	}
	after, err := repos.Metadata.FindByID(t.Context(), local.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("refresh changed canonical data: %#v -> %#v %v", before, after, err)
	}
	fail.Store(true)
	if _, err := provider.RefreshSubject(t.Context(), stored.ID); !errors.Is(err, ErrDoubanTemporarilyUnavailable) {
		t.Fatalf("forced failure was hidden: %v", err)
	}
	failed, err := provider.SubjectDetail(t.Context(), stored.ID)
	if err != nil || failed.Rating != 9 || !failed.FetchedAt.Equal(*result.Subject.FetchedAt) || failed.NextRetryAt == nil || calls.Load() != 2 {
		t.Fatalf("failed refresh lost success: %#v calls=%d err=%v", failed, calls.Load(), err)
	}
	if _, err := provider.RefreshSubject(t.Context(), uuid.NewString()); !errors.Is(err, ErrDoubanSubjectMissing) || calls.Load() != 2 {
		t.Fatalf("missing local row performed remote lookup: %v", err)
	}
}

func TestDoubanManagementDegradedReadbackAndHistoricalReadOnly(t *testing.T) {
	var calls atomic.Int64
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Path == "/rexxar/api/v2/movie/100" {
			return doubanCacheTestResponse(req, 403, "forbidden"), nil
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"movie","title":"降级新标题","rating":7}`), nil
	})
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"title":"旧完整资料","rating":9}`), model.DoubanQualityComplete, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	stored, _ := repos.Douban.Find(t.Context(), "100", "movie")
	result, err := provider.RefreshSubject(t.Context(), stored.ID)
	if err != nil || !result.Degraded || result.Subject.Title != "旧完整资料" || result.Subject.Rating != 9 || result.Subject.Quality != "complete" || result.Subject.NextRetryAt == nil {
		t.Fatalf("downgrade readback %#v %v", result, err)
	}
	if err := repos.Douban.Save(t.Context(), "200", "episode", []byte(`{"data":{"title":"历史单集","intro":"单集简介"}}`), model.DoubanQualityPartial, time.Now()); err != nil {
		t.Fatal(err)
	}
	historical, _ := repos.Douban.Find(t.Context(), "200", "episode")
	detail, err := provider.SubjectDetail(t.Context(), historical.ID)
	if err != nil || detail.Title != "历史单集" || detail.Overview != "单集简介" {
		t.Fatalf("historical details %#v %v", detail, err)
	}
	if _, err := provider.RefreshSubject(t.Context(), historical.ID); !errors.Is(err, ErrDoubanSubjectReadOnly) || calls.Load() != 2 {
		t.Fatalf("historical refresh caused network calls=%d err=%v", calls.Load(), err)
	}
	if err := repos.Douban.RecordFailure(t.Context(), "300", "movie", time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	placeholder, err := repos.Douban.Find(t.Context(), "300", "movie")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = provider.SubjectDetail(t.Context(), placeholder.ID)
	if err != nil || detail.HasPayload || len(detail.RawJSON) != 0 || detail.FetchedAt != nil || detail.NextRetryAt == nil || calls.Load() != 2 {
		t.Fatalf("failed placeholder should remain readable without network: %#v %v", detail, err)
	}
}

func TestDoubanManagementBaseRefreshPreservesRichState(t *testing.T) {
	calls := 0
	provider, repos := newDoubanCacheTestProvider(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/rexxar/api/v2/movie/100" {
			t.Fatalf("base mode requested %s", req.URL.Path)
		}
		return doubanCacheTestResponse(req, 200, `{"id":"100","type":"movie","title":"基础更新","rating":9}`), nil
	})
	if err := repos.Douban.Save(t.Context(), "100", "movie", []byte(`{"id":"100","type":"movie","title":"旧资料"}`), model.DoubanQualityComplete, time.Now()); err != nil {
		t.Fatal(err)
	}
	row, _ := repos.Douban.Find(t.Context(), "100", "movie")
	rich, at := `{"writers":["已保存编剧"]}`, time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	if err := repos.DB.Model(row).Updates(map[string]any{"rich_payload": rich, "rich_fetched_at": at, "rich_next_retry_at": at.Add(2 * time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	before, _ := repos.Douban.Find(t.Context(), "100", "movie")
	result, err := provider.RefreshSubjectBase(t.Context(), row.ID)
	after, _ := repos.Douban.Find(t.Context(), "100", "movie")
	if err != nil || calls != 1 || result.Degraded || result.SupplementUnavailable || result.Subject.Rating != 9 || !reflect.DeepEqual(before.RichPayload, after.RichPayload) || !reflect.DeepEqual(before.RichFetchedAt, after.RichFetchedAt) || !reflect.DeepEqual(before.RichNextRetryAt, after.RichNextRetryAt) {
		t.Fatalf("base-only force: %#v calls=%d err=%v", result, calls, err)
	}
}
