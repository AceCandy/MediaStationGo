package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestTMDbRecheckAirdateCooldown(t *testing.T) {
	for _, mode := range []string{"incomplete", "404", "inventory"} {
		for _, tc := range []struct {
			name      string
			age, days int
		}{
			{"recent", 5, 1}, {"middle", 100, 10}, {"old", 500, 20}, {"unknown", 0, 3}, {"preserve-local", 5, 1},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				db := newServiceTestDB(t, &model.Media{}, &model.MetadataProviderSnapshot{}, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{}, &model.TMDbRecheckJob{}, &model.TMDbRecheckSeasonLease{}, &model.TMDbRecheckChange{})
				repos := repository.New(db)
				date := ""
				if tc.age != 0 {
					date = time.Now().UTC().AddDate(0, 0, -tc.age).Format(time.DateOnly)
				}
				series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "series", Title: "测试剧"}, model.MetadataIdentifier{Provider: "tmdb", EntityKind: "series", ExternalID: "42"})
				season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1})
				if mode == "404" {
					if err := db.Model(season).Update("release_date", date).Error; err != nil {
						t.Fatal(err)
					}
				}
				if tc.name == "preserve-local" {
					createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 1, ReleaseDate: date})
					date = ""
				}
				target := season
				if mode == "inventory" {
					target = createServiceTestMetadata(t, db, model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: 2})
				}
				if err := db.Create(&model.Media{MetadataID: target.ID, Path: "/test/timing.mkv"}).Error; err != nil {
					t.Fatal(err)
				}
				past := time.Now().Add(-time.Hour)
				if err := db.Create(&model.TMDbRecheckJob{MetadataID: target.ID, DueAt: &past}).Error; err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if mode == "404" {
						http.NotFound(w, r)
						return
					}
					fmt.Fprintf(w, `{"id":22,"season_number":1,"episodes":[{"id":111,"season_number":1,"episode_number":1,"name":"第一集","overview":"中文简介","air_date":%q}]}`, date)
				}))
				defer server.Close()
				s := &ScraperService{repo: repos, tmdb: newTMDbTestProvider(server.URL), artwork: NewArtworkStore(&config.Config{}, repos.Artwork, nil)}
				job, err := repos.Metadata.ClaimTMDbRecheck(t.Context())
				if err != nil || job == nil {
					t.Fatalf("claim=%+v %v", job, err)
				}
				before := time.Now().UTC()
				details, err := s.processTMDbRecheck(withTMDbRecheckSeason(t.Context()), job, map[string]int64{})
				if err != nil {
					t.Fatal(err)
				}
				var stored model.TMDbRecheckJob
				if err := db.First(&stored, "metadata_id=?", target.ID).Error; err != nil {
					t.Fatal(err)
				}
				wantStatus := "not_found"
				if mode == "incomplete" {
					wantStatus = "pending"
				}
				cooldown := time.Duration(tc.days) * 24 * time.Hour
				if calls.Load() != 1 || stored.Status != wantStatus || stored.DueAt == nil || stored.DueAt.Before(before.Add(cooldown)) || stored.DueAt.After(time.Now().Add(cooldown)) {
					t.Fatalf("calls=%d stored=%+v cooldown=%s", calls.Load(), stored, cooldown)
				}
				if !strings.Contains(strings.Join(details, " "), fmt.Sprintf("%d 天", tc.days)) {
					t.Fatalf("delay missing from logs: %v", details)
				}
				if next, err := repos.Metadata.ClaimTMDbRecheck(t.Context()); err != nil || next != nil {
					t.Fatalf("cooldown bypassed: %+v %v", next, err)
				}
				if mode == "incomplete" {
					// 模拟归并重新唤醒，成功检查点仍阻止手动/普通任务提前请求。
					if err := db.Model(&stored).Update("due_at", past).Error; err != nil {
						t.Fatal(err)
					}
					job, err = repos.Metadata.ClaimTMDbRecheck(t.Context())
					if err != nil || job == nil {
						t.Fatalf("claim=%+v %v", job, err)
					}
					if _, err := s.processTMDbRecheck(withTMDbRecheckSeason(t.Context()), job, map[string]int64{}); err != nil {
						t.Fatal(err)
					}
					if calls.Load() != 1 {
						t.Fatal("checkpoint cooldown bypassed")
					}
				}
			})
		}
	}
}
