package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

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
	if len(response.Definitions) != 7 {
		t.Fatalf("definitions = %d, want 7", len(response.Definitions))
	}
}

func TestScanTaskDetailsKeepsAllChangesAndLimitsOnlyErrors(t *testing.T) {
	res := &service.ScanResult{
		Changes: []service.ScanChange{
			{Action: service.ScanChangeAdded, Path: "/media/a.strm"},
			{Action: service.ScanChangeUpdated, Path: "/media/b.strm", Reason: "mtime_ns 变化"},
		},
		Errors: []string{"first", "second"},
	}
	details := scanTaskDetails(res, 1)
	if len(details) != 3 || details[0] != "➕ 新增 /media/a.strm" || details[1] != "🔄 更新 /media/b.strm（mtime_ns 变化）" || details[2] != "错误: first" {
		t.Fatalf("details = %#v", details)
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
	ctx.Params = gin.Params{{Key: "key", Value: service.TaskDefinitionPeopleTranslation}}

	taskDefinitionRunHandler(&service.Container{})(ctx)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
