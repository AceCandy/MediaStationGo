package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func recheckSeasonConcurrentDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := recheckQueueDB(t)
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(2)
	pool.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	first, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.ExecContext(ctx, `SET search_path TO "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestTMDbRecheckSeasonPagesAndRecovery(t *testing.T) {
	db := recheckSeasonConcurrentDB(t)
	if !db.Migrator().HasTable(&model.TMDbRecheckSeasonLease{}) || !db.Migrator().HasIndex(&model.TMDbRecheckJob{}, "idx_tmdb_recheck_lease_token") {
		t.Fatal("season lease migration missing")
	}
	repo := New(db).Metadata
	series := model.MetadataItem{Kind: "series"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season := model.MetadataItem{Kind: "season", ParentID: &series.ID, SeasonNum: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatal(err)
	}
	episodes := make([]model.MetadataItem, 205)
	for i := range episodes {
		episodes[i] = model.MetadataItem{Kind: "episode", ParentID: &season.ID, EpisodeNum: i + 1}
	}
	if err := db.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(-time.Hour)
	jobs := make([]model.TMDbRecheckJob, len(episodes))
	for i := range jobs {
		jobs[i] = model.TMDbRecheckJob{MetadataID: episodes[i].ID, DueAt: &due}
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	cutoff, err := repo.TMDbRecheckPassBoundary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// 两个独占连接同时领取同一季，只能有一个获胜。
	type result struct {
		lease *model.TMDbRecheckSeasonLease
		err   error
	}
	start, ready, results := make(chan struct{}), make(chan struct{}, 2), make(chan result, 2)
	for range 2 {
		go func() {
			err := db.Connection(func(conn *gorm.DB) error {
				ready <- struct{}{}
				<-start
				lease, err := New(conn).Metadata.ClaimTMDbRecheckSeason(t.Context(), cutoff)
				results <- result{lease, err}
				return nil
			})
			if err != nil {
				results <- result{err: err}
			}
		}()
	}
	<-ready
	<-ready
	close(start)
	var lease *model.TMDbRecheckSeasonLease
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.lease != nil {
			if lease != nil {
				t.Fatal("same season claimed twice")
			}
			lease = got.lease
		}
	}
	if lease == nil {
		t.Fatal("season not claimed")
	}
	page, err := repo.ClaimTMDbRecheckSeasonPage(t.Context(), lease, cutoff)
	if err != nil || len(page) != 200 {
		t.Fatalf("page=%d err=%v", len(page), err)
	}
	if err := repo.RenewTMDbRecheckSeason(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	var renewed int64
	if err := db.Model(&model.TMDbRecheckJob{}).Where("lease_token=? AND lease_until>clock_timestamp()+interval '4 minutes'", lease.LeaseToken).Count(&renewed).Error; err != nil || renewed != 200 {
		t.Fatalf("renewed=%d err=%v", renewed, err)
	}
	for i := range page {
		if err := repo.FinishTMDbRecheck(t.Context(), &page[i], "done", "", nil, 0); err != nil {
			t.Fatal(err)
		}
	}
	page, err = repo.ClaimTMDbRecheckSeasonPage(t.Context(), lease, cutoff)
	if err != nil || len(page) != 5 {
		t.Fatalf("second page=%d err=%v", len(page), err)
	}
	// 季租约失效，即使目标租约尚有效，也不允许旧执行者提交或续租。
	if err := db.Model(&model.TMDbRecheckSeasonLease{}).Where("metadata_id=?", season.ID).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	state, err := repo.TMDbRecheckState(t.Context(), page[0].MetadataID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitTMDbRecheck(t.Context(), &page[0], state, func(*Container) error { t.Fatal("expired owner saved"); return nil }); !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatal(err)
	}
	if err := repo.FinishTMDbRecheck(t.Context(), &page[0], "done", "", nil, 0); !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatal(err)
	}
	if err := repo.RenewTMDbRecheckSeason(t.Context(), lease); !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatal(err)
	}
	if err := db.Model(&model.TMDbRecheckJob{}).Where("lease_token=?", lease.LeaseToken).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	newLease, err := repo.ClaimTMDbRecheckSeason(t.Context(), cutoff)
	if err != nil || newLease == nil || newLease.LeaseToken == lease.LeaseToken {
		t.Fatalf("new lease=%+v err=%v", newLease, err)
	}
	recovered, err := repo.ClaimTMDbRecheckSeasonPage(t.Context(), newLease, cutoff)
	if err != nil || len(recovered) != 5 {
		t.Fatalf("recovered=%d err=%v", len(recovered), err)
	}
	if err := repo.ReleaseTMDbRecheckSeason(t.Context(), lease); !errors.Is(err, ErrTMDbRecheckChanged) {
		t.Fatal(err)
	}
	if err := repo.ReleaseTMDbRecheckSeason(t.Context(), newLease); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := db.Model(&model.TMDbRecheckJob{}).Where("status='pending' AND lease_token='' AND attempts=0 AND due_at>?", cutoff).Count(&pending).Error; err != nil || pending != 5 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	if got, err := repo.ClaimTMDbRecheckSeason(t.Context(), cutoff); err != nil || got != nil {
		t.Fatalf("same pass reclaimed: %+v %v", got, err)
	}
}

func TestTMDbRecheckSeasonSkipsLockedAndFutureTargets(t *testing.T) {
	db := recheckSeasonConcurrentDB(t)
	repo := New(db).Metadata
	due := time.Now().Add(-time.Hour)
	job := model.TMDbRecheckJob{MetadataID: "deleted-target", DueAt: &due}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now()
	tx := db.Begin()
	defer tx.Rollback()
	if err := tx.Exec("SELECT 1 FROM tm_db_recheck_jobs WHERE metadata_id=? FOR UPDATE", job.MetadataID).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := repo.ClaimTMDbRecheckSeason(t.Context(), cutoff); err != nil || got != nil {
		t.Fatalf("locked target: %+v %v", got, err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	lease, err := repo.ClaimTMDbRecheckSeason(t.Context(), cutoff)
	if err != nil || lease == nil {
		t.Fatalf("orphan: %+v %v", lease, err)
	}
	page, err := repo.ClaimTMDbRecheckSeasonPage(t.Context(), lease, cutoff)
	if err != nil || len(page) != 1 {
		t.Fatalf("orphan page=%d %v", len(page), err)
	}
	if err := repo.FinishTMDbRecheck(t.Context(), &page[0], "retry", "", ptrRecheckTime(cutoff.Add(time.Microsecond)), 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReleaseTMDbRecheckSeason(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.ClaimTMDbRecheckSeason(t.Context(), cutoff); err != nil || got != nil {
		t.Fatalf("future: %+v %v", got, err)
	}
}
