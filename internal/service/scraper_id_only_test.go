package service

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func TestAutomaticProviderLookupRequiresExplicitIDs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tmdbID     int
		bangumiID  int
		failTMDb   bool
		notFound   bool
		wrongID    bool
		wantPaths  []string
		wantSource string
		wantError  bool
	}{
		{name: "title alone", wantPaths: []string{}},
		{name: "tmdb ID", tmdbID: 42, wantPaths: []string{"/movie/42"}, wantSource: "tmdb"},
		{name: "failed ID never searches", tmdbID: 42, failTMDb: true, wantPaths: []string{"/movie/42"}, wantError: true},
		{name: "missing ID is no match", tmdbID: 42, notFound: true, wantPaths: []string{"/movie/42"}},
		{name: "different returned ID is rejected", tmdbID: 42, wrongID: true, wantPaths: []string{"/movie/42"}},
		{name: "fallback requires its own ID", tmdbID: 42, bangumiID: 7, failTMDb: true, wantPaths: []string{"/movie/42", "/v0/subjects/7"}, wantSource: "bangumi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Secrets.TMDbAPIKey = "test-key"
			cfg.Secrets.TMDbAPIProxy = "https://tmdb.example.test"
			cfg.Secrets.TheTVDBAPIKey = "test-key"
			log := zap.NewNop()
			paths := []string{}
			client := &http.Client{Transport: imageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.Path)
				body := ""
				switch r.URL.Path {
				case "/movie/42":
					if tc.failTMDb {
						return nil, errors.New("provider unavailable")
					}
					if tc.notFound {
						return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: http.NoBody, Request: r}, nil
					}
					body = `{"id":42,"title":"Known title"}`
					if tc.wrongID {
						body = `{"id":43,"title":"Known title"}`
					}
				case "/v0/subjects/7":
					body = `{"id":7,"name_cn":"Known title"}`
				default:
					t.Errorf("unexpected provider request: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			s := &ScraperService{log: log, tmdb: NewTMDbProvider(cfg, log, nil), bangumi: NewBangumiProvider(cfg, log), douban: NewDoubanProvider(nil), thetvdb: NewTheTVDBProvider(cfg, log)}
			s.tmdb.client, s.bangumi.client, s.thetvdb.client = client, client, client
			s.douban.client, s.douban.directClient, s.douban.resinClient = client, client, client
			media := &model.Media{Title: "Known title", TMDbID: tc.tmdbID, BangumiID: tc.bangumiID}
			got := s.matchFromMediaExternalIDsWithOutcome(t.Context(), media, &model.Library{Type: "movie"})
			if !reflect.DeepEqual(paths, tc.wantPaths) || (got.Err != nil) != tc.wantError {
				t.Fatalf("paths=%v error=%v; want paths=%v error=%v", paths, got.Err, tc.wantPaths, tc.wantError)
			}
			if tc.wantSource == "" {
				if got.Match != nil {
					t.Fatalf("unexpected match: %+v", got.Match)
				}
			} else if got.Match == nil || got.Match.Source != tc.wantSource {
				t.Fatalf("match=%+v, want source %s", got.Match, tc.wantSource)
			}
			if tc.wantSource == "tmdb" {
				paths = nil
				lib := model.Library{Type: "movie", Path: t.TempDir()}
				media.Path = filepath.Join(lib.Path, "Known title.mkv")
				o := &OrganizerService{scraper: s, log: log}
				match := o.lookupReclassifyMetadata(t.Context(), *media, lib, "movie")
				if match == nil || match.TMDbID != 42 || !reflect.DeepEqual(paths, tc.wantPaths) {
					t.Fatalf("reclassify existing ID: match=%+v paths=%v", match, paths)
				}
			}
		})
	}
}

func TestAutomaticScrapeWithoutIDNeverSearches(t *testing.T) {
	s, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.Callback().Create().Remove("testutil:media-metadata"); err != nil {
		t.Fatal(err)
	}
	requests := 0
	s.tmdb.client = &http.Client{Transport: imageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected request")
	})}
	for _, typ := range []string{"movie", "tv", "anime", "variety"} {
		t.Run(typ, func(t *testing.T) {
			lib := model.Library{Name: typ, Path: t.TempDir(), Type: typ, Enabled: true}
			if err := repos.DB.Create(&lib).Error; err != nil {
				t.Fatal(err)
			}
			media := model.Media{LibraryID: lib.ID, Title: "Known title", Path: filepath.Join(lib.Path, "Known title.mkv"), ScrapeStatus: "pending"}
			if err := repos.DB.Create(&media).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.EnrichOne(t.Context(), &media); err != nil {
				t.Fatal(err)
			}
			got, err := repos.Media.FindByID(t.Context(), media.ID)
			if err != nil || got == nil || got.MetadataID != "" || got.ScrapeStatus != "no_match" {
				t.Fatalf("media=%+v error=%v", got, err)
			}
			o := &OrganizerService{scraper: s, log: zap.NewNop(), repo: repos}
			if match := o.lookupOrganizeMetadata(t.Context(), media.Path, lib.Path, typ, media.Title, 2024, 0, 0); match != nil {
				t.Fatalf("organize matched without ID: %+v", match)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("provider requests without ID: %d", requests)
	}
}
