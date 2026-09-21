package service

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestClaimPendingGroupUsesBoundedReadsAndIndexes(t *testing.T) {
	db := newServiceTestDB(t)
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "queue", Path: "/media/queue", Type: "tv", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, path, series_hint, season_num, episode_num, scrape_status)
		SELECT lpad(n::text,36,'0'), ?, '/media/queue/' || n || '.mkv', 'series-' || ((n-1)/2), 1, n % 2 + 1, 'pending'
		FROM generate_series(1,10000) AS n`, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	movie := model.Media{PermanentBase: model.PermanentBase{ID: "movie"}, LibraryID: lib.ID, Path: "/media/queue/movie.mkv", ScrapeStatus: "pending"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ANALYZE media").Error; err != nil {
		t.Fatal(err)
	}
	var seedSQL string
	var seedVars []any
	var groupSQL string
	var groupVars []any
	if err := db.Callback().Query().After("gorm:query").Register("test:bounded-claim", func(tx *gorm.DB) {
		if tx.Statement.Table != "media" {
			return
		}
		if tx.RowsAffected > 2 {
			t.Errorf("claim loaded %d rows instead of one group", tx.RowsAffected)
		}
		if strings.Contains(tx.Statement.SQL.String(), "LIMIT") {
			seedSQL = tx.Statement.SQL.String()
			seedVars = append([]any(nil), tx.Statement.Vars...)
		} else {
			groupSQL = tx.Statement.SQL.String()
			groupVars = append([]any(nil), tx.Statement.Vars...)
		}
	}); err != nil {
		t.Fatal(err)
	}
	scraper := &ScraperService{repo: repository.New(db)}
	for i := range 3 {
		group, err := scraper.claimNextPendingMediaGroup(t.Context())
		if err != nil || group == nil {
			t.Fatalf("claim %d: %+v %v", i, group, err)
		}
		if i == 0 && (group.Representative.ID != movie.ID || len(group.MediaIDs) != 1) || i > 0 && len(group.MediaIDs) != 2 {
			t.Fatalf("priority/group changed: %+v", group)
		}
	}
	if !strings.Contains(seedSQL, "LIMIT") {
		t.Fatalf("unbounded seed query: %s", seedSQL)
	}
	var plan string
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+seedSQL, seedVars...).Row().Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "idx_media_scrape_pending_pick") || !strings.Contains(plan, "idx_media_scrape_running") || strings.Contains(plan, `"Node Type": "Seq Scan"`) {
		t.Fatalf("queue claim did not use bounded index reads: %s", plan)
	}
	if err := db.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+groupSQL, groupVars...).Row().Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "idx_media_scrape_group") || strings.Contains(plan, `"Node Type": "Seq Scan"`) {
		t.Fatalf("group lookup scanned the queue: %s", plan)
	}
	t.Log("10,000 pending files: each claim loads at most two files; seed and running checks use partial indexes")
}

func TestClaimPendingGroupNullWhitespaceAndSourceIsolation(t *testing.T) {
	db := newServiceTestDB(t)
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Path: "/media", Type: "tv", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Shared"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, path, series_hint, metadata_id, scrape_status, catalog_source) VALUES
		('a', ?, '/a.mkv', ' Show ', NULL, NULL, ''),
		('b', ?, '/b.mkv', 'Show', NULL, '', ''),
		('c', ?, '/c.mkv', 'Show', NULL, 'running', 'nfo'),
		('d', ?, '/d.mkv', NULL, ?, 'pending', ''),
		('e', ?, '/e.mkv', ' ', ?, 'pending', ''),
		('f', ?, '/f.mkv', NULL, ?, 'running', ''),
		('g', ?, '/g.mkv', 'Show', NULL, 'pending', 'hongguo'),
		('h', ?, '/h.mkv', 'Show', NULL, 'pending', 'nfo')`,
		lib.ID, lib.ID, lib.ID, lib.ID, metadata.ID, lib.ID, metadata.ID, lib.ID, metadata.ID, lib.ID, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	scraper := &ScraperService{repo: repository.New(db)}
	group, err := scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil || group == nil || strings.Join(group.MediaIDs, ",") != "a,b" {
		t.Fatalf("trimmed complete series/source isolation: %+v %v", group, err)
	}
	seriesGroup := *group
	group, err = scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil || group != nil {
		t.Fatalf("NULL series bypassed running metadata group: %+v %v", group, err)
	}
	if err := db.Exec("UPDATE media SET scrape_status = 'matched' WHERE id = 'f'").Error; err != nil {
		t.Fatal(err)
	}
	group, err = scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil || group == nil || strings.Join(group.MediaIDs, ",") != "d,e" {
		t.Fatalf("NULL/blank metadata grouping: %+v %v", group, err)
	}
	for _, claimed := range []scrapeCandidateGroup{seriesGroup, *group} {
		if err := scraper.resetScrapeGroupPending(t.Context(), claimed); err != nil {
			t.Fatal(err)
		}
	}
	var pending int64
	if err := db.Model(&model.Media{}).Where("id IN ('a', 'b', 'd', 'e') AND scrape_status = 'pending'").Count(&pending).Error; err != nil || pending != 4 {
		t.Fatalf("cancel left claimed rows running: count=%d err=%v", pending, err)
	}
	var foreignRunning int64
	if err := db.Model(&model.Media{}).Where("id = 'c' AND scrape_status = 'running'").Count(&foreignRunning).Error; err != nil || foreignRunning != 1 {
		t.Fatalf("cancel reset another catalog: count=%d err=%v", foreignRunning, err)
	}
}

