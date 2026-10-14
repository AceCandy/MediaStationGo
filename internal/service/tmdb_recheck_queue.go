package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const tmdbRecheckDetailsTimeout = 30 * time.Second

func (s *ScraperService) processTMDbRecheck(parent context.Context, job *model.TMDbRecheckJob, metrics map[string]int64) ([]string, error) {
	ctx := parent
	stageStarted := time.Now()
	state, err := s.repo.Metadata.TMDbRecheckState(ctx, job.MetadataID)
	err = recordTMDbRecheckStage(metrics, "save", "数据库状态核对", stageStarted, err)
	if err != nil {
		return s.retryTMDbRecheck(parent, job, metrics, err)
	}
	if state != nil && job.NotFoundIdentity != "" && job.NotFoundIdentity != state.RequestIdentity() {
		job.NotFoundIdentity = ""
	}
	finish := func(status, reason string, due *time.Time) error {
		started := time.Now()
		err := s.repo.Metadata.CommitTMDbRecheck(ctx, job, state, func(repos *repository.Container) error {
			return repos.Metadata.FinishTMDbRecheck(ctx, job, status, reason, due, 0)
		})
		return recordTMDbRecheckStage(metrics, "save", "数据库状态保存", started, err)
	}
	identityValid := state != nil && state.IdentityValid
	now := time.Now().UTC()
	var cooldown time.Duration
	if state != nil {
		cooldown = state.Cooldown(now)
	}
	if identityValid {
		id, parseErr := strconv.Atoi(state.SeriesTMDbID)
		identityValid = parseErr == nil && id > 0
	}
	if state == nil || !state.Playable || (!tmdbMetadataCandidateNeedsRecheck(state.TMDbMetadataRecheckCandidate) && job.NotFoundIdentity == "") {
		err = finish("done", "", nil)
	} else if !identityValid {
		next := time.Now().UTC().Add(7 * 24 * time.Hour)
		err = finish("blocked", "TMDb 标识缺失或不唯一", &next)
	} else if state.CheckedAt != nil && state.CheckedAt.Add(cooldown).After(now) {
		next := state.CheckedAt.Add(cooldown)
		if job.NotFoundIdentity != "" {
			err = finish("not_found", job.LastError, &next)
		} else {
			err = finish("pending", fmt.Sprintf("成功检查后的 %d 天冷却", int(cooldown/(24*time.Hour))), &next)
		}
	} else {
		return s.fetchAndCommitTMDbRecheck(ctx, parent, job, state, metrics)
	}
	if err != nil {
		return s.retryTMDbRecheck(parent, job, metrics, err)
	}
	return nil, nil
}

func (s *ScraperService) fetchAndCommitTMDbRecheck(ctx, parent context.Context, job *model.TMDbRecheckJob, state *repository.TMDbRecheckState, metrics map[string]int64) (details []string, resultErr error) {
	defer func() {
		for i := range details {
			details[i] = strings.ReplaceAll(details[i], job.MetadataID, tmdbRecheckSubject(state))
		}
	}()
	seriesID, err := strconv.Atoi(state.SeriesTMDbID)
	if err != nil || seriesID <= 0 {
		return s.retryTMDbRecheck(parent, job, metrics, errors.New("TMDb 整剧标识无效"))
	}
	requestCtx, cancel := context.WithTimeout(ctx, tmdbRecheckDetailsTimeout)
	stageStarted := time.Now()
	data, err := s.fetchTMDbMetadataRecheck(requestCtx, state.TMDbMetadataRecheckCandidate, seriesID)
	cancel()
	metrics["requests"]++
	timing := state.TMDbRecheckTiming
	if data != nil && data.timing != nil {
		if data.timing.SeasonReleaseDate != "" {
			timing.SeasonReleaseDate = data.timing.SeasonReleaseDate
		}
		var localDates, freshDates []string
		_ = json.Unmarshal([]byte(timing.SeasonEpisodeDates), &localDates)
		_ = json.Unmarshal([]byte(data.timing.SeasonEpisodeDates), &freshDates)
		dates, _ := json.Marshal(append(localDates, freshDates...))
		timing.SeasonEpisodeDates = string(dates)
	}
	if isTMDbHTTPStatus(err, http.StatusNotFound) || errors.Is(err, errTMDbEpisodeMissingFromSeason) {
		recordTMDbRecheckStage(metrics, "details", "详情请求", stageStarted, nil)
		now := time.Now().UTC()
		cooldown := timing.Cooldown(now)
		next := now.Add(cooldown)
		job.NotFoundIdentity = state.RequestIdentity()
		reason := fmt.Sprintf("TMDb 上游未找到该季/集（404），%d 天后复核；请核对剧集匹配及编号，勿仅凭 404 删除文件", int(cooldown/(24*time.Hour)))
		if errors.Is(err, errTMDbEpisodeMissingFromSeason) {
			reason = fmt.Sprintf("TMDb 整季清单未包含该集，%d 天后复核；请核对匹配及编号，勿据此删除文件", int(cooldown/(24*time.Hour)))
		}
		stageStarted = time.Now()
		err = s.repo.Metadata.CommitTMDbRecheck(ctx, job, state, func(repos *repository.Container) error {
			return repos.Metadata.FinishTMDbRecheck(ctx, job, "not_found", reason, &next, job.Attempts+1)
		})
		err = recordTMDbRecheckStage(metrics, "save", "数据库保存", stageStarted, err)
		if err != nil {
			return s.retryTMDbRecheck(parent, job, metrics, err)
		}
		metrics["not_found"]++
		return []string{"⚠️ " + job.MetadataID + "，" + reason}, nil
	}
	if err == nil && (data == nil || data.id <= 0 || !json.Valid(data.rawJSON)) {
		err = errors.New("TMDb 返回数据无效")
	}
	err = recordTMDbRecheckStage(metrics, "details", "详情请求", stageStarted, err)
	if err != nil {
		return s.retryTMDbRecheck(parent, job, metrics, err)
	}
	return s.commitTMDbSourceRecheck(ctx, job, state, data, timing, metrics)
}

