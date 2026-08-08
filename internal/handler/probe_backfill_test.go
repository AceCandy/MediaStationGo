package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestProbeLibraryRouteRequiresAdminAndReturnsAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	library := model.Library{Name: "Movies", Path: "/media", Type: "movie", Enabled: true}
	if err := db.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Movie", Source: "local"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, MetadataID: metadata.ID, Title: "Movie", Path: "/media/movie.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	probeJSON, err := service.MarshalProbeDocument(&service.ProbeDocument{SchemaVersion: service.ProbeDocumentSchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MediaProbeMetadata{
		MediaID: media.ID, ProbeJSON: probeJSON,
		SchemaVersion: service.ProbeDocumentSchemaVersion, ProbedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	secret := "probe-test-secret"
	repos := repository.New(db)
	svc := &service.Container{
		Log: zap.NewNop(), Repo: repos,
		Tasks:      service.NewTaskTrackerService(zap.NewNop(), nil),
		MediaProbe: service.NewMediaProbeService(repos, nil),
	}
	router := gin.New()
	api := router.Group("/api")
	api.Use(middleware.AuthRequired(secret))
	registerAuthedLibraryRoutes(api, svc)

	tests := []struct {
		name string
		role string
		id   string
		want int
	}{
		{name: "missing token", id: library.ID, want: http.StatusUnauthorized},
		{name: "non admin", role: "user", id: library.ID, want: http.StatusForbidden},
		{name: "missing library", role: "admin", id: "missing", want: http.StatusNotFound},
		{name: "admin", role: "admin", id: library.ID, want: http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+tt.id+"/probe", nil)
			if tt.role != "" {
				req.Header.Set("Authorization", "Bearer "+signedProbeRoleToken(t, secret, tt.role))
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d body=%s, want %d", w.Code, w.Body.String(), tt.want)
			}
			if tt.want == http.StatusAccepted {
				var payload map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || payload["status"] != "started" {
					t.Fatalf("body = %s, err=%v", w.Body.String(), err)
				}
			}
		})
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot := svc.Tasks.Snapshot()
		if len(snapshot.Recent) > 0 {
			task := snapshot.Recent[0]
			if task.Kind != service.TaskKindProbe || task.Status != service.TaskStatusCompleted || task.Metrics["total"] != 1 || task.Metrics["completed"] != 0 || task.Metrics["skipped"] != 1 || task.Metrics["failed"] != 0 {
				t.Fatalf("task = %#v", task)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("probe task did not finish")
}

func signedProbeRoleToken(t *testing.T, secret, role string) string {
	t.Helper()
	claims := middleware.Claims{
		UserID: "user-1", Role: role,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
