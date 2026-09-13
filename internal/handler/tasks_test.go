package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestTasksHandlerReturnsStableDefinitions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	svc := &service.Container{Tasks: service.NewTaskTrackerService(zap.NewNop(), nil)}

	tasksHandler(svc)(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Definitions []service.TaskDefinition `json:"definitions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Definitions) != 19 {
		t.Fatalf("definitions = %d, want 19", len(response.Definitions))
	}
}

func TestTaskDefinitionRunHandlerReportsTMDbSnapshotBackfillUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionTMDbSnapshotBackfill}}

	taskDefinitionRunHandler(&service.Container{})(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerReportsSeriesLocalCorrectionUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionSeriesLocalCorrection}}
	taskDefinitionRunHandler(&service.Container{})(ctx)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestScanTaskDetailsKeepsAllChangesAndLimitsOnlyErrors(t *testing.T) {
	res := &service.ScanResult{
		Reconciled: 2,
		Changes: []service.ScanChange{
			{Action: service.ScanChangeAdded, Path: "/media/a.strm"},
			{Action: service.ScanChangeUpdated, Path: "/media/b.strm", Reason: "mtime_ns 变化"},
		},
		Errors: []string{"first", "second"},
	}
	details := scanTaskDetails(res, 1)
	if len(details) != 4 || details[0] != "🧹 纠正 2 条电影库季集脏数据" || details[1] != "➕ 新增 /media/a.strm" || details[2] != "🔄 更新 /media/b.strm（mtime_ns 变化）" || details[3] != "错误: first" {
		t.Fatalf("details = %#v", details)
	}
	if got := scanTaskMetrics(res)["reconciled"]; got != 2 {
		t.Fatalf("reconciled metric = %d, want 2", got)
	}
}

func TestTaskDefinitionHistoryHandlerRejectsUnknownDefinition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: "unknown"}}
	svc := &service.Container{Tasks: service.NewTaskTrackerService(zap.NewNop(), nil)}

	taskDefinitionHistoryHandler(svc)(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionLogHandlerRejectsUnknownDefinition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/tasks/definitions/unknown/log", nil)
	ctx.Params = gin.Params{{Key: "key", Value: "unknown"}}
	svc := &service.Container{Tasks: service.NewTaskTrackerService(zap.NewNop(), nil)}

	taskDefinitionLogHandler(svc)(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionLogHandlerRejectsInvalidDate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/tasks/definitions/organize/log?date=../../app", nil)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionOrganize}}
	svc := &service.Container{Tasks: service.NewTaskTrackerService(zap.NewNop(), nil)}

	taskDefinitionLogHandler(svc)(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerRejectsTaskWithoutAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionLibraryWatch}}

	taskDefinitionRunHandler(&service.Container{})(ctx)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerRejectsUnknownDefinition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: "unknown"}}

	taskDefinitionRunHandler(&service.Container{})(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerAcceptsScheduledManualTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	scheduler := service.NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	scheduler.Start(t.Context())
	t.Cleanup(scheduler.Stop)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/definitions/people_translation/run", nil)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionPeopleTranslation}}

	taskDefinitionRunHandler(&service.Container{Scheduler: scheduler})(ctx)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerRequiresLibraryScanTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	svc := &service.Container{
		Repo:      repos,
		Scheduler: service.NewSchedulerService(zap.NewNop(), repos, nil, nil, nil),
	}

	for _, body := range []string{"", `{}`, `{invalid`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionLibraryScan}}
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/definitions/library_scan/run", bytes.NewBufferString(body))
		ctx.Request.Header.Set("Content-Type", "application/json")

		taskDefinitionRunHandler(svc)(ctx)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, response = %s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestTaskDefinitionRunHandlerRejectsUnknownLibraryScanTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionLibraryScan}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/definitions/library_scan/run", bytes.NewBufferString(`{"library_id":"missing"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	taskDefinitionRunHandler(&service.Container{
		Repo:      repos,
		Scheduler: service.NewSchedulerService(zap.NewNop(), repos, nil, nil, nil),
	})(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerQueuesSingleAndAllMediaLibraries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, &model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	libraries := []model.Library{
		{Name: "电影", Path: "/media/movie", Type: "movie", Enabled: true},
		{Name: "个人短片", Path: "/media/clips", Type: model.LibraryTypeNFOMovie, Enabled: true},
		{Name: "音乐", Path: "/media/music", Type: "music", Enabled: true},
	}
	if err := db.Create(&libraries).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: libraries[0].ID, Title: "电影", Path: "/media/movie/a.mkv", ScrapeStatus: "error"},
		{LibraryID: libraries[1].ID, Title: "短片", Path: "/media/clips/a.mkv", ScrapeStatus: "no_match"},
		{LibraryID: libraries[2].ID, Title: "歌曲", Path: "/media/music/a.flac", ScrapeStatus: "error"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	scraper := service.NewScraperService(&config.Config{}, log, repos, nil, nil, nil, nil, service.NewHub(log))
	svc := &service.Container{Repo: repos, Scraper: scraper}

	runMediaScrapeAction := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionMediaScrape}}
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/definitions/media_scrape/run", bytes.NewBufferString(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		taskDefinitionRunHandler(svc)(ctx)
		return recorder
	}

	if recorder := runMediaScrapeAction(`{"library_id":"` + libraries[0].ID + `"}`); recorder.Code != http.StatusAccepted {
		t.Fatalf("single status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var stored model.Media
	if err := db.First(&stored, "id = ?", rows[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ScrapeStatus != "no_match" {
		t.Fatalf("single-library action changed another library: %#v", stored)
	}
	if err := db.Model(&model.Media{}).Where("id = ?", rows[0].ID).Update("scrape_status", "error").Error; err != nil {
		t.Fatal(err)
	}

	recorder := runMediaScrapeAction(`{"all_libraries":true}`)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("all status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Count     int64 `json:"count"`
		Libraries int   `json:"libraries"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Count != 2 || response.Libraries != 2 {
		t.Fatalf("all response = %#v", response)
	}
	stored = model.Media{}
	if err := db.First(&stored, "id = ?", rows[2].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ScrapeStatus != "error" {
		t.Fatalf("unsupported music library was queued: %#v", stored)
	}
	if err := db.Model(&model.Media{}).Where("library_id = ?", libraries[0].ID).Update("scrape_status", "matched").Error; err != nil {
		t.Fatal(err)
	}
	recorder = runMediaScrapeAction(`{"library_id":"` + libraries[0].ID + `"}`)
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != http.StatusAccepted || response.Count != 0 {
		t.Fatalf("matched-only library response: %s, err=%v", recorder.Body.String(), err)
	}

	for body, want := range map[string]int{
		`{}`: http.StatusBadRequest,
		`{"library_id":"` + libraries[0].ID + `","all_libraries":true}`: http.StatusBadRequest,
		`{"library_id":"` + libraries[2].ID + `"}`:                      http.StatusBadRequest,
		`{"library_id":"missing"}`:                                      http.StatusNotFound,
	} {
		if recorder := runMediaScrapeAction(body); recorder.Code != want {
			t.Fatalf("body %s: status = %d, want %d; response = %s", body, recorder.Code, want, recorder.Body.String())
		}
	}
}

func TestPeopleBackfillCompatibilityHandlerUsesScheduler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	scheduler := service.NewSchedulerService(zap.NewNop(), nil, nil, nil, nil)
	scheduler.Start(t.Context())
	t.Cleanup(scheduler.Stop)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/people-backfill", nil)

	peopleBackfillHandler(&service.Container{Scheduler: scheduler})(ctx)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionScheduleHandlerUpdatesSchedule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	scheduler := service.NewSchedulerService(zap.NewNop(), repos, nil, nil, nil)
	scheduler.Start(t.Context())
	defer scheduler.Stop()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionLibraryScan}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/tasks/definitions/library_scan/schedule", bytes.NewBufferString(`{"enabled":true,"interval_seconds":7200}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	taskDefinitionScheduleHandler(&service.Container{Scheduler: scheduler, Tasks: service.NewTaskTrackerService(zap.NewNop(), nil)})(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var definition service.TaskDefinition
	if err := json.Unmarshal(recorder.Body.Bytes(), &definition); err != nil {
		t.Fatal(err)
	}
	if definition.ScheduleConfig == nil || !definition.ScheduleConfig.Enabled || definition.ScheduleConfig.IntervalSeconds != 7200 {
		t.Fatalf("definition = %#v", definition)
	}
}

func TestTaskDefinitionScheduleHandlerRejectsUnsupportedDefinition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionLibraryWatch}}

	taskDefinitionScheduleHandler(&service.Container{})(ctx)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionScheduleHandlerRejectsUnknownDefinition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: "unknown"}}

	taskDefinitionScheduleHandler(&service.Container{})(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionScheduleHandlerRejectsInvalidInterval(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	scheduler := service.NewSchedulerService(zap.NewNop(), repository.New(db), nil, nil, nil)
	scheduler.Start(t.Context())
	defer scheduler.Stop()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionLibraryScan}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/tasks/definitions/library_scan/schedule", bytes.NewBufferString(`{"enabled":true,"interval_seconds":59}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	taskDefinitionScheduleHandler(&service.Container{Scheduler: scheduler, Tasks: service.NewTaskTrackerService(zap.NewNop(), nil)})(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerStartsProbeBackfill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionProbeBackfill}}
	tracker := service.NewTaskTrackerService(zap.NewNop(), nil)
	svc := &service.Container{Tasks: tracker, MediaProbe: service.NewMediaProbeService(nil, nil)}

	taskDefinitionRunHandler(svc)(ctx)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if recent := tracker.Snapshot().Recent; len(recent) > 0 {
			if recent[0].Kind != service.TaskKindProbe || recent[0].Status != service.TaskStatusFailed {
				t.Fatalf("task = %#v", recent[0])
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("probe backfill task did not finish")
}

func TestTaskDefinitionRunHandlerRejectsRunningProbeBackfill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionProbeBackfill}}
	tracker := service.NewTaskTrackerService(zap.NewNop(), nil)
	if task := tracker.StartTriggered(service.TaskKindProbe, service.TaskTriggerManual, "媒体轨道回填", service.TaskUpdate{}); task == nil {
		t.Fatal("expected running task")
	}

	taskDefinitionRunHandler(&service.Container{Tasks: tracker, MediaProbe: service.NewMediaProbeService(nil, nil)})(ctx)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerRejectsNegativeProbeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionProbeBackfill}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/definitions/probe_backfill/run", bytes.NewBufferString(`{"limit":-1}`))

	taskDefinitionRunHandler(&service.Container{
		Tasks: service.NewTaskTrackerService(zap.NewNop(), nil), MediaProbe: service.NewMediaProbeService(nil, nil),
	})(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTaskDefinitionRunHandlerRejectsUnknownProbeLibrary(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionProbeBackfill}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/tasks/definitions/probe_backfill/run", bytes.NewBufferString(`{"library_id":"missing"}`))
	repos := repository.New(db)

	taskDefinitionRunHandler(&service.Container{
		Repo: repos, Tasks: service.NewTaskTrackerService(zap.NewNop(), nil), MediaProbe: service.NewMediaProbeService(repos, nil),
	})(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
