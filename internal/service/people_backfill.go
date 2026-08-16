package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type PeopleBackfillResult struct {
	Total     int
	Completed int
	Skipped   int
	Failed    int
	Details   []string
}

func (r PeopleBackfillResult) Metrics() map[string]int64 {
	return map[string]int64{"total": int64(r.Total), "completed": int64(r.Completed), "skipped": int64(r.Skipped), "failed": int64(r.Failed)}
}

type peopleBackfillCandidate struct {
	MetadataID string
	Kind       string
	ExternalID string
}

func (s *ScraperService) StartPeopleBackfillWorker(ctx context.Context) {
	if s == nil {
		return
	}
	s.peopleBackfillOnce.Do(func() {
		s.peopleBackfillWG.Add(1)
		go s.runPeopleBackfillWorker(ctx)
		s.queuePeopleBackfill()
	})
}

func (s *ScraperService) WaitPeopleBackfillWorker() {
	if s != nil {
		s.peopleBackfillWG.Wait()
	}
}

func (s *ScraperService) TriggerPeopleBackfill() {
	if s != nil {
		s.peopleBackfillManual.Store(true)
		s.queuePeopleBackfill()
	}
}

func (s *ScraperService) queuePeopleBackfill() {
	if s == nil {
		return
	}
	select {
	case s.peopleBackfillWake <- struct{}{}:
	default:
	}
}

func (s *ScraperService) runPeopleBackfillWorker(ctx context.Context) {
	defer s.peopleBackfillWG.Done()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.peopleBackfillWake:
		case <-ticker.C:
		}
		trigger := TaskTriggerEvent
		if s.peopleBackfillManual.Swap(false) {
			trigger = TaskTriggerManual
		}
		if err := s.runPeopleBackfillPass(ctx, trigger); err != nil && ctx.Err() == nil && s.log != nil {
			s.log.Warn("people backfill pass failed", zap.Error(err))
		}
	}
}

func (s *ScraperService) runPeopleBackfillPass(ctx context.Context, trigger string) error {
	candidates, err := s.pendingPeopleBackfillCandidates(ctx)
	if trigger != TaskTriggerManual && (err != nil || len(candidates) == 0) {
		return err
	}
	if s.tasks == nil {
		return errors.New("task tracker unavailable")
	}
	result := PeopleBackfillResult{Total: len(candidates)}
	task := s.tasks.StartTriggered(TaskKindPeople, trigger, "人物信息补齐", TaskUpdate{Stage: "people", Message: "人物信息补齐已启动", Metrics: result.Metrics()})
	if task == nil {
		return errors.New("create task execution failed")
	}
	if err != nil {
		task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: "people", Message: "人物信息补齐失败", Metrics: result.Metrics()})
		return err
	}
	if len(candidates) == 0 {
		task.Finish(nil, TaskUpdate{Stage: "completed", Message: "人物信息补齐执行完成，无待补齐人物", Metrics: result.Metrics()})
		return nil
	}
	detailCount := 0
	result, runErr := s.backfillPeopleCandidates(ctx, candidates, func(current PeopleBackfillResult) {
		newDetails := current.Details[detailCount:]
		detailCount = len(current.Details)
		task.Update(TaskUpdate{Stage: "people", Metrics: current.Metrics(), Details: newDetails})
	})
	message := "人物信息补齐完成"
	stage := "completed"
	if runErr != nil {
		message = "人物信息补齐失败"
		stage = "people"
	}
	task.Finish(sanitizeTaskLogError(runErr), TaskUpdate{Stage: stage, Message: message, Metrics: result.Metrics()})
	return runErr
}

