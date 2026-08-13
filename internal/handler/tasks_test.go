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
	if len(response.Definitions) != 8 {
		t.Fatalf("definitions = %d, want 8", len(response.Definitions))
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
