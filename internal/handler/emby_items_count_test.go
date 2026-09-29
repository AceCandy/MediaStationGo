package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestEmbyItemsOptionalTotal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateMediaHandlerTestDB(db, model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO libraries(id,name,type,path) VALUES ('movies','Movies','movie','/fixture/movies')`,
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'movie-'||n,'movie','Movie '||n,'local' FROM generate_series(1,3) n`,
		`INSERT INTO media(id,metadata_id,library_id,path) SELECT 'file-'||n,'movie-'||n,'movies','/fixture/movies/'||n FROM generate_series(1,3) n`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.New(db)
	router := gin.New()
	svc := &service.Container{Repo: repo, Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repo)}
	if err := db.Create(&model.User{Base: model.Base{ID: "user-1"}, Username: "viewer", Role: "admin", Tier: "plus", IsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
	registerEmbyRoutes(router, "test-secret", svc)
	token := signedTestToken(t, "test-secret")
	counts := 0
	if err := db.Callback().Row().After("gorm:row").Register("test:optional-total", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "SELECT COUNT(*) FROM (") {
			counts++
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/Items", "/items", "/Users/user-1/Items", "/users/user-1/items", "/emby/Items", "/emby/items", "/emby/Users/user-1/Items", "/emby/users/user-1/items"} {
		for _, tc := range []struct {
			query string
			count bool
		}{
			{"", false}, {"&EnableTotalRecordCount=false", false},
			{"&EnableTotalRecordCount=true", true}, {"&enableTotalRecordCount=TRUE", true},
			{"&enabletotalrecordcount=false", false},
		} {
			for _, start := range []string{"0", "2", "3"} {
				counts = 0
				request := httptest.NewRequest(http.MethodGet, path+"?ParentId=movies&IncludeItemTypes=Movie&SortBy=SortName&Limit=2&StartIndex="+start+tc.query, nil)
				request.Header.Set("X-Emby-Token", token)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				var result struct {
					Items            []struct{ ID string }
					TotalRecordCount *int
					StartIndex       int
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK {
					t.Fatalf("%s: status=%d err=%v body=%s", request.URL, response.Code, err, response.Body.String())
				}
				got := []string{}
				for _, item := range result.Items {
					got = append(got, item.ID)
				}
				want := map[string][]string{"0": {"movie-1", "movie-2"}, "2": {"movie-3"}, "3": {}}[start]
				if !reflect.DeepEqual(got, want) || result.StartIndex != int(start[0]-'0') {
					t.Fatalf("%s: ids=%v start=%d", request.URL, got, result.StartIndex)
				}
				if tc.count {
					if result.TotalRecordCount == nil || *result.TotalRecordCount != 3 || counts != 1 {
						t.Fatalf("%s: total=%v count queries=%d", request.URL, result.TotalRecordCount, counts)
					}
				} else {
					wantTotal := map[string]int{"0": 3, "2": 3, "3": 0}[start]
					if result.TotalRecordCount == nil || *result.TotalRecordCount != wantTotal || counts != 0 {
						t.Fatalf("%s: total hint=%v want=%d queries=%d", request.URL, result.TotalRecordCount, wantTotal, counts)
					}
				}
			}
		}
	}
	// 首轮分别填充两种缓存，次轮都命中；计数模式不能读到无总数页的内部零值。
	svc.Emby.SetRuntimeCache(service.NewRuntimeCacheService(nil, zap.NewNop()))
	for index, enabled := range []string{"false", "true", "false", "true"} {
		counts = 0
		request := httptest.NewRequest(http.MethodGet, "/Items?ParentId=movies&IncludeItemTypes=Movie&EnableTotalRecordCount="+enabled, nil)
		request.Header.Set("X-Emby-Token", token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var result struct {
			Items            []map[string]any
			TotalRecordCount *int
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || len(result.Items) != 3 {
			t.Fatalf("cached response: status=%d err=%v", response.Code, err)
		}
		if result.TotalRecordCount == nil || *result.TotalRecordCount != 3 {
			t.Fatalf("cached count mode %s: total=%v", enabled, result.TotalRecordCount)
		}
		wantCounts := 0
		if index == 1 {
			wantCounts = 1
		}
		if counts != wantCounts {
			t.Fatalf("cache request %d: count queries=%d want=%d", index, counts, wantCounts)
		}
	}
	// 501 条内部前瞻不能被外部 500 条上限误判为无效并重置为 50。
	for _, sql := range []string{
		`INSERT INTO libraries(id,name,type,path) VALUES ('movies-2','Movies 2','movie','/fixture/movies-2')`,
		`INSERT INTO metadata_items(id,kind,title,source) SELECT 'extra-'||n,'movie','Extra '||n,'local' FROM generate_series(1,501) n`,
		`INSERT INTO media(id,metadata_id,library_id,path,season_num,episode_num) SELECT 'extra-file-'||n,'extra-'||n,'movies','/fixture/movies/more/'||n,0,0 FROM generate_series(1,501) n`,
		`INSERT INTO people(id,name,original_name,normalized_name,source) SELECT 'person-'||n,'Person '||n,'Person '||n,'person '||n,'local' FROM generate_series(1,501) n`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query string
		items int
		total int
		start int
	}{
		{"IncludeItemTypes=Movie&ParentId=movies&Limit=500", 500, 501, 0},
		{"IncludeItemTypes=Person&Limit=500", 500, 501, 0},
		{"IncludeItemTypes=Person&Ids=person-1,person-2,person-3&Limit=1", 1, 2, 0},
		{"IncludeItemTypes=Movie&ParentId=movies&Limit=0", 50, 51, 0},
		{"IncludeItemTypes=Movie&ParentId=movies&Limit=501", 50, 51, 0},
		{"IncludeItemTypes=Movie&ParentId=movies&Limit=2&StartIndex=-1", 2, 3, 0},
		{"IncludeItemTypes=Movie&ParentId=movies&Limit=2&StartIndex=503", 1, 504, 503},
		{"IncludeItemTypes=Movie&ParentId=movies&Limit=2&StartIndex=1000", 0, 0, 1000},
		{"Ids=movie-1,movie-2,movie-3&Limit=1&StartIndex=1000", 3, 3, 0},
		{"Limit=1&StartIndex=1000", 2, 2, 0},
		{"IncludeItemTypes=Folder&Limit=1&StartIndex=1000", 2, 2, 0},
		{"IncludeItemTypes=Movie&ParentId=movies&SearchTerm=Extra&Limit=99", 99, 100, 0},
		{"IncludeItemTypes=Movie&ParentId=movies&SearchTerm=Extra&Limit=20&StartIndex=99", 1, 100, 99},
		{"IncludeItemTypes=Movie&ParentId=movies&SearchTerm=Extra&Limit=20&StartIndex=100", 0, 0, 100},
		{"IncludeItemTypes=Movie&ParentId=movies&SearchTerm=Extra&Limit=500", 100, 100, 0},
	} {
		counts = 0
		request := httptest.NewRequest(http.MethodGet, "/Items?Fields=BasicSyncInfo&SortBy=SortName&"+tc.query, nil)
		request.Header.Set("X-Emby-Token", token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var result struct {
			Items            []map[string]any
			TotalRecordCount *int
			StartIndex       int
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK {
			t.Fatalf("%s: status=%d err=%v", tc.query, response.Code, err)
		}
		if len(result.Items) != tc.items || result.TotalRecordCount == nil || *result.TotalRecordCount != tc.total || result.StartIndex != tc.start || counts != 0 {
			t.Fatalf("%s: items=%d total=%v start=%d countSQL=%d", tc.query, len(result.Items), result.TotalRecordCount, result.StartIndex, counts)
		}
	}
}