func (s *ScraperService) pendingPeopleBackfillCandidates(ctx context.Context) ([]peopleBackfillCandidate, error) {
	if s == nil || s.repo == nil || s.repo.Person == nil || s.tmdb == nil || !s.tmdb.Enabled() {
		return nil, nil
	}
	var candidates []peopleBackfillCandidate
	err := s.repo.DB.WithContext(ctx).Table("metadata_items AS mi").
		Select("DISTINCT mi.id AS metadata_id, mi.kind, mid.external_id").
		Joins("JOIN metadata_identifiers AS mid ON mid.metadata_id = mi.id AND mid.deleted_at IS NULL AND mid.provider = ? AND mid.entity_kind = mi.kind", "tmdb").
		Where("mi.deleted_at IS NULL AND mi.kind IN ?", []string{model.MetadataKindMovie, model.MetadataKindSeries}).
		Where("mi.source = ?", "tmdb").
		Where("mi.people_hydrated_at IS NULL").
		Where("NOT EXISTS (SELECT 1 FROM metadata_credits mc WHERE mc.metadata_id = mi.id AND mc.deleted_at IS NULL)").
		Order("mi.kind, mi.id").Scan(&candidates).Error
	return candidates, err
}

func (s *ScraperService) backfillPeopleCandidates(ctx context.Context, candidates []peopleBackfillCandidate, progress func(PeopleBackfillResult)) (PeopleBackfillResult, error) {
	result := PeopleBackfillResult{Total: len(candidates)}
	if progress != nil {
		progress(result)
	}
	for i, candidate := range candidates {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		tmdbID, parseErr := strconv.Atoi(strings.TrimSpace(candidate.ExternalID))
		if parseErr != nil || tmdbID <= 0 {
			result.Skipped++
			result.Details = append(result.Details, fmt.Sprintf("%s %s: 跳过无效 TMDB ID %q", candidate.Kind, candidate.MetadataID, candidate.ExternalID))
		} else {
			wakeScrapeWorker := false
			func() {
				s.scrapeRunMu.Lock()
				defer s.scrapeRunMu.Unlock()
				mediaType := "movie"
				if candidate.Kind == model.MetadataKindSeries {
					mediaType = "tv"
				}
				credits, loaded, fetchErr := s.tmdb.GetCredits(ctx, tmdbID, mediaType)
				if fetchErr != nil {
					result.Failed++
					if !isTMDbHTTPStatus(fetchErr, http.StatusNotFound) {
						result.Details = append(result.Details, candidate.MetadataID+": "+sanitizeTaskLogError(fetchErr).Error())
						return
					}
					reset, invalidateErr := s.repo.Metadata.InvalidateTMDbIdentifier(ctx, candidate.MetadataID, candidate.Kind, candidate.ExternalID)
					if invalidateErr != nil {
						combinedErr := fmt.Errorf("%w; invalidate TMDB identifier: %v", fetchErr, invalidateErr)
						result.Details = append(result.Details, candidate.MetadataID+": "+sanitizeTaskLogError(combinedErr).Error())
						return
					}
					wakeScrapeWorker = reset > 0
					result.Details = append(result.Details, fmt.Sprintf("%s: TMDB 标识 %s 已失效，已重置 %d 个媒体", candidate.MetadataID, candidate.ExternalID, reset))
				} else if persistErr := s.persistCredits(ctx, candidate.MetadataID, loaded, credits); persistErr != nil {
					result.Failed++
					result.Details = append(result.Details, candidate.MetadataID+": "+sanitizeTaskLogError(persistErr).Error())
				} else {
					result.Completed++
					result.Details = append(result.Details, fmt.Sprintf("%s %s: TMDB %s 人物信息补齐成功", candidate.Kind, candidate.MetadataID, candidate.ExternalID))
				}
			}()
			if wakeScrapeWorker {
				s.WakeScrapeWorker()
			}
		}
		if progress != nil {
			progress(result)
		}
		if i < len(candidates)-1 {
			if delay := s.scrapeDelay(ctx); delay > 0 {
				select {
				case <-ctx.Done():
					return result, ctx.Err()
				case <-time.After(delay):
				}
			}
		}
	}
	return result, nil
}

// BackfillLibraryPeople 保留旧调用兼容，实际按全局缺失对象补齐。
func (s *ScraperService) BackfillLibraryPeople(ctx context.Context, _ string, progress func(PeopleBackfillResult)) (PeopleBackfillResult, error) {
	candidates, err := s.pendingPeopleBackfillCandidates(ctx)
	if err != nil {
		return PeopleBackfillResult{}, err
	}
	return s.backfillPeopleCandidates(ctx, candidates, progress)
}
