package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoWakeupCoalescesAndCancelClearsPending(t *testing.T) {
	s := &HongGuoService{}
	s.runMu.Lock()
	for range 10 {
		s.requestRefresh(t.Context())
	}
	if !s.refreshRequested {
		t.Fatal("busy worker lost wakeup")
	}
	s.Cancel()
	if s.refreshRequested {
		t.Fatal("cancel retained wakeup")
	}
	s.runMu.Unlock()
	s.Wait()
	s.requestRefresh(context.Background())
	if s.refreshRequested {
		t.Fatal("closed service accepted wakeup")
	}
}

func TestHongGuoWakeupDrainsLaterDiscoveries(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	tasks := NewTaskTrackerService(zap.NewNop(), nil)
	tasks.ConfigurePersistence(repo.TaskExecution, t.TempDir())
	s := NewHongGuoService(repo, tasks, nil, t.TempDir())
	t.Cleanup(s.Wait)
	started := make(chan string, 4)
	release := make(chan struct{})
	s.client = hongguo.NewClient(&http.Client{Transport: hongGuoTestTransport(func(r *http.Request) (*http.Response, error) {
		id := r.URL.Query().Get("series_id")
		started <- id
		if id == "90001" {
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		body := fmt.Sprintf(`_ROUTER_DATA={"loaderData":{"detail_page":{"seriesDetail":{"series_id":"%s","series_name":"测试","episode_cnt":1,"episode_right_text":"全1集"}}}}`, id)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	queue := func(id string) {
		t.Helper()
		if err := repo.HongGuo.QueueMissingSearchResults(t.Context(), []repository.HongGuoListWork{{HongGuoWork: model.HongGuoWork{SourceID: id, Title: "测试"}}}); err != nil {
			t.Fatal(err)
		}
		s.requestRefresh(t.Context())
	}
	await := func(want string) {
		t.Helper()
		select {
		case got := <-started:
			if got != want {
				t.Fatalf("got %s want %s", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("refresh was not awakened")
		}
	}
	queue("90001")
	await("90001")
	queue("90002")
	s.requestRefresh(t.Context())
	close(release)
	await("90002")
	// 等待当前任务释放锁，避免取消正在保存的第二个作品。
	s.runMu.Lock()
	s.runMu.Unlock()
	if _, err := repo.HongGuo.FindBySourceID(t.Context(), "90002"); err != nil {
		t.Fatal(err)
	}
	page, err := tasks.ListSystem(model.TaskSystemHongGuo, 1, 10)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("tasks=%+v err=%v", page, err)
	}
	for _, item := range page.Items {
		if item.Trigger != TaskTriggerEvent {
			t.Fatalf("trigger=%s", item.Trigger)
		}
	}
}
