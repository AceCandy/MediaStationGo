package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
)

func TestActiveMediaScrapesPreservesStatusAndSource(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{})
	scraper := &ScraperService{repo: repository.New(db)}
	for _, tc := range []struct {
		status any
		source string
		want   bool
	}{
		{nil, "", true}, {"", "", true}, {"pending", "", true}, {"running", "", true},
		{"matched", "", false}, {"error", "", false}, {"no_match", "", false},
		{"pending", "nfo", false}, {"running", "hongguo", false},
	} {
		t.Run(fmt.Sprintf("%v/%s", tc.status, tc.source), func(t *testing.T) {
			if err := db.Exec(`INSERT INTO media (id, path, scrape_status, catalog_source)
				VALUES ('candidate', '/candidate.mkv', ?, ?)`, tc.status, tc.source).Error; err != nil {
				t.Fatal(err)
			}
			got, err := scraper.hasActiveMediaScrapes(t.Context())
			if err != nil || got != tc.want {
				t.Fatalf("active = %v, error = %v, want %v", got, err, tc.want)
			}
			if err := db.Exec("DELETE FROM media WHERE id = 'candidate'").Error; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPendingProbeQueryPreservesScope(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.Library{})
	probe := NewMediaProbeService(repository.New(db), nil)
	series := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series"})
	season := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season"})
	episode := createServiceTestMetadata(t, db, model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: 1, Title: "Episode"})
	for _, tc := range []struct {
		name      string
		episode   any
		library   string
		metadata  any
		suffix    string
		document  string
		version   int
		automatic bool
		manual    bool
	}{
		{"missing", 0, "movie", nil, ".mkv", "", 0, true, true},
		{"null_episode", nil, "movie", nil, ".mkv", "", 0, true, true},
		{"empty", 0, "movie", nil, ".mkv", "", ProbeDocumentSchemaVersion, true, true},
		{"outdated", 0, "movie", nil, ".mkv", "{}", ProbeDocumentSchemaVersion + 1, true, true},
		{"current", 0, "movie", nil, ".mkv", "{}", ProbeDocumentSchemaVersion, false, false},
		{"episode", 1, "movie", nil, ".mkv", "", 0, false, true},
		{"negative_episode", -1, "movie", nil, ".mkv", "", 0, false, true},
		{"tv_library", 0, " TV ", nil, ".mkv", "", 0, false, true},
		{"nfo_tv", 0, model.LibraryTypeNFOTV, nil, ".mkv", "", 0, false, true},
		{"series_metadata", 0, "movie", series.ID, ".mkv", "", 0, false, true},
		{"season_metadata", 0, "movie", season.ID, ".mkv", "", 0, false, true},
		{"episode_metadata", 0, "movie", episode.ID, ".mkv", "", 0, false, true},
		{"unknown_library_type", 0, "custom", nil, ".mkv", "", 0, true, true},
		{"iso", 0, "movie", nil, ".ISO", "", 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lib := model.Library{Name: tc.name, Type: tc.library, Path: "/media"}
			if err := db.Create(&lib).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`INSERT INTO media (id, library_id, metadata_id, path, episode_num)
				VALUES ('candidate', ?, ?, ?, ?)`, lib.ID, tc.metadata, "/candidate"+tc.suffix, tc.episode).Error; err != nil {
				t.Fatal(err)
			}
			if tc.version != 0 {
				if err := db.Create(&model.MediaProbeMetadata{MediaID: "candidate", ProbeJSON: tc.document, SchemaVersion: tc.version}).Error; err != nil {
					t.Fatal(err)
				}
			}
			got, err := probe.hasPendingProbe(t.Context())
			if err != nil || got != tc.automatic {
				t.Fatalf("automatic pending = %v, error = %v, want %v", got, err, tc.automatic)
			}
			var count int64
			if err := pendingProbeQuery(db.Table("media AS m"), false).Count(&count).Error; err != nil || (count > 0) != tc.manual {
				t.Fatalf("manual count = %d, error = %v, want pending %v", count, err, tc.manual)
			}
			if err := db.Exec("DELETE FROM media WHERE id = 'candidate'").Error; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBackgroundPendingChecksUseIndexes(t *testing.T) {
	db := newServiceTestDB(t)
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "movie", Type: "movie", Path: "/media"}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, path, episode_num, scrape_status)
		SELECT lpad(n::text, 36, '0'), ?, '/media/' || n || '.mkv',
		CASE WHEN n <= 20000 THEN 1 ELSE 0 END, 'matched'
		FROM generate_series(1, 20500) AS n`, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO media_probe_metadata (media_id, probe_json, schema_version, probed_at)
		SELECT id, repeat('x', 1000), ?, NOW() FROM media`, ProbeDocumentSchemaVersion).Error; err != nil {
		t.Fatal(err)
	}
	// 集号尚未识别的剧集仍由库类型或元数据排除，即使没有探测文档。
	tv := model.Library{Name: "tv", Type: "tv", Path: "/tv"}
	if err := db.Create(&tv).Error; err != nil {
		t.Fatal(err)
	}
	episode := createServiceTestEpisodeMetadata(t, db,
		model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series"},
		model.MetadataItem{Kind: model.MetadataKindEpisode, SeasonNum: 1, EpisodeNum: 1, Title: "Episode"},
	)
	if err := db.Exec(`INSERT INTO media (id, library_id, metadata_id, path, episode_num, scrape_status)
		SELECT 'tv-' || n, ?, NULL, '/tv/' || n || '.mkv', 0, 'matched' FROM generate_series(1, 100) AS n
		UNION ALL SELECT 'episode-' || n, ?, ?, '/episode/' || n || '.mkv', 0, 'matched' FROM generate_series(1, 100) AS n`, tv.ID, lib.ID, episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"media", "media_probe_metadata"} {
		if err := db.Exec("VACUUM (ANALYZE) " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	var statement string
	var args []any
	if err := db.Callback().Row().After("gorm:row").Register("test:pending-query", func(tx *gorm.DB) {
		statement = tx.Statement.SQL.String()
		args = append([]any(nil), tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		check func() (bool, error)
	}{
		{"scrape", func() (bool, error) {
			return (&ScraperService{repo: repository.New(db)}).hasActiveMediaScrapes(t.Context())
		}},
		{"probe", func() (bool, error) {
			return NewMediaProbeService(repository.New(db), nil).hasPendingProbe(t.Context())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pending, err := tc.check()
			if err != nil || pending {
				t.Fatalf("idle pending = %v, error = %v", pending, err)
			}
			// 使用实际执行的查询，同时验证 PostgreSQL 通用预编译计划。
			query, vars := statement, append([]any(nil), args...)
			if err := db.Exec("SET plan_cache_mode = force_generic_plan").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("PREPARE pending_" + tc.name + " AS " + query).Error; err != nil {
				t.Fatal(err)
			}
			execute := "EXECUTE pending_" + tc.name
			if len(vars) > 0 {
				placeholders := make([]string, len(vars))
				for i := range vars {
					placeholders[i] = fmt.Sprintf("$%d", i+1)
				}
				execute = db.Dialector.Explain(execute+"("+strings.Join(placeholders, ",")+")", vars...)
			}
			var raw string
			if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON, TIMING OFF) " + execute).Row().Scan(&raw); err != nil {
				t.Fatal(err)
			}
			type planNode struct {
				Type     string     `json:"Node Type"`
				Relation string     `json:"Relation Name"`
				Rows     float64    `json:"Actual Rows"`
				Removed  float64    `json:"Rows Removed by Filter"`
				Loops    float64    `json:"Actual Loops"`
				Plans    []planNode `json:"Plans"`
			}
			var plans []struct {
				Plan planNode
				MS   float64 `json:"Execution Time"`
			}
			if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
				t.Fatalf("invalid plan: %s, %v", raw, err)
			}
			var walk func(planNode)
			walk = func(node planNode) {
				if node.Relation == "media" && (strings.Contains(node.Type, "Seq Scan") || (node.Rows+node.Removed)*node.Loops > 700) {
					t.Fatalf("idle check scanned unrelated media: %s", raw)
				}
				for _, child := range node.Plans {
					walk(child)
				}
			}
			walk(plans[0].Plan)
			if tc.name == "probe" && (!strings.Contains(raw, "idx_media_probe_automatic_candidates") || !strings.Contains(raw, "idx_media_probe_nonempty_document")) {
				t.Fatalf("probe check missed covering indexes: %s", raw)
			}
			t.Logf("20,700 media, no pending work: %s check %.3f ms", tc.name, plans[0].MS)
		})
	}
}
