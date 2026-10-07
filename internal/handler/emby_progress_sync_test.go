package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestEmbyProgressSnapshotRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	user := model.User{Base: model.Base{ID: "user-1"}, Username: "tester", PasswordHash: "x", Role: "admin", Tier: "plus", IsActive: true}
	if err := repos.User.Create(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO libraries(id,name,path,type,enabled) VALUES ('library','Library','/test','movie',true)`,
		`INSERT INTO metadata_items(id,kind,title,source) VALUES ('item','movie','Item','local'),('other','movie','Other','local')`,
		`INSERT INTO media(id,path,metadata_id,library_id) VALUES ('file','/test/file','item','library'),('wrong','/test/wrong','other','library')`,
		`INSERT INTO media_probe_metadata(media_id,duration_ms,probe_json,schema_version,probed_at) VALUES ('file',120000,'',0,now()),('wrong',120000,'',0,now())`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	registerEmbyRoutes(router, "progress-test-secret", &service.Container{Repo: repos, Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos)})
	token := signedTestToken(t, "progress-test-secret")
	request := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.Header.Set("X-Emby-Token", token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	path := "/Users/user-1/Items/item/PlaybackProgress"
	if w := request("GET", path, "", false); w.Code != 401 {
		t.Fatalf("unauthorized=%d", w.Code)
	}
	for _, prefix := range []string{"", "/emby"} {
		w := request("GET", prefix+path, "", true)
		if w.Code != 200 {
			t.Fatalf("GET %d %s", w.Code, w.Body.String())
		}
		var state service.EmbyProgressSnapshot
		if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.MediaSourceID != "file" || state.RunTimeTicks != 1200000000 || state.Revision != "0" {
			t.Fatalf("initial=%+v", state)
		}
	}
	for _, body := range []string{
		`{}`, `{"MediaSourceId":"file","ExpectedRevision":"0","PositionTicks":0,"RunTimeTicks":1200000000}`,
		`{"MediaSourceId":"file","ExpectedRevision":"0","PositionTicks":-10000,"RunTimeTicks":1200000000,"Played":false}`,
		`{"MediaSourceId":"file","ExpectedRevision":"0","PositionTicks":0,"RunTimeTicks":1000000000,"Played":false}`,
	} {
		if w := request("POST", path, body, true); w.Code != 400 {
			t.Fatalf("invalid=%d %s", w.Code, w.Body.String())
		}
	}
	if w := request("POST", path, `{"MediaSourceId":"wrong","ExpectedRevision":"0","PositionTicks":0,"RunTimeTicks":1200000000,"Played":false}`, true); w.Code != 404 {
		t.Fatalf("wrong source=%d %s", w.Code, w.Body.String())
	}
	w := request("POST", "/emby"+path, `{"MediaSourceId":"file","ExpectedRevision":"0","PositionTicks":10000000,"RunTimeTicks":1200000000,"Played":false}`, true)
	if w.Code != http.StatusOK {
		t.Fatalf("short=%d %s", w.Code, w.Body.String())
	}
	var state service.EmbyProgressSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.PositionTicks != 10000000 || state.Revision == "0" {
		t.Fatalf("short=%+v", state)
	}
	if w := request("POST", path, `{"MediaSourceId":"file","ExpectedRevision":"0","PositionTicks":0,"RunTimeTicks":1200000000,"Played":false}`, true); w.Code != 409 {
		t.Fatalf("stale=%d %s", w.Code, w.Body.String())
	}
	clear := `{"MediaSourceId":"file","ExpectedRevision":"` + state.Revision + `","PositionTicks":0,"RunTimeTicks":1200000000,"Played":false}`
	if w := request("POST", path, clear, true); w.Code != 200 {
		t.Fatalf("clear=%d %s", w.Code, w.Body.String())
	}
	w = request("GET", path, "", true)
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || w.Code != 200 || state.PositionTicks != 0 || state.Played {
		t.Fatalf("readback=%d %+v err=%v", w.Code, state, err)
	}
	var histories, events int64
	db.Model(&model.PlaybackHistory{}).Count(&histories)
	db.Model(&model.PlaybackEvent{}).Count(&events)
	if histories != 1 || events != 0 {
		t.Fatalf("clear deleted history or generated event: %d/%d", histories, events)
	}
	// 切换为普通用户，既有鉴权必须拒绝另一用户的路径。
	if err := db.Model(&model.User{}).Where("id=?", user.ID).Update("role", "user").Error; err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/Users/foreign/Items/item/PlaybackProgress", "", true); w.Code != 403 {
		t.Fatalf("foreign user=%d %s", w.Code, w.Body.String())
	}
}
