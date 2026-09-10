package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	tmdbSnapshotBackfillCompletedSettingKey = "internal.tmdb_snapshot_backfill_completed"
	tmdbSnapshotBackfillPageSize            = 50
)

var (
	ErrTMDbSnapshotBackfillRunning     = errors.New("TMDB snapshot backfill already running")
	ErrTMDbSnapshotBackfillUnavailable = errors.New("TMDB snapshot backfill unavailable")
)

// TMDbSnapshotBackfillResult 汇总一次 TMDB 快照回填的实时进度。
type TMDbSnapshotBackfillResult struct {
	Processed int64
	Total     int64
	Succeeded int64
	Failed    int64
	Remaining int64
}

func (r TMDbSnapshotBackfillResult) Metrics() map[string]int64 {
	return map[string]int64{
		"processed": r.Processed,
		"total":     r.Total,
		"succeeded": r.Succeeded,
		"failed":    r.Failed,
		"remaining": r.Remaining,
	}
}

func (s *ScraperService) persistTMDbSnapshot(ctx context.Context, metadataID string, payload json.RawMessage) bool {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || !json.Valid(payload) {
		return false
	}
	if err := s.repo.Metadata.UpsertProviderSnapshot(ctx, metadataID, "tmdb", payload, time.Now().UTC()); err != nil {
		if s.log != nil {
			s.log.Warn("failed to persist tmdb snapshot", zap.String("metadata_id", metadataID), zap.Error(err))
		}
		return false
	}
	return true
}

// StartTMDbSnapshotBackfill 启动一次全库回填；automatic 仅在完成标记缺失时启动。
func (s *ScraperService) StartTMDbSnapshotBackfill(ctx context.Context, automatic bool) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.repo.Setting == nil || s.tmdb == nil || s.tasks == nil {
		return ErrTMDbSnapshotBackfillUnavailable
	}
	if automatic {
		completed, err := s.repo.Setting.Get(ctx, tmdbSnapshotBackfillCompletedSettingKey)
		if err != nil {
			return err
		}
		if completed == "true" {
			return nil
		}
	}
	if s.tmdb.resolveAPIKey(ctx) == "" {
		return ErrTMDbSnapshotBackfillUnavailable
	}
	trigger := TaskTriggerManual
	if automatic {
		trigger = TaskTriggerEvent
	}
	task := s.tasks.StartTriggeredIfKindIdle(TaskKindTMDbSnapshotBackfill, trigger, "TMDB 快照回填", TaskUpdate{
		Stage: "backfill", Message: "TMDB 快照回填已启动", Metrics: TMDbSnapshotBackfillResult{}.Metrics(),
	})
	if task == nil {
		if s.tasks.IsKindRunning(TaskKindTMDbSnapshotBackfill) {
			return ErrTMDbSnapshotBackfillRunning
		}
		return errors.New("create task execution failed")
	}
	go s.runTMDbSnapshotBackfill(ctx, task, automatic)
	return nil
}

func (s *ScraperService) runTMDbSnapshotBackfill(ctx context.Context, task *TaskHandle, automatic bool) {
	progress := func(result TMDbSnapshotBackfillResult, detail string) {
		update := TaskUpdate{Stage: "backfill", Metrics: result.Metrics()}
		if detail != "" {
			update.Details = []string{detail}
		}
		task.Update(update)
	}
	result, err := s.BackfillTMDbSnapshots(ctx, progress)
	if err == nil && automatic {
		err = s.repo.Setting.Set(ctx, tmdbSnapshotBackfillCompletedSettingKey, "true")
	}
	if err == nil && result.Failed > 0 {
		err = fmt.Errorf("%d TMDB snapshots failed", result.Failed)
	}
	stage, message := "completed", "TMDB 快照回填完成"
	if errors.Is(err, context.Canceled) {
		stage, message = "interrupted", "TMDB 快照回填已中断"
	} else if err != nil {
		stage, message = "failed", "TMDB 快照回填完成，但存在失败"
		if result.Failed == 0 {
			message = "TMDB 快照回填失败"
		}
		err = sanitizeTaskLogError(err)
	}
	task.Finish(err, TaskUpdate{Stage: stage, Message: message, Metrics: result.Metrics()})
}

