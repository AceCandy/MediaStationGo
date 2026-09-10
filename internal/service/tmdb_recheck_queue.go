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

func (s *ScraperService) runTMDbRecheckQueue(ctx context.Context, metrics map[string]int64, task *TaskHandle) error {
	ctx = withTMDbSeasonBatch(ctx)
	defer func() { metrics["requests"] = tmdbSeasonBatchFromContext(ctx).requests.Load() }()
	started, lastReport := time.Now(), time.Time{}
	lastStage := ""
	report := func(stage, message string, force bool, details []string) {
		metrics["requests"] = tmdbSeasonBatchFromContext(ctx).requests.Load()
		if task == nil {
			return
		}
		if force || stage != lastStage || time.Since(lastReport) >= 5*time.Second {
			message = fmt.Sprintf("%s，耗时 %s", message, time.Since(started).Round(time.Second))
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
	report("expand", "正在归并季集待办变更", true, nil)
	for {
		more, err := s.repo.Metadata.ExpandTMDbRecheckChange(ctx)
		if err != nil {
			return err
		}
		if !more {
			break
		}
		metrics["change_batches"]++
		report("expand", fmt.Sprintf("本次已归并季集变更 %d 批", metrics["change_batches"]), false, nil)
	}
	report("expand", fmt.Sprintf("季集变更归并完成，本次 %d 批", metrics["change_batches"]), true, nil)
	report("recheck", "开始领取到期待办并检查 TMDb 季/集信息", true, nil)
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
				job, err := s.repo.Metadata.ClaimTMDbRecheck(ctx)
				if err != nil {
					results <- result{err: err}
					return
				}
				if job == nil {
					return
				}
				local := map[string]int64{"scanned": 1}
				details, err := s.processTMDbRecheck(ctx, job, local)
				results <- result{metrics: local, details: details, err: err}
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
		report("recheck", fmt.Sprintf("本次已处理 %d 个待办，请求 %d 次，上游未找到 %d 个，失败 %d 个", metrics["scanned"], metrics["requests"], metrics["not_found"], metrics["failed"]), false, res.details)
	}
	report("recheck", fmt.Sprintf("到期待办处理结束，本次 %d 个，请求 %d 次，上游未找到 %d 个，失败 %d 个", metrics["scanned"], metrics["requests"], metrics["not_found"], metrics["failed"]), true, nil)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return firstErr
}

func (s *ScraperService) processTMDbRecheck(parent context.Context, job *model.TMDbRecheckJob, metrics map[string]int64) ([]string, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stopped := make(chan struct{})
	defer func() { cancel(); <-stopped }()
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(repository.TMDbRecheckLease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.repo.Metadata.RenewTMDbRecheck(ctx, job) != nil {
					cancel()
					return
				}
			}
		}
	}()
	state, err := s.repo.Metadata.TMDbRecheckState(ctx, job.MetadataID)
	if err != nil {
		return s.retryTMDbRecheck(parent, job, metrics, err)
	}
	finish := func(status, reason string, due *time.Time) error {
		return s.repo.Metadata.CommitTMDbRecheck(ctx, job, state, func(repos *repository.Container) error {
			return repos.Metadata.FinishTMDbRecheck(ctx, job, status, reason, due, 0)
		})
	}
	identityValid := state != nil && state.IdentityValid
	if identityValid {
		id, parseErr := strconv.Atoi(state.SeriesTMDbID)
		identityValid = parseErr == nil && id > 0
	}
	if state == nil || !state.Playable || !tmdbMetadataCandidateNeedsRecheck(state.TMDbMetadataRecheckCandidate) {
		err = finish("done", "", nil)
	} else if !identityValid {
		next := time.Now().UTC().Add(7 * 24 * time.Hour)
		err = finish("blocked", "TMDb 标识缺失或不唯一", &next)
	} else if state.CheckedAt != nil && state.CheckedAt.Add(tmdbEpisodeMetadataRecheckCooldown).After(time.Now()) {
		next := state.CheckedAt.Add(tmdbEpisodeMetadataRecheckCooldown)
		err = finish("pending", "成功检查后的 72 小时冷却", &next)
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
	requestCtx, cancel := context.WithTimeout(ctx, tmdbDetailsTimeout)
	data, err := s.fetchTMDbMetadataRecheck(requestCtx, state.TMDbMetadataRecheckCandidate, seriesID)
	cancel()
	metrics["requests"]++
	if isTMDbHTTPStatus(err, http.StatusNotFound) || errors.Is(err, errTMDbEpisodeMissingFromSeason) {
		next := time.Now().UTC().Add(tmdbEpisodeMetadataRecheckCooldown)
		job.NotFoundIdentity = state.RequestIdentity()
		reason := "TMDb 上游未找到该季/集（404），3 天后复核；请核对剧集匹配及编号，勿仅凭 404 删除文件"
		if errors.Is(err, errTMDbEpisodeMissingFromSeason) {
			reason = "TMDb 整季清单未包含该集，3 天后复核；请核对匹配及编号，勿据此删除文件"
		}
		err = s.repo.Metadata.CommitTMDbRecheck(ctx, job, state, func(repos *repository.Container) error {
			return repos.Metadata.FinishTMDbRecheck(ctx, job, "not_found", reason, &next, job.Attempts+1)
		})
		if err != nil {
			return s.retryTMDbRecheck(parent, job, metrics, err)
		}
		metrics["not_found"]++
		return []string{"⚠️ " + job.MetadataID + "，" + reason}, nil
	}
	if err == nil && (data == nil || data.id <= 0 || !json.Valid(data.rawJSON)) {
		err = errors.New("TMDb 返回数据无效")
	}
	if err != nil {
		return s.retryTMDbRecheck(parent, job, metrics, err)
	}
	var asset *model.ArtworkAsset
	if state.ArtworkMissing && strings.TrimSpace(data.artworkURL) != "" {
		if s.artwork.imageProxy == nil {
			return s.retryTMDbRecheck(parent, job, metrics, errors.New("图片服务不可用"))
		}
		if err = s.artwork.imageProxy.RemoveFailed(data.artworkURL); err == nil {
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
		if err != nil {
			return s.retryTMDbRecheck(parent, job, metrics, err)
		}
	}
	credits := s.prepareCreditInputs(ctx, data.credits)
	savedMetrics := map[string]int64{}
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
	if err != nil {
		return s.retryTMDbRecheck(parent, job, metrics, err)
	}
	for k, v := range savedMetrics {
		metrics[k] += v
	}
	return details, nil
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
