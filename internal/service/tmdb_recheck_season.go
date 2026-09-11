package service

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// processTMDbRecheckSeason 一次季执行共享上游结果，分批领取、逐目标提交，统一维护在途租约。
func (s *ScraperService) processTMDbRecheckSeason(parent context.Context, lease *model.TMDbRecheckSeasonLease, cutoff time.Time, report func(map[string]int64, []string)) (resultErr error) {
	ctx, cancel := context.WithCancelCause(withTMDbRecheckSeason(parent))
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(repository.TMDbRecheckLease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.repo.Metadata.RenewTMDbRecheckSeason(ctx, lease); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	defer func() {
		cancel(nil)
		<-stopped
		cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer stop()
		if err := s.repo.Metadata.ReleaseTMDbRecheckSeason(cleanup, lease); err != nil && !errors.Is(err, repository.ErrTMDbRecheckChanged) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	for ctx.Err() == nil {
		jobs, err := s.repo.Metadata.ClaimTMDbRecheckSeasonPage(ctx, lease, cutoff)
		if err != nil || len(jobs) == 0 {
			return err
		}
		for i := range jobs {
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			metrics := map[string]int64{"scanned": 1}
			details, err := s.processTMDbRecheck(ctx, &jobs[i], metrics)
			report(metrics, details)
			if err != nil {
				return err
			}
		}
	}
	return context.Cause(ctx)
}
