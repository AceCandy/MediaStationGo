package service

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestCatalogInventoryPreservesExistingDetails(t *testing.T) {
	calls := 0
	center, provider := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("append_to_response") != "" {
			t.Error("inventory requested extensions")
		}
		if r.URL.Path == "/tv/42" {
			fmt.Fprint(w, `{"id":42,"name":"改名剧集","overview":"新版正文","poster_path":"/new.jpg","status":"Ended","number_of_seasons":2,"seasons":[{"id":200,"season_number":1,"name":"新版季名","overview":"新版简介"},{"id":300,"season_number":2,"name":"第二季","overview":"新季简介","poster_path":"/new.jpg"}]}`)
		} else {
			fmt.Fprint(w, `{"id":200,"season_number":1,"name":"新版季名","overview":"新版简介","poster_path":"/new.jpg","episodes":[{"id":201,"episode_number":1,"name":"新版集名","overview":"新版集简介","still_path":"/new.jpg","air_date":"2026-10-02","vote_average":1},{"id":202,"episode_number":2,"name":"新的开始","overview":"新集简介","still_path":"/new.jpg","air_date":"2026-10-03"}]}`)
		}
	})
	ctx := t.Context()
	old := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	seriesReq := repository.TMDbSourceRequest{Kind: "series", TMDbID: 42, Component: "detail", Language: "zh-CN"}
	seriesRaw := `{"id":42,"name":"原剧名","overview":"原剧简介","poster_path":"/old.jpg","status":"Returning Series","seasons":[{"id":200,"season_number":1,"name":"第一季"}]}`
	seasonReq := repository.TMDbSourceRequest{Kind: "season", SeriesID: 42, Season: 1, Component: "detail", Language: "zh-CN"}
	seasonRaw := `{"id":200,"season_number":1,"name":"第一季","overview":"原季简介","poster_path":"/old.jpg","credits":{"cast":[{"id":9,"name":"原演员","credit_id":"old"}],"crew":[]},"episodes":[{"id":201,"episode_number":1,"name":"原集标题","overview":"原集简介","air_date":"2026-10-01","still_path":"/old.jpg","vote_average":8}]}`
	for _, seed := range []struct {
		req repository.TMDbSourceRequest
		raw string
	}{{seriesReq, seriesRaw}, {seasonReq, seasonRaw}} {
		if err := center.repo.TMDb.SaveResponse(ctx, seed.req, []byte(seed.raw), old, ""); err != nil {
			t.Fatal(err)
		}
	}
	var before []model.TMDbSubject
	if err := center.repo.DB.Order("kind").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	var payloads []model.TMDbPayload
	if err := center.repo.DB.Order("id").Find(&payloads).Error; err != nil {
		t.Fatal(err)
	}
	budget := &tmdbBudget{limit: 2}
	runCtx := context.WithValue(ctx, tmdbBudgetKey{}, budget)
	if err := center.maintainSubjects(runCtx, catalogMaintenanceTask{inventory: true}, 10, map[string]int64{}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || budget.used.Load() != 2 {
		t.Fatalf("calls=%d budget=%d", calls, budget.used.Load())
	}
	for _, row := range before {
		var after model.TMDbSubject
		if err := center.repo.DB.First(&after, "id=?", row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if after.Title != row.Title || after.Overview != row.Overview || after.ReleaseDate != row.ReleaseDate || !reflect.DeepEqual(after.Rating, row.Rating) || !reflect.DeepEqual(after.FetchedAt, row.FetchedAt) || after.Quality != row.Quality {
			t.Fatalf("inventory changed details: before=%+v after=%+v", row, after)
		}
		var arts []model.TMDbArtwork
		if err := center.repo.DB.Where("subject_id=?", row.ID).Find(&arts).Error; err != nil {
			t.Fatal(err)
		}
		for _, art := range arts {
			if art.SourcePath != "/old.jpg" {
				t.Fatalf("image changed: %+v", art)
			}
		}
		if row.Kind == "series" && after.Status != "Ended" {
			t.Fatal("status not updated")
		}
	}
	for _, payload := range payloads {
		var after model.TMDbPayload
		if err := center.repo.DB.First(&after, "id=?", payload.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(payload, after) {
			t.Fatal("inventory overwrote existing payload")
		}
	}
	for _, item := range []struct {
		kind string
		id   int
	}{{"season", 300}, {"episode", 202}} {
		if _, err := center.repo.TMDb.SubjectIdentity(ctx, item.kind, item.id); err != nil {
			t.Fatal("new coordinate missing", err)
		}
	}
	var credits int64
	if err := center.repo.DB.Model(&model.TMDbCredit{}).Count(&credits).Error; err != nil || credits != 1 {
		t.Fatalf("credits=%d error=%v", credits, err)
	}
	// 目录响应不能充当普通基础请求的缓存。
	req, err := center.parseRequest(ctx, provider.resolveBaseURL(ctx)+"/tv/42?language=zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := center.repo.TMDb.Cached(ctx, req, false); err != nil || len(raw) != 0 {
		t.Fatalf("inventory leaked to base cache: %s %v", raw, err)
	}
	// 显式正文保存仍可覆盖；目录缺失不能当成功。
	if err := center.repo.TMDb.SaveResponse(ctx, seriesReq, []byte(`{"id":42,"name":"手动新标题"}`), time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	got, err := center.repo.TMDb.SubjectIdentity(ctx, "series", 42)
	if err != nil || got.Title != "手动新标题" {
		t.Fatal("manual refresh blocked", err)
	}
	seriesReq.Component = "inventory"
	if err := center.repo.TMDb.SaveResponse(ctx, seriesReq, []byte(`{"id":42,"status":"Returning Series"}`), time.Now(), ""); err == nil {
		t.Fatal("missing inventory accepted")
	}
}
