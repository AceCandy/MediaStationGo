package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestApplyTMDbChineseTitlePrefersMainlandAlternative(t *testing.T) {
	match := &Match{Title: "The Rookie", OriginalName: "The Rookie"}
	applyTMDbChineseTitle(match, []tmdbAlternativeTitle{
		{Country: "TW", Title: "菜鳥新移民"},
		{Country: "CN", Title: "菜鸟老警"},
	}, nil)

	if match.Title != "菜鸟老警" || match.OriginalName != "The Rookie" {
		t.Fatalf("title=%q original=%q, want mainland Chinese title with original preserved", match.Title, match.OriginalName)
	}
}

func TestApplyTMDbChineseTitleFallsBackToSingaporeTranslation(t *testing.T) {
	match := &Match{Title: "English Title", OriginalName: "English Title"}
	translation := tmdbTranslation{Country: "SG", Language: "zh"}
	translation.Data.Name = "新加坡中文名"
	applyTMDbChineseTitle(match, nil, []tmdbTranslation{translation})

	if match.Title != "新加坡中文名" {
		t.Fatalf("title=%q, want Singapore Chinese translation", match.Title)
	}
}

func TestPreferredTMDbEntityTitleSkipsGeneratedEpisodeName(t *testing.T) {
	translation := tmdbTranslation{Country: "US", Language: "en"}
	translation.Data.Name = "Inside S1 E1"

	if got := preferredTMDbEntityTitle("第 1 集", []tmdbTranslation{translation}, model.MetadataKindEpisode, 1); got != "Inside S1 E1" {
		t.Fatalf("episode title = %q, want specific translation", got)
	}
	if got := preferredTMDbEntityTitle("Episode 4", nil, model.MetadataKindEpisode, 4); got != "第 4 集" {
		t.Fatalf("episode title = %q, want generated localized fallback", got)
	}
	if got := preferredTMDbEntityTitle("Season 2", nil, model.MetadataKindSeason, 2); got != "第 2 季" {
		t.Fatalf("season title = %q, want generated localized fallback", got)
	}
}

func TestPreferredTMDbEntityTitlePrefersChineseTranslationOverOriginalFallback(t *testing.T) {
	translation := tmdbTranslation{Country: "CN", Language: "zh"}
	translation.Data.Name = "跨越边界"

	if got := preferredTMDbEntityTitle("The Crossing", []tmdbTranslation{translation}, model.MetadataKindEpisode, 11); got != "跨越边界" {
		t.Fatalf("episode title = %q, want Chinese translation", got)
	}
}

func TestPreferredTMDbEntityTitleRejectsUnrelatedTranslationLanguage(t *testing.T) {
	thai := tmdbTranslation{Country: "TH", Language: "th"}
	thai.Data.Name = "ซีซั่น 1"
	for _, tc := range []struct {
		current, kind, want string
		number              int
	}{
		{"第 1 季", model.MetadataKindSeason, "第 1 季", 1},
		{"", model.MetadataKindSeason, "第 1 季", 1},
		{"Specials", model.MetadataKindSeason, "特别篇", 0},
		{"第 1 集", model.MetadataKindEpisode, "第 1 集", 1},
		{"京都篇", model.MetadataKindSeason, "京都篇", 1},
		{"再会", model.MetadataKindEpisode, "再会", 1},
		{"The Crossing", model.MetadataKindEpisode, "The Crossing", 1},
	} {
		if got := preferredTMDbEntityTitle(tc.current, []tmdbTranslation{thai}, tc.kind, tc.number); got != tc.want {
			t.Errorf("%s %q: got %q, want %q", tc.kind, tc.current, got, tc.want)
		}
	}
	provider := NewTMDbProvider(&config.Config{}, zap.NewNop(), nil)
	details, err := provider.parseTVSeasonDetails([]byte(`{"name":"第 1 季","season_number":1,"translations":{"translations":[{"iso_639_1":"zh","iso_3166_1":"CN","data":{"name":""}},{"iso_639_1":"en","iso_3166_1":"US","data":{"name":""}},{"iso_639_1":"th","iso_3166_1":"TH","data":{"name":"ซีซั่น 1"}}]}}`))
	if err != nil || details.Name != "第 1 季" {
		t.Fatalf("season snapshot = %#v, err = %v", details, err)
	}
}

func TestPreferredTMDbEntityTextFallsBackWithoutParentMetadata(t *testing.T) {
	translation := tmdbTranslation{Country: "US", Language: "en"}
	translation.Data.Overview = "Episode-specific overview"

	if got := preferredTMDbEntityOverview("", []tmdbTranslation{translation}); got != "Episode-specific overview" {
		t.Fatalf("episode overview = %q, want translated own overview", got)
	}
	if got := preferredTMDbEntityTitle("", nil, model.MetadataKindSeason, 0); got != "特别篇" {
		t.Fatalf("empty season title = %q, want generated own fallback", got)
	}
	chinese := tmdbTranslation{Country: "CN", Language: "zh"}
	chinese.Data.Overview = "本集专属简介"
	if got := preferredTMDbEntityOverview("Original overview", []tmdbTranslation{chinese}); got != "本集专属简介" {
		t.Fatalf("episode overview = %q, want Chinese translation", got)
	}
}

func TestGetMovieMatchUsesChineseAlternativeTitle(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/1292695" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("append_to_response"); !strings.Contains(got, "alternative_titles") {
			t.Fatalf("append_to_response=%q, want alternative_titles", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                1292695,
			"title":             "They Will Kill You",
			"original_title":    "They Will Kill You",
			"original_language": "en",
			"release_date":      "2026-03-27",
			"alternative_titles": map[string]any{
				"titles": []map[string]any{{
					"iso_3166_1": "CN",
					"title":      "杀的就是你",
				}},
			},
		})
	}))
	defer upstream.Close()

	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	provider := NewTMDbProvider(cfg, zap.NewNop(), nil)
	match, err := provider.GetMovieMatch(t.Context(), 1292695)
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Title != "杀的就是你" || match.OriginalName != "They Will Kill You" {
		t.Fatalf("match=%#v, want Chinese movie title with English original", match)
	}
}