// BackfillTMDbSnapshots 串行处理全库缺失快照，只写 provider snapshot。
func (s *ScraperService) BackfillTMDbSnapshots(ctx context.Context, progress func(TMDbSnapshotBackfillResult, string)) (TMDbSnapshotBackfillResult, error) {
	ctx = withTMDbSeasonBatch(ctx)
	var result TMDbSnapshotBackfillResult
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.tmdb == nil {
		return result, ErrTMDbSnapshotBackfillUnavailable
	}
	total, err := s.repo.Metadata.CountMissingTMDbSnapshots(ctx)
	if err != nil {
		return result, err
	}
	result.Total, result.Remaining = total, total
	if progress != nil {
		progress(result, "")
	}
	afterID := ""
	for {
		candidates, err := s.repo.Metadata.ListMissingTMDbSnapshotsAfter(ctx, afterID, tmdbSnapshotBackfillPageSize)
		if err != nil {
			return result, err
		}
		if len(candidates) == 0 {
			result.Remaining = 0
			if progress != nil {
				progress(result, "")
			}
			return result, nil
		}
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			afterID = candidate.MetadataID
			result.Processed++
			payload, fetchErr := s.fetchTMDbSnapshot(ctx, candidate)
			if fetchErr == nil {
				fetchErr = s.repo.Metadata.UpsertProviderSnapshot(ctx, candidate.MetadataID, "tmdb", payload, time.Now().UTC())
			}
			detail := fmt.Sprintf("✅ %s %s TMDb %d", candidate.EntityKind, candidate.MetadataID, candidate.TMDbID)
			if fetchErr != nil {
				result.Failed++
				detail = fmt.Sprintf("❌ %s %s TMDb %d %s", candidate.EntityKind, candidate.MetadataID, candidate.TMDbID, sanitizeTaskLogError(fetchErr))
			} else {
				result.Succeeded++
			}
			result.Remaining = max(result.Total-result.Processed, 0)
			if progress != nil {
				progress(result, detail)
			}
		}
	}
}

func (s *ScraperService) fetchTMDbSnapshot(ctx context.Context, candidate repository.TMDbSnapshotBackfillCandidate) (json.RawMessage, error) {
	detailCtx, cancel := context.WithTimeout(ctx, tmdbDetailsTimeout)
	defer cancel()
	var payload json.RawMessage
	switch candidate.EntityKind {
	case model.MetadataKindMovie:
		match, err := s.tmdb.GetMovieMatch(detailCtx, candidate.TMDbID)
		if err != nil {
			return nil, err
		}
		if match == nil || match.TMDbID != candidate.TMDbID {
			return nil, errors.New("TMDB movie details unavailable")
		}
		payload = match.RawJSON
	case model.MetadataKindSeries:
		match, err := s.tmdb.GetTVMatch(detailCtx, candidate.TMDbID)
		if err != nil {
			return nil, err
		}
		if match == nil || match.TMDbID != candidate.TMDbID {
			return nil, errors.New("TMDB series details unavailable")
		}
		payload = match.RawJSON
	case model.MetadataKindSeason:
		if candidate.SeriesTMDbID <= 0 {
			return nil, errors.New("TMDB series identity unavailable")
		}
		details, err := s.tmdb.GetTVSeasonDetails(detailCtx, candidate.SeriesTMDbID, candidate.SeasonNum)
		if err != nil {
			return nil, err
		}
		if details == nil || details.ID != candidate.TMDbID {
			return nil, errors.New("TMDB season details unavailable")
		}
		payload = details.RawJSON
	case model.MetadataKindEpisode:
		if candidate.SeriesTMDbID <= 0 {
			return nil, errors.New("TMDB series identity unavailable")
		}
		details, err := s.tmdb.GetTVEpisodeDetails(detailCtx, candidate.SeriesTMDbID, candidate.SeasonNum, candidate.EpisodeNum)
		if err != nil {
			return nil, err
		}
		if details == nil || details.ID != candidate.TMDbID {
			return nil, errors.New("TMDB episode details unavailable")
		}
		payload = details.RawJSON
	default:
		return nil, errors.New("unsupported metadata kind")
	}
	if !json.Valid(payload) {
		return nil, errors.New("TMDB details returned invalid JSON")
	}
	return payload, nil
}
