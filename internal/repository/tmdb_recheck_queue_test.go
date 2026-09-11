package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func recheckQueueDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := testdb.OpenPostgres(t, &gorm.Config{PrepareStmt: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.MetadataItem{}, &model.MetadataIdentifier{}, &model.MetadataProviderSnapshot{}, &model.Media{}, &model.ArtworkAsset{}, &model.MetadataArtwork{}, &model.TMDbRecheckJob{}, &model.TMDbRecheckChange{}, &model.TMDbRecheckScan{}, &model.TMDbRecheckAssetChange{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = database.EnsureTMDbRecheckTriggers(db); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestTMDbRecheckPendingIndexSkipsProcessedPrefix(t *testing.T) {
	db := recheckQueueDB(t)
	// 模拟旧版本仅按布尔值索引，以及大部分较小 ID 已处理的队列。
	for _, sql := range []string{
		`DROP INDEX idx_tmdb_recheck_changes_pending_id`,
		`CREATE INDEX idx_tmdb_recheck_changes_pending ON tm_db_recheck_changes(pending) WHERE pending`,
		`INSERT INTO tm_db_recheck_changes(metadata_id,pending) SELECT lpad(i::text,8,'0'),i>30000 FROM generate_series(1,40000) i`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := db.AutoMigrate(&model.TMDbRecheckChange{}); err != nil {
			t.Fatal(err)
		}
		if err := database.EnsureTMDbRecheckTriggers(db); err != nil {
			t.Fatal(err)
		}
	}
	if db.Migrator().HasIndex(&model.TMDbRecheckChange{}, "idx_tmdb_recheck_changes_pending") {
		t.Fatal("obsolete pending-only index retained")
	}
	for _, sql := range []string{
		`ANALYZE tm_db_recheck_changes`,
		`SET plan_cache_mode = force_generic_plan`,
		`PREPARE recheck_pending_plan(int) AS SELECT * FROM tm_db_recheck_changes WHERE pending ORDER BY metadata_id LIMIT $1 FOR UPDATE SKIP LOCKED`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	var plan []struct {
		Line string `gorm:"column:QUERY PLAN"`
	}
	if err := db.Raw(`EXPLAIN (ANALYZE, BUFFERS) EXECUTE recheck_pending_plan(1)`).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, row := range plan {
		lines = append(lines, row.Line)
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "idx_tmdb_recheck_changes_pending_id") || strings.Contains(text, "Rows Removed by Filter") || strings.Contains(text, "Sort Key") {
		t.Fatal(text)
	}
	if worked, err := New(db).Metadata.ExpandTMDbRecheckChange(t.Context()); err != nil || !worked {
		t.Fatalf("claim after migration: worked=%v err=%v", worked, err)
	}
	var change model.TMDbRecheckChange
	if err := db.First(&change, "metadata_id = ?", "00030001").Error; err != nil || change.Pending {
		t.Fatalf("first pending change not consumed: %+v err=%v", change, err)
	}
}

func TestTMDbRecheckConcurrentConnections(t *testing.T) {
	db := recheckQueueDB(t)
	series := model.MetadataItem{Kind: "series", Title: "Series", Source: "test"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", Title: "Season", ParentID: &series.ID, Source: "test"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episodes := []model.MetadataItem{
		{Kind: "episode", Title: "One", ParentID: &season.ID, EpisodeNum: 1, Source: "test"},
		{Kind: "episode", Title: "Two", ParentID: &season.ID, EpisodeNum: 2, Source: "test"},
	}
	if err := db.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	for i, episode := range episodes {
		if err := db.Create(&model.Media{MetadataID: episode.ID, Path: fmt.Sprintf("/concurrent-%d.mkv", i)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.TMDbRecheckJob{MetadataID: episode.ID, DueAt: ptrRecheckTime(time.Now().Add(-time.Hour))}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	workers := make([]*gorm.DB, 2)
	for i := range workers {
		conn, err := pool.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `SET search_path TO "`+schema+`"`); err != nil {
			t.Fatal(err)
		}
		workers[i], err = gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := New(workers[1]).Metadata
	// 另一连接持有最早到期任务，领取必须跳过而不是等待或重复领取。
	tx := workers[0].WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("SELECT 1 FROM tm_db_recheck_jobs WHERE metadata_id=? FOR UPDATE", episodes[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimTMDbRecheck(ctx)
	if err != nil || job == nil || job.MetadataID != episodes[1].ID {
		t.Fatalf("skip locked: %+v %v", job, err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	state, err := repo.TMDbRecheckState(ctx, job.MetadataID)
	if err != nil || state == nil {
		t.Fatalf("state=%+v %v", state, err)
	}
	// 同季另一集保存不应使当前集快照失效。
	if err := workers[0].Model(&episodes[0]).Update("overview", "Other episode updated").Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitTMDbRecheck(ctx, job, state, func(*Container) error { return nil }); err != nil {
		t.Fatalf("sibling invalidated snapshot: %v", err)
	}
	// 模拟后代展开先锁父变更行；提交必须退让，不能反向等待形成死锁。
	locked := workers[0].WithContext(ctx).Begin()
	defer locked.Rollback()
	if err := locked.Exec("SELECT 1 FROM tm_db_recheck_changes WHERE metadata_id=? FOR UPDATE", season.ID).Error; err != nil {
		t.Fatal(err)
	}
	err = repo.CommitTMDbRecheck(ctx, job, state, func(*Container) error { t.Fatal("contended callback executed"); return nil })
	if !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatalf("contended commit=%v", err)
	}
	if err := locked.Exec("SELECT tmdb_recheck_mark(?, false)", job.MetadataID).Error; err != nil {
		t.Fatal(err)
	}
	if err := locked.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitTMDbRecheck(ctx, job, state, func(*Container) error { t.Fatal("changed callback executed"); return nil }); !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatal(err)
	}
	identifier := model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "42"}
	if err := workers[0].Create(&identifier).Error; err != nil {
		t.Fatal(err)
	}
	state, err = repo.TMDbRecheckState(ctx, job.MetadataID)
	if err != nil {
		t.Fatal(err)
	}
	busy := workers[0].WithContext(ctx).Begin()
	defer busy.Rollback()
	if err := busy.Exec("SELECT 1 FROM metadata_identifiers WHERE id=? FOR UPDATE", identifier.ID).Error; err != nil {
		t.Fatal(err)
	}
	err = repo.CommitTMDbRecheck(ctx, job, state, func(repos *Container) error {
		return repos.DB.Exec("UPDATE metadata_identifiers SET external_id='43' WHERE id=?", identifier.ID).Error
	})
	if !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatalf("busy business row: %v", err)
	}
	if err := busy.Exec("UPDATE metadata_identifiers SET external_id='44' WHERE id=?", identifier.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := busy.Commit().Error; err != nil {
		t.Fatal(err)
	}
}

func ptrRecheckTime(value time.Time) *time.Time { return &value }

func TestTMDbRecheckScanBatchesStateReadsAndRegistration(t *testing.T) {
	db := recheckQueueDB(t)
	series := model.MetadataItem{Kind: "series", Source: "test"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, Source: "test"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.ArtworkAsset{SHA256: "batch", StorageKey: "batch", MimeType: "image/png"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	episodes := make([]model.MetadataItem, 80)
	for i := range episodes {
		episodes[i] = model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: i + 1, Overview: "Complete", ReleaseDate: "2026-09-08", Source: "test"}
	}
	if err := db.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	files := make([]model.Media, 0, 200)
	for i, episode := range episodes {
		for version := range 2 {
			files = append(files, model.Media{MetadataID: episode.ID, Path: fmt.Sprintf("/batch-%d-%d.mkv", i, version)})
		}
		if i%2 == 0 {
			if err := db.Create(&model.MetadataArtwork{MetadataID: episode.ID, AssetID: asset.ID, ArtworkType: "still"}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range 40 {
		files = append(files, model.Media{Path: fmt.Sprintf("/batch-unlinked-%d.mkv", i)})
	}
	if err := db.CreateInBatches(&files, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE tm_db_recheck_changes SET pending=false, revision=9").Error; err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Microsecond)
	job := model.TMDbRecheckJob{MetadataID: episodes[1].ID, Status: "running", DueAt: &future, LeaseToken: "keep-lease", LeaseUntil: &future}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	reads, writes := 0, 0
	if err := db.Callback().Row().After("gorm:row").Register("test:batch-state", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "AS fingerprint") {
			reads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Raw().After("gorm:raw").Register("test:batch-register", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "INSERT INTO tm_db_recheck_changes") {
			writes++
		}
	}); err != nil {
		t.Fatal(err)
	}
	more, scanned, err := New(db).Metadata.ScanTMDbRecheckFiles(t.Context())
	if err != nil || !more || scanned != 200 || reads != 1 || writes != 1 {
		t.Fatalf("batch: more=%v scanned=%d reads=%d writes=%d err=%v", more, scanned, reads, writes, err)
	}
	var changes []model.TMDbRecheckChange
	if err := db.Where("pending").Find(&changes).Error; err != nil {
		t.Fatal(err)
	}
	if len(changes) != 41 {
		t.Fatalf("want 40 missing stills and season, got %d", len(changes))
	}
	for _, change := range changes {
		if change.Revision != 9 {
			t.Fatalf("revision changed: %+v", change)
		}
	}
	var saved model.TMDbRecheckJob
	if err := db.First(&saved, "metadata_id=?", job.MetadataID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.LeaseToken != job.LeaseToken || !saved.DueAt.Equal(future) || saved.Status != "running" {
		t.Fatalf("lease/cooldown changed: %+v", saved)
	}
}

func TestTMDbRecheckMergeRegistrationRollsBackWithGraph(t *testing.T) {
	db := recheckQueueDB(t)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	series := []model.MetadataItem{{Kind: "series", Title: "Source", Source: "test"}, {Kind: "series", Title: "Target", Source: "test"}}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	seasons := []model.MetadataItem{{Kind: "season", ParentID: &series[0].ID, SeasonNum: 0, Source: "test"}, {Kind: "season", ParentID: &series[1].ID, SeasonNum: 0, Source: "test"}}
	if err := db.Create(&seasons).Error; err != nil {
		t.Fatal(err)
	}
	episodes := []model.MetadataItem{{Kind: "episode", ParentID: &seasons[0].ID, EpisodeNum: 1, Source: "test"}, {Kind: "episode", ParentID: &seasons[1].ID, EpisodeNum: 1, Source: "test"}}
	if err := db.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	files := []model.Media{{MetadataID: episodes[0].ID, Path: "/merge-one.mkv"}, {MetadataID: episodes[0].ID, Path: "/merge-two.mkv"}}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TMDbRecheckJob{MetadataID: episodes[0].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE tm_db_recheck_changes SET pending=false").Error; err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback graph")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := mergeMetadataGraph(tx, series[0].ID, series[1].ID); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.TMDbRecheckChange{}).Where("pending").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback changes=%d %v", count, err)
	}
	if err := db.Model(&model.Media{}).Where("metadata_id=?", episodes[0].ID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("rollback files=%d %v", count, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return mergeMetadataGraph(tx, series[0].ID, series[1].ID) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Media{}).Where("metadata_id=?", episodes[1].ID).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("merged files=%d %v", count, err)
	}
	for i := 0; i < 30; i++ {
		more, err := New(db).Metadata.ExpandTMDbRecheckChange(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if i == 29 {
			t.Fatal("merge changes did not settle")
		}
	}
	if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id=?", episodes[0].ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("deleted source job=%d %v", count, err)
	}
	if err := db.Model(&model.TMDbRecheckJob{}).Where("metadata_id=?", episodes[1].ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("target job=%d %v", count, err)
	}
}

func TestTMDbRecheckScanResumesAcrossEmptyBatch(t *testing.T) {
	db := recheckQueueDB(t)
	rows := make([]model.Media, 201)
	for i := range rows {
		rows[i].Path = fmt.Sprintf("/unlinked-%d.mkv", i)
	}
	if err := db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db).Metadata
	more, scanned, err := repo.ScanTMDbRecheckFiles(t.Context())
	if err != nil || !more || scanned != 200 {
		t.Fatalf("first empty batch: %v %v", more, err)
	}
	var scan model.TMDbRecheckScan
	if err := db.First(&scan, 1).Error; err != nil {
		t.Fatal(err)
	}
	if scan.Cursor == "" {
		t.Fatal("empty candidate batch lost cursor")
	}
	// 新仓库实例沿持久游标继续，完成后延后低频核对。
	more, scanned, err = New(db).Metadata.ScanTMDbRecheckFiles(t.Context())
	if err != nil || more || scanned != 1 {
		t.Fatalf("resume: %v %v", more, err)
	}
	if err := db.First(&scan, 1).Error; err != nil {
		t.Fatal(err)
	}
	if scan.Cursor != "" || scan.NextAt.Before(time.Now().Add(6*24*time.Hour)) {
		t.Fatalf("scan=%+v", scan)
	}
}

func TestTMDbRecheckQueueTransactionsAndClaims(t *testing.T) {
	db := recheckQueueDB(t)
	repo := New(db).Metadata
	series := model.MetadataItem{Kind: "series", Title: "Series", Source: "test"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", Title: "Season", ParentID: &series.ID, SeasonNum: 1, Source: "test"}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episode := model.MetadataItem{Kind: "episode", Title: "Episode", ParentID: &season.ID, EpisodeNum: 1, Source: "test"}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.Media{MetadataID: episode.ID, Path: "/rollback.mkv"}).Error; err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var count int64
	db.Model(&model.TMDbRecheckChange{}).Count(&count)
	if count != 0 {
		t.Fatalf("rolled back changes=%d", count)
	}
	media := model.Media{MetadataID: episode.ID, Path: "/episode.mkv"}
	if err := db.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: "series", ExternalID: "42"}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		more, err := repo.ExpandTMDbRecheckChange(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if i == 19 {
			t.Fatal("expansion did not finish")
		}
	}
	state, err := repo.TMDbRecheckState(t.Context(), episode.ID)
	if err != nil || state == nil || !state.Playable || !state.IdentityValid || state.SeriesTMDbID != "42" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	jobs := map[string]*model.TMDbRecheckJob{}
	for range 2 {
		job, err := repo.ClaimTMDbRecheck(t.Context())
		if err != nil || job == nil {
			t.Fatalf("claim=%+v %v", job, err)
		}
		jobs[job.MetadataID] = job
	}
	if len(jobs) != 2 {
		t.Fatal("duplicate claim")
	}
	if job, err := repo.ClaimTMDbRecheck(t.Context()); err != nil || job != nil {
		t.Fatalf("claimed leased=%+v %v", job, err)
	}
	job := jobs[episode.ID]
	if job == nil {
		t.Fatal("episode missing")
	}
	if err := db.Model(&episode).Update("overview", "manual edit").Error; err != nil {
		t.Fatal(err)
	}
	err = repo.CommitTMDbRecheck(t.Context(), job, state, func(*Container) error { t.Fatal("stale callback executed"); return nil })
	if !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatalf("stale=%v", err)
	}
	state, _ = repo.TMDbRecheckState(t.Context(), episode.ID)
	err = repo.CommitTMDbRecheck(t.Context(), job, state, func(repos *Container) error {
		return repos.Metadata.FinishTMDbRecheck(t.Context(), job, "done", "", nil, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.RenewTMDbRecheck(t.Context(), job); !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatalf("finished lease renewed=%v", err)
	}
	seasonJob := jobs[season.ID]
	if err = db.Model(&model.TMDbRecheckJob{}).Where("metadata_id=?", season.ID).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	reclaimed, err := repo.ClaimTMDbRecheck(t.Context())
	if err != nil || reclaimed == nil || reclaimed.LeaseToken == seasonJob.LeaseToken {
		t.Fatalf("reclaim=%+v %v", reclaimed, err)
	}
	if err = repo.FinishTMDbRecheck(t.Context(), seasonJob, "done", "", nil, 0); !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatalf("old token accepted=%v", err)
	}
	page, err := repo.ListTMDbRechecks(t.Context(), "done", "", 1, 20)
	if err != nil || len(page.Items) != 1 || page.Items[0].SeriesTitle != "Series" {
		t.Fatalf("page=%+v %v", page, err)
	}
	if page, err = repo.ListTMDbRechecks(t.Context(), "done", "Series", 1, 20); err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("search page=%+v %v", page, err)
	}
	if page, err = repo.ListTMDbRechecks(t.Context(), "done", "Series", 2, 1); err != nil || page.Total != 1 || len(page.Items) != 0 {
		t.Fatalf("past-end search page=%+v %v", page, err)
	}
	if page, err = repo.ListTMDbRechecks(t.Context(), "done", "%", 1, 20); err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("literal wildcard search page=%+v %v", page, err)
	}
	if page, err = repo.ListTMDbRechecks(t.Context(), "done", "S01E01", 1, 20); err != nil || len(page.Items) != 1 {
		t.Fatalf("coordinate search page=%+v %v", page, err)
	}
	if page, err = repo.ListTMDbRechecks(t.Context(), "done", "missing", 1, 20); err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("empty search page=%+v %v", page, err)
	}
	if _, err = repo.ListTMDbRechecks(t.Context(), "invalid", "", 1, 20); !errors.Is(err, ErrTMDbRecheckFilter) {
		t.Fatal(err)
	}
}

func TestTMDbRecheckIdleIndexAndBoundedExpansion(t *testing.T) {
	db := recheckQueueDB(t)
	repo := New(db).Metadata
	series := model.MetadataItem{Kind: "series", Title: "Large", Source: "test"}
	db.Create(&series)
	children := make([]model.MetadataItem, 401)
	for i := range children {
		children[i] = model.MetadataItem{Kind: "season", Title: "Season", ParentID: &series.ID, SeasonNum: i, Source: "test"}
	}
	if err := db.CreateInBatches(&children, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("SELECT tmdb_recheck_mark(?,true)", series.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ExpandTMDbRecheckChange(t.Context()); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&model.TMDbRecheckChange{}).Where("metadata_id<>?", series.ID).Count(&n)
	if n != 200 {
		t.Fatalf("expanded=%d", n)
	}
	asset := model.ArtworkAsset{PermanentBase: model.PermanentBase{ID: "ffffffff-ffff-ffff-ffff-ffffffffffff"}, SHA256: "test", StorageKey: "test", MimeType: "image/png"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	links := make([]model.MetadataArtwork, len(children))
	for i, child := range children {
		links[i] = model.MetadataArtwork{MetadataID: child.ID, AssetID: asset.ID, ArtworkType: "poster"}
	}
	if err := db.CreateInBatches(&links, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE tm_db_recheck_changes SET pending=false").Error; err != nil {
		t.Fatal(err)
	}
	for batch, want := range []int64{200, 400, 401} {
		if worked, err := repo.ExpandTMDbRecheckAsset(t.Context()); err != nil || !worked {
			t.Fatalf("asset batch %d: %v %v", batch, worked, err)
		}
		if err := db.Model(&model.TMDbRecheckChange{}).Where("pending").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Fatalf("asset expanded=%d want=%d", n, want)
		}
	}
	if worked, err := repo.ExpandTMDbRecheckAsset(t.Context()); err != nil || worked {
		t.Fatalf("asset did not finish: %v %v", worked, err)
	}
	if err := db.Delete(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if worked, err := repo.ExpandTMDbRecheckAsset(t.Context()); err != nil || !worked {
		t.Fatalf("asset deletion not registered: %v %v", worked, err)
	}
	if err := db.Exec(`INSERT INTO tm_db_recheck_jobs(metadata_id,status,due_at,attempts,last_error,lease_token)
SELECT 'future-'||i, 'pending', clock_timestamp()+interval '10 days',0,'','' FROM generate_series(1,30000) i`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ANALYZE tm_db_recheck_jobs").Error; err != nil {
		t.Fatal(err)
	}
	var plan []struct {
		Line string `gorm:"column:QUERY PLAN"`
	}
	if err := db.Raw(`EXPLAIN (ANALYZE, BUFFERS) SELECT * FROM tm_db_recheck_jobs WHERE due_at <= statement_timestamp() AND (lease_until IS NULL OR lease_until <= statement_timestamp()) ORDER BY due_at,metadata_id LIMIT 1`).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, row := range plan {
		lines = append(lines, row.Line)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Index Cond:") || strings.Contains(strings.Join(lines, "\n"), "Rows Removed by Filter: 30000") {
		t.Fatal(fmt.Sprint(lines))
	}
}
