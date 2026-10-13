package handler

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestTMDbCreditCategoryPagination(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	cast := []map[string]any{}
	// 输入倒序，验证来源 order 而非插入顺序或生成 ID 决定演员顺序。
	for i := 44; i >= 0; i-- {
		cast = append(cast, map[string]any{"id": 100 + i, "name": fmt.Sprintf("演员%d", i), "order": i, "credit_id": fmt.Sprint(i)})
	}
	body, _ := json.Marshal(map[string]any{"id": 8, "title": "作品", "credits": map[string]any{"cast": cast, "crew": []map[string]any{
		{"id": 200, "name": "特技", "job": "Stunts", "department": "Crew", "credit_id": "stunt"},
		{"id": 201, "name": "导演", "job": "Director", "department": "Directing", "credit_id": "director"},
	}}})
	if err = repo.TMDb.SaveResponse(t.Context(), repository.TMDbSourceRequest{Kind: "movie", TMDbID: 8, Component: "detail", Language: "zh-CN"}, body, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	subject, err := repo.TMDb.SubjectIdentity(t.Context(), "movie", 8)
	if err != nil {
		t.Fatal(err)
	}
	svc := &service.Container{Repo: repo, TMDbCenter: service.NewTMDbDataCenter(repo, service.NewTMDbProvider(&config.Config{}, zap.NewNop(), nil))}
	router := gin.New()
	router.GET("/subjects/:id/credits", tmdbCenterCredits(svc, false))
	router.GET("/people/:id/works", tmdbCenterCredits(svc, true))
	var firstPerson string
	for page := 1; page <= 4; page++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/subjects/%s/credits?category=cast&page_size=20&page=%d", subject.ID, page), nil))
		var got struct {
			Data     []service.TMDbCreditView `json:"data"`
			Total    int                      `json:"total"`
			PageSize int                      `json:"page_size"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
			t.Fatalf("response %d %s", w.Code, w.Body.String())
		}
		want := 20
		if page == 3 {
			want = 5
		}
		if page == 4 {
			want = 0
		}
		if got.Total != 45 || got.PageSize != 20 || len(got.Data) != want {
			t.Fatalf("page %d: %+v", page, got)
		}
		for i, row := range got.Data {
			if row.Category != "cast" || row.Person.TMDbID != 100+(page-1)*20+i {
				t.Fatalf("order/category: %+v", row)
			}
		}
		if page == 1 {
			firstPerson = got.Data[0].Person.PersonID
		}
	}
	for _, tc := range []struct {
		path        string
		total, size int
		first       string
	}{
		{"/subjects/" + subject.ID + "/credits?category=crew&page_size=20", 2, 20, "crew"},
		{"/subjects/" + subject.ID + "/credits", 47, 60, "cast"},
		{"/people/" + firstPerson + "/works?category=cast&page_size=20", 1, 60, "cast"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		var got struct {
			Data     []service.TMDbCreditView `json:"data"`
			Total    int                      `json:"total"`
			PageSize int                      `json:"page_size"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Total != tc.total || got.PageSize != tc.size || len(got.Data) == 0 || got.Data[0].Category != tc.first {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		if tc.first == "crew" && got.Data[0].Job != "Director" {
			t.Fatal("principal crew should lead")
		}
	}
	for _, query := range []string{"page_size=0", "page_size=61", "page_size=abc", "category=invalid", "page=0"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/subjects/"+subject.ID+"/credits?"+query, nil))
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", query, w.Code)
		}
	}
}
