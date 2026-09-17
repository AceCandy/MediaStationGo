package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestHongGuoSupplementTaskHTTP(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	tasks := service.NewTaskTrackerService(zap.NewNop(), nil)
	downloads := service.NewHongGuoDownloadService(repo, service.NewHongGuoService(repo, tasks, nil, t.TempDir()), tasks)
	scheduler := service.NewSchedulerService(zap.NewNop(), repo, nil, nil, nil)
	scheduler.SetTaskTracker(tasks)
	scheduler.SetHongGuoDownloads(downloads)
	scheduler.Start(t.Context())
	t.Cleanup(scheduler.Stop)
	svc := &service.Container{Scheduler: scheduler, Tasks: tasks}
	call := func(action, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "key", Value: service.TaskKindHongGuoSupplement}}
		c.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/definitions/hongguo_download_supplement/"+action, strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		if action == "schedule" {
			taskDefinitionScheduleHandler(svc)(c)
		} else {
			taskDefinitionRunHandler(svc)(c)
		}
		return w
	}
	w := call("schedule", `{"enabled":false,"interval_seconds":86400,"count":7}`)
	var definition service.TaskDefinition
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &definition) != nil || definition.ScheduleConfig.Count != 7 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, body := range []string{`{"count":0}`, `{"count":101}`, `{"count":1.5}`, `{}`, strings.Repeat(" ", 4096) + `{"count":1}`} {
		if w := call("run", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{"enabled":true,"interval_seconds":60,"count":0}`, `{"enabled":true,"interval_seconds":60,"count":101}`, `{"enabled":true,"interval_seconds":1,"count":5}`} {
		if w := call("schedule", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := call("run", `{"count":2}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		history, err := tasks.DefinitionHistory(service.TaskKindHongGuoSupplement, 1, 20)
		if err != nil {
			t.Fatal(err)
		}
		if history.Total == 1 && history.Items[0].Status == service.TaskStatusFailed {
			if history.Items[0].Metrics["requested"] != 2 {
				t.Fatal(history.Items)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("missing asynchronous failure record")
		}
		time.Sleep(time.Millisecond)
	}
	definitions, err := tasks.Definitions(scheduler.Status())
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if definition.Key == service.TaskKindHongGuoSupplement && (definition.ScheduleConfig.Count != 7 || definition.ScheduleConfig.Enabled || definition.ScheduleConfig.IntervalSeconds != 86400) {
			t.Fatal("manual/invalid request changed schedule", definition)
		}
	}
}