func TestClaimPendingGroupIndependentConnections(t *testing.T) {
	db := newServiceTestDB(t)
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Path: "/media", Type: "tv", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, path, series_hint, scrape_status) VALUES
		('a', ?, '/a.mkv', 'series', 'pending'), ('b', ?, '/b.mkv', 'series', 'pending')`, lib.ID, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	connections := []*gorm.DB{db}
	for range 2 {
		connections = append(connections, newClaimTestConnection(t, db))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var ready atomic.Int32
	release := make(chan struct{})
	type result struct {
		group *scrapeCandidateGroup
		err   error
	}
	results := make(chan result, len(connections))
	for _, conn := range connections {
		// 三个事务都读完同组后才允许 UPDATE，确定性覆盖整组领取冲突。
		if err := conn.Callback().Query().After("gorm:query").Register("test:claim-race", func(tx *gorm.DB) {
			if tx.Statement.Table == "media" && !strings.Contains(tx.Statement.SQL.String(), "LIMIT") {
				if ready.Add(1) == int32(len(connections)) {
					close(release)
				}
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		go func() {
			scraper := &ScraperService{repo: repository.New(conn)}
			group, err := scraper.claimNextPendingMediaGroup(ctx)
			results <- result{group, err}
		}()
	}
	claimed := 0
	for range connections {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.group != nil {
			claimed++
			if len(result.group.MediaIDs) != 2 {
				t.Fatalf("partial claim: %+v", result.group)
			}
		}
	}
	if claimed != 1 {
		t.Fatalf("successful concurrent claims = %d, want 1", claimed)
	}
}

func newClaimTestConnection(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	other, err := gorm.Open(postgres.Open(os.Getenv("MEDIASTATION_TEST_POSTGRES_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := other.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := other.Exec("SELECT set_config('search_path', ?, false)", schema).Error; err != nil {
		t.Fatal(err)
	}
	return other
}

func TestClaimPendingGroupRechecksRunningBetweenReads(t *testing.T) {
	db := newServiceTestDB(t)
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Path: "/media", Type: "tv", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, path, series_hint, scrape_status) VALUES
		('a', ?, '/a.mkv', 'series', 'pending')`, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	other := newClaimTestConnection(t, db)
	if err := db.Callback().Query().After("gorm:query").Register("test:late-episode", func(tx *gorm.DB) {
		if tx.Statement.Table != "media" || !strings.Contains(tx.Statement.SQL.String(), "LIMIT") {
			return
		}
		scraper := &ScraperService{repo: repository.New(other)}
		group, err := scraper.claimNextPendingMediaGroup(t.Context())
		if err != nil || group == nil {
			t.Fatalf("competing claim: %+v %v", group, err)
		}
		if err := other.Exec(`INSERT INTO media (id, library_id, path, series_hint, scrape_status) VALUES
			('b', ?, '/b.mkv', 'series', 'pending')`, lib.ID).Error; err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	scraper := &ScraperService{repo: repository.New(db)}
	group, err := scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil || group != nil {
		t.Fatalf("late episode claimed while sibling is running: %+v %v", group, err)
	}
}

func TestClaimPendingGroupRollsBackSourceChange(t *testing.T) {
	db := newServiceTestDB(t)
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Path: "/media", Type: "tv", Enabled: true}
	if err := db.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO media (id, library_id, path, series_hint, scrape_status) VALUES
		('a', ?, '/a.mkv', 'series', 'pending'), ('b', ?, '/b.mkv', 'series', 'pending')`, lib.ID, lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	other := newClaimTestConnection(t, db)
	if err := db.Callback().Query().After("gorm:query").Register("test:source-change", func(tx *gorm.DB) {
		if tx.Statement.Table == "media" && !strings.Contains(tx.Statement.SQL.String(), "LIMIT") {
			if err := other.Exec("UPDATE media SET catalog_source = 'nfo' WHERE id = 'b'").Error; err != nil {
				t.Fatal(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	scraper := &ScraperService{repo: repository.New(db)}
	group, err := scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil || group != nil {
		t.Fatalf("changed-source group claimed: %+v %v", group, err)
	}
	var pending int64
	if err := other.Model(&model.Media{}).Where("scrape_status = 'pending'").Count(&pending).Error; err != nil || pending != 2 {
		t.Fatalf("partial claim was not rolled back: count=%d err=%v", pending, err)
	}
}