// recordTMDbRecheckStage 保留错误链用于冷却/取消分类；仅添加阶段与耗时，输出仍由任务日志统一脱敏。
func recordTMDbRecheckStage(metrics map[string]int64, key, label string, started time.Time, err error) error {
	elapsed := time.Since(started)
	metrics[key+"_ms"] += elapsed.Milliseconds()
	if err == nil {
		return nil
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, repository.ErrTMDbRecheckChanged) {
		metrics[key+"_failed"]++
	}
	return fmt.Errorf("%s（耗时 %s）：%w", label, elapsed.Round(time.Millisecond), err)
}

func tmdbRecheckSubject(state *repository.TMDbRecheckState) string {
	coordinate := fmt.Sprintf("S%d", state.SeasonNum)
	if state.Kind == "episode" {
		coordinate += fmt.Sprintf("E%d", state.EpisodeNum)
	}
	return fmt.Sprintf("%s · %s · TMDb %s（%s）", strings.Join(strings.Fields(state.SeriesTitle), " "), coordinate, state.SeriesTMDbID, state.MetadataID)
}

func (s *ScraperService) retryTMDbRecheck(ctx context.Context, job *model.TMDbRecheckJob, metrics map[string]int64, cause error) ([]string, error) {
	// 取消后使用短独立上下文释放租约；失败仍可靠租约到期恢复。
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	attempts := job.Attempts + 1
	delay := min(5*time.Minute*time.Duration(1<<min(job.Attempts, 9)), 24*time.Hour)
	next := time.Now().UTC().Add(delay)
	status, reason := "retry", "上游请求或保存失败，请查看任务日志"
	if errors.Is(cause, repository.ErrTMDbRecheckChanged) {
		status, reason = "pending", "数据变更，等待重新核对"
		attempts = job.Attempts
	}
	if errors.Is(cause, errTMDbBudget) || errors.Is(cause, errTMDbDeferred) {
		status, reason = "pending", "请求预算或来源冷却，等待下轮"
		attempts = job.Attempts
	}
	if errors.Is(cause, context.Canceled) {
		status, reason = "pending", "执行已取消"
		attempts = job.Attempts
	}
	err := s.repo.Metadata.FinishTMDbRecheck(cleanup, job, status, reason, &next, attempts)
	if err != nil && !errors.Is(err, repository.ErrTMDbRecheckChanged) {
		return nil, err
	}
	if errors.Is(cause, errTMDbBudget) || errors.Is(cause, errTMDbDeferred) {
		return nil, cause
	}
	if status == "retry" {
		metrics["failed"]++
		return []string{"⚠️ " + job.MetadataID + "，" + reason + "：" + sanitizeTaskLogError(cause).Error()}, nil
	}
	return []string{"⚠️ " + job.MetadataID + "，" + reason}, nil
}
