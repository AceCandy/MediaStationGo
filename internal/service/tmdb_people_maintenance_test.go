package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTMDbPeopleMaintenanceWithoutMediaAndEmptyCredits(t *testing.T) {
	calls := map[string]int{}
	center, _ := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/movie/11":
			writePeopleResponse(t, w, r, `{"id":11,"title":"电影","credits":{"cast":[{"id":77,"name":"演员"}],"crew":[]}}`)
		case "/tv/12":
			writePeopleResponse(t, w, r, `{"id":12,"name":"电视剧","credits":{"cast":[],"crew":[]}}`)
		case "/tv/12/season/1":
			writePeopleResponse(t, w, r, `{"id":13,"season_number":1,"name":"第一季","credits":{"cast":[],"crew":[]},"episodes":[]}`)
		case "/person/77":
			writePeopleResponse(t, w, r, `{"id":77,"name":"演员","biography":""}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := t.Context()
	for _, seed := range []struct {
		kind, body string
		id         int
	}{
		{"movie", `{"id":11,"title":"电影"}`, 11},
		{"series", `{"id":12,"name":"电视剧","seasons":[{"id":13,"season_number":1,"name":"第一季"}]}`, 12},
	} {
		if err := center.repo.TMDb.SaveResponse(ctx, repository.TMDbSourceRequest{Kind: seed.kind, TMDbID: seed.id, Component: "base", Language: "zh-CN", Origin: "api"}, []byte(seed.body), time.Now(), ""); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		metrics := map[string]int64{}
		budget := &tmdbBudget{limit: 100}
		if err := center.maintainPeople(context.WithValue(ctx, tmdbBudgetKey{}, budget), 100, metrics, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/movie/11", "/tv/12", "/tv/12/season/1", "/person/77"} {
		if calls[path] != 1 {
			t.Fatalf("%s requested %d times", path, calls[path])
		}
	}
	rows, err := center.repo.TMDb.PeopleMaintenanceCandidates(ctx, 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("loaded empty response selected again: %+v %v", rows, err)
	}
	for _, table := range []string{"metadata_items", "media"} {
		var n int64
		if err := center.repo.DB.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("local identities created: %s %d %v", table, n, err)
		}
	}
	var credits int64
	if err := center.repo.DB.Model(&model.TMDbCredit{}).Count(&credits).Error; err != nil || credits != 1 {
		t.Fatalf("source credits=%d %v", credits, err)
	}
}

func TestTMDbPeopleMaintenanceBudgetRetryAndCancellation(t *testing.T) {
	calls := 0
	center, _ := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/21") {
			http.Error(w, "failed", 500)
			return
		}
		writePeopleResponse(t, w, r, `{"id":22,"title":"电影","credits":{"cast":[],"crew":[]}}`)
	})
	ctx := t.Context()
	for _, row := range []model.TMDbSubject{
		{PermanentBase: model.PermanentBase{ID: "a"}, Kind: "movie", TMDbID: 21},
		{PermanentBase: model.PermanentBase{ID: "b"}, Kind: "movie", TMDbID: 22},
	} {
		if err := center.repo.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	budget := &tmdbBudget{limit: 1}
	metrics := map[string]int64{}
	if err := center.maintainPeople(context.WithValue(ctx, tmdbBudgetKey{}, budget), 100, metrics, nil); err == nil || errors.Is(err, errTMDbBudget) {
		t.Fatalf("budget error=%v", err)
	}
	if calls != 1 || metrics["failed"] != 1 {
		t.Fatalf("calls=%d metrics=%v", calls, metrics)
	}
	rows, err := center.repo.TMDb.PeopleMaintenanceCandidates(ctx, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != "b" {
		t.Fatalf("failed source blocked later candidate: %+v %v", rows, err)
	}
	var pending int64
	if err := center.repo.DB.Model(&model.TMDbRefreshJob{}).Where("explicit_pending=true").Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("automatic budget escaped to explicit worker: %d %v", pending, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := center.maintainPeople(canceled, 100, map[string]int64{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if calls != 1 {
		t.Fatal("canceled pass requested upstream")
	}
	if err := center.maintainPeople(context.WithValue(ctx, tmdbBudgetKey{}, &tmdbBudget{limit: 1}), 100, map[string]int64{}, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("eligible retry requests=%d", calls)
	}
}

// 模拟上游为每个实际请求的附加组件返回有效空对象。
func writePeopleResponse(t *testing.T, w http.ResponseWriter, r *http.Request, body string) {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		t.Fatal(err)
	}
	for _, component := range strings.Split(r.URL.Query().Get("append_to_response"), ",") {
		if component != "" && data[component] == nil {
			data[component] = map[string]any{}
		}
	}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		t.Error(err)
	}
}

func TestTMDbPeopleMaintenanceRejectsIncompleteCredits(t *testing.T) {
	center, _ := tmdbCenterTest(t, func(w http.ResponseWriter, r *http.Request) {
		writePeopleResponse(t, w, r, `{"id":31,"title":"电影","credits":{"cast":[],"crew":null}}`)
	})
	ctx := t.Context()
	subject := model.TMDbSubject{Kind: "movie", TMDbID: 31}
	if err := center.repo.DB.Create(&subject).Error; err != nil {
		t.Fatal(err)
	}
	metrics := map[string]int64{}
	if err := center.maintainPeople(context.WithValue(ctx, tmdbBudgetKey{}, &tmdbBudget{limit: 1}), 100, metrics, nil); err == nil || metrics["failed"] != 1 {
		t.Fatalf("incomplete credits accepted: %v %v", err, metrics)
	}
	loaded, err := center.repo.TMDb.CreditsLoaded(ctx, subject.ID)
	if err != nil || loaded {
		t.Fatalf("incomplete evidence: %v %v", loaded, err)
	}
	rows, err := center.repo.TMDb.PeopleMaintenanceCandidates(ctx, 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("failed request did not back off: %v %v", rows, err)
	}
	if err := center.repo.DB.Model(&subject).UpdateColumn("next_people_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	rows, err = center.repo.TMDb.PeopleMaintenanceCandidates(ctx, 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("incomplete source lost retry: %v %v", rows, err)
	}
}
