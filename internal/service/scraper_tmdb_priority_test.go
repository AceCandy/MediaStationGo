package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTMDbOnlinePriorityMoviesAndSeries(t *testing.T) {
	for _, kind := range []string{model.MetadataKindMovie, model.MetadataKindSeries} {
		t.Run(kind, func(t *testing.T) {
			s, repos, closeServer := newTestScraper(t)
			defer closeServer()
			if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
				t.Fatal(err)
			}
			mediaType, root := "movie", "movie"
			if kind == model.MetadataKindSeries {
				mediaType, root = "tv", "tv"
			}
			requests := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/"+root+"/310708" {
					t.Errorf("unexpected provider path: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				requests++
				fmt.Fprint(w, `{"id":310708,"title":"在线片名","name":"在线片名","overview":"","external_ids":{"tvdb_id":477657,"imdb_id":"tt43250938"}}`)
			}))
			defer upstream.Close()
			s.tmdb.cfg.Secrets.TMDbAPIProxy = upstream.URL
			lib := model.Library{Name: "测试库", Path: t.TempDir(), Type: mediaType, Enabled: true}
			if err := repos.DB.Create(&lib).Error; err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(lib.Path, "在线片名 [tmdbid=310708].mkv")
			nfo := nfoPath(path)
			wantTVDB := "477126" // 电影响应无 TVDB 字段，保留已有值。
			if kind == model.MetadataKindSeries {
				nfo = filepath.Join(lib.Path, "tvshow.nfo")
				wantTVDB = "477657"
			}
			if err := os.WriteFile(nfo, []byte(`<movie><title>本地片名</title><plot>本地简介</plot><tmdbid>310708</tmdbid><tvdbid>477126</tvdbid></movie>`), 0o600); err != nil {
				t.Fatal(err)
			}
			media := model.Media{LibraryID: lib.ID, Path: path, TMDbID: 310708, TheTVDBID: "477126", ScrapeStatus: "pending"}
			if kind == model.MetadataKindSeries {
				media.SeasonNum, media.EpisodeNum = 1, 1
			}
			if err := repos.DB.Create(&media).Error; err != nil {
				t.Fatal(err)
			}
			// 两轮联网更新和一轮复用，旧 NFO 始终留在磁盘。
			for round := 0; round < 3; round++ {
				if err := s.EnrichOneWithOptions(t.Context(), &media, ScrapeOptions{IncludeMatched: round < 2, DeferEpisodeDetails: true}); err != nil {
					t.Fatal(err)
				}
				got, err := repos.Media.FindByID(t.Context(), media.ID)
				if err != nil || got == nil {
					t.Fatalf("media: %v %v", got, err)
				}
				if got.ScrapeStatus != "matched" || got.TMDbID != 310708 || got.TheTVDBID != wantTVDB {
					t.Fatalf("round %d: stale identifiers/status: %+v", round, got)
				}
				item, err := repos.Metadata.FindByIdentifier(t.Context(), "tmdb", kind, "310708")
				if err != nil || item == nil || item.Kind != kind || item.Title != "在线片名" {
					t.Fatalf("metadata: %+v %v", item, err)
				}
				wantOverview := "本地简介"
				if round > 0 {
					wantOverview = "已存简介"
				}
				if item.Overview != wantOverview {
					t.Fatalf("round %d: overview %q, want %q", round, item.Overview, wantOverview)
				}
				if round == 0 {
					if err := repos.DB.Model(item).Update("overview", "已存简介").Error; err != nil {
						t.Fatal(err)
					}
					if err := repos.Metadata.ReplaceIdentifier(t.Context(), item.ID, "imdb", kind, "tt00000001"); err != nil {
						t.Fatal(err)
					}
				} else {
					identified, err := repos.Metadata.FindByIdentifier(t.Context(), "imdb", kind, "tt43250938")
					if err != nil || identified == nil || identified.ID != item.ID {
						t.Fatalf("online IMDb ID not restored: %v %v", identified, err)
					}
				}
				media = *got
			}
			if requests != 2 {
				t.Fatalf("requests = %d, want 2", requests)
			}
		})
	}
}
