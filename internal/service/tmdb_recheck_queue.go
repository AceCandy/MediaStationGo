package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const tmdbRecheckDetailsTimeout = 30 * time.Second

func (s *ScraperService) runTMDbRecheckQueue(ctx context.Context, metrics map[string]int64, task *TaskHandle) error {
	ctx = withTMDbSeasonBatch(ctx)
	defer func() { metrics["requests"] = tmdbSeasonBatchFromContext(ctx).requests.Load() }()
	started, lastReport := time.Now(), time.Time{}
	stageStarted := started
	lastStage := ""
	report := func(stage, message string, force bool, details []string) {
		metrics["requests"] = tmdbSeasonBatchFromContext(ctx).requests.Load()
		if task == nil {
			return
		}
		if force || stage != lastStage || time.Since(lastReport) >= 5*time.Second {
			if stage != lastStage {
				stageStarted = time.Now()
			}
			message = fmt.Sprintf("%s，阶段耗时 %s，累计耗时 %s", message, time.Since(stageStarted).Round(time.Second), time.Since(started).Round(time.Second))
			lastReport, lastStage = time.Now(), stage
		} else {
			message = ""
			if len(details) == 0 {
				return
			}
		}
		task.Update(TaskUpdate{Stage: stage, Message: message, Metrics: metrics, Details: details})
	}
	report("assets", "正在归并图片资产变更", true, nil)
	for {
		more, err := s.repo.Metadata.ExpandTMDbRecheckAsset(ctx)
		if err != nil {
			return err
		}
		if !more {
			break
		}
		metrics["asset_batches"]++
		report("assets", fmt.Sprintf("本次已归并图片资产 %d 批", metrics["asset_batches"]), false, nil)
	}
	report("assets", fmt.Sprintf("图片资产归并完成，本次 %d 批", metrics["asset_batches"]), true, nil)
	report("scan", "正在分批核对季集复查待办", true, nil)
	for {
		more, scanned, err := s.repo.Metadata.ScanTMDbRecheckFiles(ctx)
		if err != nil {
			return err
		}
		metrics["scan_files"] += int64(scanned)
		report("scan", fmt.Sprintf("本次已核对 %d 个文件", metrics["scan_files"]), false, nil)
		if !more {
			break
		}
	}
	report("scan", fmt.Sprintf("文件核对结束，本次 %d 个文件（低频核对未到期时跳过）", metrics["scan_files"]), true, nil)
	report("expand", "正在归并季集待办变更（本地登记处理，尚未开始请求 TMDb）", true, nil)
	for {
		more, err := s.repo.Metadata.ExpandTMDbRecheckChange(ctx)
		if err != nil {
			return err
		}
		if !more {
			break
		}
		metrics["change_batches"]++
		report("expand", fmt.Sprintf("本次已处理季集变更登记 %d 次（非 TMDb 请求数）", metrics["change_batches"]), false, nil)
	}
	report("expand", fmt.Sprintf("季集变更归并完成，本次处理登记 %d 次", metrics["change_batches"]), true, nil)
	cutoff, err := s.repo.Metadata.TMDbRecheckPassBoundary(ctx)
	if err != nil {
		return err
	}
	remaining, err := s.repo.Metadata.CountTMDbRechecksDue(ctx, cutoff)
	if err != nil {
		return err
	}
	metrics["total"], metrics["remaining"] = remaining, remaining
	lastCount := time.Now()
	report("recheck", fmt.Sprintf("开始检查 TMDb 季/集信息，本轮到期 %d 个；新到期重试留待下轮", remaining), true, nil)
	type result struct {
		metrics map[string]int64
		details []string
		err     error
	}
	results := make(chan result, 3)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				lease, err := s.repo.Metadata.ClaimTMDbRecheckSeason(ctx, cutoff)
				if err != nil {
					results <- result{err: err}
					return
				}
				if lease == nil {
					return
				}
				results <- result{metrics: map[string]int64{"seasons_scanned": 1}}
				err = s.processTMDbRecheckSeason(ctx, lease, cutoff, func(local map[string]int64, details []string) {
					results <- result{metrics: local, details: details}
				})
				if err != nil {
					results <- result{err: err}
					return
				}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()
	var firstErr error
	for res := range results {
		for k, v := range res.metrics {
			metrics[k] += v
		}
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		if time.Since(lastCount) >= 30*time.Second {
			remaining, countErr := s.repo.Metadata.CountTMDbRechecksDue(ctx, cutoff)
			if countErr == nil {
				metrics["remaining"] = remaining
			} else {
				res.details = append(res.details, "⚠️ 本轮剩余量更新失败，暂保留上次统计："+sanitizeTaskLogError(countErr).Error())
			}
			lastCount = time.Now()
		}
		metrics["requests"] = tmdbSeasonBatchFromContext(ctx).requests.Load()
		report("recheck", fmt.Sprintf("本次已领取 %d 季、核对 %d 条元数据，本轮剩余 %d 条（每 30 秒更新），请求 %d 次，上游未找到 %d 条，失败待重试 %d 条", metrics["seasons_scanned"], metrics["scanned"], metrics["remaining"], metrics["requests"], metrics["not_found"], metrics["failed"]), false, res.details)
	}
	if remaining, err := s.repo.Metadata.CountTMDbRechecksDue(ctx, cutoff); err == nil {
		metrics["remaining"] = remaining
	} else if firstErr == nil {
		firstErr = err
	}
	report("recheck", fmt.Sprintf("本轮领取结束，已领取 %d 季、核对 %d 条元数据，剩余到期 %d 条，请求 %d 次，上游未找到 %d 条，失败待重试 %d 条；尚未释放的租约及后续到期条目留待下轮", metrics["seasons_scanned"], metrics["scanned"], metrics["remaining"], metrics["requests"], metrics["not_found"], metrics["failed"]), true, nil)
	report("recheck", fmt.Sprintf("阶段累计耗时：详情 %dms（失败 %d 次），图片 %dms（失败 %d 次），数据库核对及保存 %dms（失败 %d 次）", metrics["details_ms"], metrics["details_failed"], metrics["image_ms"], metrics["image_failed"], metrics["save_ms"], metrics["save_failed"]), true, nil)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return firstErr
}

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
	if identityValid {
		id, parseErr := strconv.Atoi(state.SeriesTMDbID)
		identityValid = parseErr == nil && id > 0
	}
	if state == nil || !state.Playable || (!tmdbMetadataCandidateNeedsRecheck(state.TMDbMetadataRecheckCandidate) && job.NotFoundIdentity == "") {
		err = finish("done", "", nil)
	} else if !identityValid {
		next := time.Now().UTC().Add(7 * 24 * time.Hour)
		err = finish("blocked", "TMDb 标识缺失或不唯一", &next)
	} else if state.CheckedAt != nil && state.CheckedAt.Add(tmdbEpisodeMetadataRecheckCooldown).After(time.Now()) {
		next := state.CheckedAt.Add(tmdbEpisodeMetadataRecheckCooldown)
		if job.NotFoundIdentity != "" {
			err = finish("not_found", job.LastError, &next)
		} else {
			err = finish("pending", "成功检查后的 72 小时冷却", &next)
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
	if isTMDbHTTPStatus(err, http.StatusNotFound) || errors.Is(err, errTMDbEpisodeMissingFromSeason) {
		recordTMDbRecheckStage(metrics, "details", "详情请求", stageStarted, nil)
		next := time.Now().UTC().Add(tmdbEpisodeMetadataRecheckCooldown)
		job.NotFoundIdentity = state.RequestIdentity()
		reason := "TMDb 上游未找到该季/集（404），3 天后复核；请核对剧集匹配及编号，勿仅凭 404 删除文件"
		if errors.Is(err, errTMDbEpisodeMissingFromSeason) {
			reason = "TMDb 整季清单未包含该集，3 天后复核；请核对匹配及编号，勿据此删除文件"
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
	var asset *model.ArtworkAsset
	if state.ArtworkMissing && strings.TrimSpace(data.artworkURL) != "" {
		stageStarted = time.Now()
		if s.artwork.imageProxy == nil {
			err = errors.New("图片服务不可用")
		} else if err = s.artwork.imageProxy.RemoveFailed(data.artworkURL); err == nil {
			var bytes []byte
			bytes, _, err = s.artwork.imageProxy.Fetch(ctx, data.artworkURL)
			if err == nil {
				kind := model.ArtworkTypeStill
				if state.Kind == "season" {
					kind = model.ArtworkTypePoster
				}
				asset, err = s.artwork.prepareAsset(state.MetadataID, kind, bytes)
			}
		}
		err = recordTMDbRecheckStage(metrics, "image", "图片下载及落盘", stageStarted, err)
		if err != nil {
			return s.retryTMDbRecheck(parent, job, metrics, err)
		}
	}
	credits := s.prepareCreditInputs(ctx, data.credits)
	savedMetrics := map[string]int64{}
	stageStarted = time.Now()
	err = s.repo.Metadata.CommitTMDbRecheck(ctx, job, state, func(repos *repository.Container) error {
		local := &ScraperService{repo: repos, log: s.log}
		now := time.Now().UTC()
		var saveErr error
		details, saveErr = local.persistTMDbMetadataRecheck(ctx, state.TMDbMetadataRecheckCandidate, now, savedMetrics, data, asset, credits, true)
		if saveErr != nil {
			return saveErr
		}
		current, saveErr := repos.Metadata.TMDbRecheckState(ctx, job.MetadataID)
		if saveErr != nil {
			return saveErr
		}
		status := "done"
		var next *time.Time
		if current != nil && tmdbMetadataCandidateNeedsRecheck(current.TMDbMetadataRecheckCandidate) {
			status = "pending"
			due := now.Add(tmdbEpisodeMetadataRecheckCooldown)
			next = &due
		}
		return repos.Metadata.FinishTMDbRecheck(ctx, job, status, "", next, 0)
	})
	err = recordTMDbRecheckStage(metrics, "save", "数据库保存", stageStarted, err)
	if err != nil {
		return s.retryTMDbRecheck(parent, job, metrics, err)
	}
	for k, v := range savedMetrics {
		metrics[k] += v
	}
	return details, nil
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
	if errors.Is(cause, context.Canceled) {
		status, reason = "pending", "执行已取消"
		attempts = job.Attempts
	}
	err := s.repo.Metadata.FinishTMDbRecheck(cleanup, job, status, reason, &next, attempts)
	if err != nil && !errors.Is(err, repository.ErrTMDbRecheckChanged) {
		return nil, err
	}
	if status == "retry" {
		metrics["failed"]++
		return []string{"⚠️ " + job.MetadataID + "，" + reason + "：" + sanitizeTaskLogError(cause).Error()}, nil
	}
	return []string{"⚠️ " + job.MetadataID + "，" + reason}, nil
}
