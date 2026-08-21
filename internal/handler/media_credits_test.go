package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	testdb "github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestListMediaCreditsReturnsOrderedCast(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	library := model.Library{Name: "Movies", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Credits Movie", Source: "local"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, MetadataID: metadata.ID, Title: "Credits Movie", Path: "credits-movie.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	director := model.Person{Name: "导演甲", OriginalName: "导演甲", NormalizedName: "导演甲", Source: "tmdb", ProfileURL: "https://images.example/director.jpg"}
	actor := model.Person{Name: "演员乙", OriginalName: "演员乙", NormalizedName: "演员乙", Source: "tmdb", ProfileURL: "https://images.example/actor.jpg"}
	for _, p := range []*model.Person{&director, &actor} {
		if err := db.Create(p).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 先写导演再写演员，验证响应按类型排序后演员在前。
	if err := db.Create(&model.MetadataCredit{MetadataID: metadata.ID, PersonID: director.ID, Type: model.CreditTypeDirector, Role: "导演", SortOrder: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataCredit{MetadataID: metadata.ID, PersonID: actor.ID, Type: model.CreditTypeActor, Role: "主角", SortOrder: 1}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, "user-1")
	c.Set(middleware.CtxUserRole, "admin")
	c.Params = gin.Params{{Key: "id", Value: media.ID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/media/"+media.ID+"/credits", nil)
	listMediaCreditsHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET media credits status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []mediaCredit `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode credits: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("credits len = %d, want 2 (%#v)", len(resp.Items), resp.Items)
	}
	if resp.Items[0].Name != "演员乙" || resp.Items[0].Role != "主角" || resp.Items[0].ProfileURL == "" {
		t.Fatalf("first credit = %#v, want actor first with role and profile", resp.Items[0])
	}
	if resp.Items[1].Type != model.CreditTypeDirector {
		t.Fatalf("second credit = %#v, want director", resp.Items[1])
	}
}

func TestListMediaCreditsEmptyWhenNoMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	library := model.Library{Name: "Movies", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &library); err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, Title: "No Metadata", Path: "no-metadata.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{
		Repo:  repos,
		Media: service.NewMediaService(&config.Config{}, zap.NewNop(), repos),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(middleware.CtxUserID, "user-1")
	c.Set(middleware.CtxUserRole, "admin")
	c.Params = gin.Params{{Key: "id", Value: media.ID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/media/"+media.ID+"/credits", nil)
	listMediaCreditsHandler(svc)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET media credits status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []mediaCredit `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode credits: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("credits len = %d, want 0", len(resp.Items))
	}
}
