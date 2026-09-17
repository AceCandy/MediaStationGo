package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestDiscoverDetailFieldsIdentityAndReadOnly(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	work := &model.MetadataItem{Kind: "movie", Title: "关联作品", Source: "tmdb"}
	if err := repos.Metadata.Create(t.Context(), work, []model.MetadataIdentifier{{Provider: "tmdb", EntityKind: "movie", ExternalID: "124"}, {Provider: "douban", EntityKind: "movie", ExternalID: "456"}}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/movie/999" {
			fmt.Fprint(w, `{"id":998}`)
			return
		}
		if r.URL.Path != "/movie/123" && r.URL.Path != "/tv/123" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"id":123,"title":"电影","name":"整剧","overview":"简介","release_date":"2026-09-15","first_air_date":"2026-09-01","vote_average":8.2,"runtime":122,"episode_run_time":[45,0,50,45],"original_language":"zh","origin_country":["CN"],"production_countries":[{"iso_3166_1":"CN"}],"genres":[{"name":"剧情"}],"credits":{"cast":[{"id":1,"name":"演员","character":"主角"}],"crew":[{"id":2,"name":"导演","job":"Director"}]},"seasons":[{"id":9,"season_number":1}],"private_test_field":"must-not-leak"}`)
	}))
	defer server.Close()
	cfg := &config.Config{Secrets: config.SecretsConfig{TMDbAPIKey: "test-key", TMDbAPIProxy: server.URL}}
	svc := NewMediaService(cfg, zap.NewNop(), repos).SetTMDbProvider(NewTMDbProvider(cfg, zap.NewNop(), nil))
	for _, kind := range []string{"movie", "tv"} {
		detail, err := svc.DiscoverTMDbDetail(t.Context(), repository.DiscoverIdentity{TMDbID: 123, MediaType: kind})
		if err != nil {
			t.Fatal(err)
		}
		want := []int{122}
		if kind == "tv" {
			want = []int{45, 50}
		}
		if !reflect.DeepEqual(detail.RuntimeMinutes, want) || len(detail.Credits) != 2 || detail.Credits[0].Role != "主角" || len(detail.Genres) != 1 || detail.Rating != 8.2 {
			t.Fatalf("bad detail: %#v", detail)
		}
		if detail.DoubanID != "" {
			t.Fatal("metadata-only association leaked")
		}
		data, _ := json.Marshal(detail)
		for _, forbidden := range []string{"seasons", "RawJSON", "private_test_field", "test-key"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("exposed %s", forbidden)
			}
		}
	}
	if requests != 2 {
		t.Fatalf("requests %d", requests)
	}
	if _, err := svc.DiscoverTMDbDetail(t.Context(), repository.DiscoverIdentity{TMDbID: 999, MediaType: "movie"}); err == nil {
		t.Fatal("identity mismatch accepted")
	}
	if _, err := svc.DiscoverTMDbDetail(t.Context(), repository.DiscoverIdentity{TMDbID: 123, MediaType: "episode"}); err == nil {
		t.Fatal("episode accepted")
	}
	var count int64
	if err := db.Model(&model.MetadataItem{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("metadata changed: %d %v", count, err)
	}
}

func TestDiscoverDetailUsesMetadataWithoutMedia(t *testing.T) {
	for _, kind := range []string{"movie", "tv"} {
		t.Run(kind, func(t *testing.T) {
			db := newServiceTestDB(t, &model.Library{}, &model.Media{}, &model.MediaProbeMetadata{}, &model.MetadataProviderSnapshot{}, &model.Person{}, &model.MetadataCredit{})
			repos := repository.New(db)
			id := repository.DiscoverIdentity{TMDbID: 123, MediaType: kind}
			lib := model.Library{Name: "可见库", Path: "/fixture/discover", Type: kind, Enabled: true}
			if err := repos.Library.Create(t.Context(), &lib); err != nil {
				t.Fatal(err)
			}
			work := createServiceTestMetadata(t, db, model.MetadataItem{Kind: id.Kind(), Title: "库内中文标题", Overview: "库内简介", Rating: 7.6, Year: 2026, ReleaseDate: "2026-08-12", Genres: "科幻,悬疑", Languages: "en,de", Countries: "US", RuntimeSec: 5580}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: id.Kind(), ExternalID: "123"}, model.MetadataIdentifier{Provider: "douban", EntityKind: id.Kind(), ExternalID: "456"})
			poster := createServiceTestArtwork(t, db, work.ID, "poster", "local-poster")
			backdrop := createServiceTestArtwork(t, db, work.ID, "backdrop", "local-backdrop")
			person := model.Person{Name: "已翻译演员", OriginalName: "Original Actor", NormalizedName: "original actor", Source: "tmdb"}
			if err := db.Create(&person).Error; err != nil {
				t.Fatal(err)
			}
			for _, credit := range []model.MetadataCredit{
				{MetadataID: work.ID, PersonID: person.ID, Type: model.CreditTypeDirector, SortOrder: 0},
				{MetadataID: work.ID, PersonID: person.ID, Type: model.CreditTypeActor, OriginalRole: "Original Role", Role: "已翻译角色", SortOrder: 1},
			} {
				if err := db.Create(&credit).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := repos.Metadata.UpsertProviderSnapshot(t.Context(), work.ID, "tmdb", []byte(`{"runtime":100,"episode_run_time":[45,50]}`), time.Now()); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":123,"title":"远端电影","name":"远端整剧","runtime":100,"credits":{"cast":[]}}`)
			}))
			defer server.Close()
			cfg := &config.Config{Secrets: config.SecretsConfig{TMDbAPIKey: "test-key", TMDbAPIProxy: server.URL}}
			svc := NewMediaService(cfg, zap.NewNop(), repos).SetTMDbProvider(NewTMDbProvider(cfg, zap.NewNop(), nil))
			detail, err := svc.DiscoverTMDbDetail(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			wantRuntime := []int{93}
			if kind == "tv" {
				wantRuntime = []int{45, 50}
			}
			if requests.Load() != 0 || detail.Title != work.Title || detail.Rating != work.Rating || detail.Overview != work.Overview || detail.PosterURL != poster || detail.BackdropURL != backdrop || detail.DoubanID != "456" || !reflect.DeepEqual(detail.RuntimeMinutes, wantRuntime) {
				t.Fatalf("local projection not used: %+v, requests=%d", detail, requests.Load())
			}
			if len(detail.Credits) != 2 || detail.Credits[0].Name != person.Name || detail.Credits[0].Role != "已翻译角色" || detail.Credits[0].Type != "Actor" {
				t.Fatalf("translated credits/order lost: %+v", detail.Credits)
			}
			// 已入库预览不依赖 TMDb 配置，不为本地缺项联网覆盖。
			svc.SetTMDbProvider(nil)
			if err := db.Model(work).Update("overview", "").Error; err != nil {
				t.Fatal(err)
			}
			local, err := svc.DiscoverTMDbDetail(t.Context(), id)
			if err != nil || local.Overview != "" || requests.Load() != 0 {
				t.Fatalf("local missing field fell back: %+v %v", local, err)
			}
			svc.SetTMDbProvider(NewTMDbProvider(cfg, zap.NewNop(), nil))
			data, _ := json.Marshal(detail)
			for _, secret := range []string{"/fixture/discover/file.strm", "Original Role", "Original Actor", "metadata_id", "test-key"} {
				if strings.Contains(string(data), secret) {
					t.Fatalf("leaked %s", secret)
				}
			}
			var mediaCount int64
			if err := db.Model(&model.Media{}).Count(&mediaCount).Error; err != nil || mediaCount != 0 {
				t.Fatalf("unexpected media records: %d %v", mediaCount, err)
			}
			owned, err := repos.MediaView.FindDiscoverLibraryItems(t.Context(), []repository.DiscoverIdentity{id}, repository.MediaQueryFilter{})
			if err != nil || len(owned) != 0 {
				t.Fatalf("metadata-only title marked as owned: %+v %v", owned, err)
			}
			if requests.Load() != 0 {
				t.Fatal("metadata-only detail requested TMDb")
			}

		})
	}
}
