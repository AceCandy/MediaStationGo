package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	tmdbEpisodeMetadataRecheckPageLimit = 200
	tmdbEpisodeMetadataRecheckCooldown  = 72 * time.Hour
)

func (s *ScraperService) runTMDbEpisodeMetadataRecheck(ctx context.Context, trigger string) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.tmdb == nil || !s.tmdb.Enabled() || s.artwork == nil {
		return errors.New("TMDb episode metadata recheck dependencies unavailable")
	}
	metrics := map[string]int64{}
	defer func() {
		if metrics["checked"] > 0 {
			s.invalidateMediaCache(ctx)
		}
	}()
	var task *TaskHandle
	if s.tasks != nil {
		// 执行名称沿用旧值，保持历史筛选及每日日志归属不变。
		task = s.tasks.StartTriggered(TaskKindArtwork, trigger, "TMDb 集信息补全/复查", TaskUpdate{Stage: "scan", Message: "正在补全或复查 TMDb 季/集信息", Metrics: metrics})
		if task == nil {
			return errors.New("create TMDb episode metadata recheck task execution failed")
		}
	}
	fail := func(err error, message string) error {
		if task != nil {
			task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: "failed", Message: message, Metrics: metrics})
		}
		return err
	}
	if err := s.runTMDbRecheckQueue(ctx, metrics, task); err != nil {
		return fail(err, "TMDb 季/集信息补全/复查失败或已取消")
	}
	if metrics["failed"] > 0 {
		return fail(fmt.Errorf("%d TMDb metadata rechecks failed", metrics["failed"]), "TMDb 季/集信息补全/复查完成，但存在失败")
	}
	if task != nil {
		task.Finish(nil, TaskUpdate{Stage: "completed", Message: "TMDb 季/集信息补全/复查完成", Metrics: metrics})
	}
	return nil
}

// recheckTMDbMetadataPage 限制三条在途复查，只有收集端写入总指标和任务日志。
func (s *ScraperService) recheckTMDbMetadataPage(ctx context.Context, page []repository.TMDbMetadataRecheckCandidate, now time.Time, metrics map[string]int64, task *TaskHandle) {
	jobs := make(chan repository.TMDbMetadataRecheckCandidate, len(page))
	for _, candidate := range page {
		if tmdbMetadataCandidateNeedsRecheck(candidate) {
			jobs <- candidate
		}
	}
	close(jobs)
	type result struct {
		metrics map[string]int64
		details []string
	}
	results := make(chan result, 3)
	var workers sync.WaitGroup
	for range min(3, len(jobs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					return
				}
				local := map[string]int64{"scanned": 1}
				details, err := s.recheckTMDbMetadata(ctx, candidate, now, local)
				if err != nil {
					local["failed"]++
				}
				results <- result{metrics: local, details: details}
			}
		}()
	}
	go func() { workers.Wait(); close(results) }()
	for result := range results {
		for key, count := range result.metrics {
			metrics[key] += count
		}
		if task != nil {
			task.Update(TaskUpdate{Stage: "recheck", Metrics: metrics, Details: result.details})
		}
	}
}

func tmdbMetadataCandidateNeedsRecheck(item repository.TMDbMetadataRecheckCandidate) bool {
	// 标题只随其他缺失信息顺带更新，不单独触发复查。
	return strings.TrimSpace(item.Overview) == "" || strings.TrimSpace(item.ReleaseDate) == "" || item.ArtworkMissing ||
		(item.Kind == model.MetadataKindSeason && (item.TMDbID == "" || item.SnapshotMissing))
}

func (s *ScraperService) recheckTMDbMetadata(ctx context.Context, candidate repository.TMDbMetadataRecheckCandidate, now time.Time, metrics map[string]int64) ([]string, error) {
	subject := fmt.Sprintf("%s，S%02dE%02d，集=%s，TMDb=%s", strings.TrimSpace(candidate.SeriesTitle), candidate.SeasonNum, candidate.EpisodeNum, strings.TrimSpace(candidate.Title), candidate.SeriesTMDbID)
	if candidate.Kind == model.MetadataKindSeason {
		subject = fmt.Sprintf("%s，S%02d，季=%s，TMDb=%s", strings.TrimSpace(candidate.SeriesTitle), candidate.SeasonNum, strings.TrimSpace(candidate.Title), candidate.SeriesTMDbID)
	}
	seriesTMDbID, err := strconv.Atoi(candidate.SeriesTMDbID)
	if err != nil || seriesTMDbID <= 0 {
		err = errors.New("invalid Series TMDb identity")
		return []string{"❌ " + subject + "，结果=失败：" + err.Error()}, err
	}
	detailCtx, cancel := context.WithTimeout(ctx, tmdbDetailsTimeout)
	metadata, err := s.fetchTMDbMetadataRecheck(detailCtx, candidate, seriesTMDbID)
	cancel()
	metrics["requests"]++
	if err != nil || metadata == nil || metadata.id <= 0 || !json.Valid(metadata.rawJSON) {
		if err == nil {
			err = errors.New("TMDb metadata details unavailable")
		}
		return []string{"❌ " + subject + "，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	return s.persistTMDbMetadataRecheck(ctx, candidate, now, metrics, metadata, nil, nil, false)
}

// persistTMDbMetadataRecheck 的队列路径只保存已下载内容，允许调用方持有短事务。
func (s *ScraperService) persistTMDbMetadataRecheck(ctx context.Context, candidate repository.TMDbMetadataRecheckCandidate, now time.Time, metrics map[string]int64, metadata *tmdbMetadataRecheckDetails, prepared *model.ArtworkAsset, credits []repository.CreditInput, downloaded bool) ([]string, error) {
	subject := fmt.Sprintf("%s，S%02dE%02d", candidate.SeriesTitle, candidate.SeasonNum, candidate.EpisodeNum)
	artworkType := model.ArtworkTypeStill
	if candidate.Kind == model.MetadataKindSeason {
		artworkType = model.ArtworkTypePoster
	}
	var creditErr error
	if downloaded {
		creditErr = s.saveCreditInputs(ctx, candidate.MetadataID, metadata.loadedCreditTypes, credits)
	} else {
		creditErr = s.persistCredits(ctx, candidate.MetadataID, metadata.loadedCreditTypes, metadata.credits)
	}
	if err := creditErr; err != nil {
		return []string{"❌ " + subject + "，动作=保存演职员，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	details := []string{}
	artworkSatisfied := !candidate.ArtworkMissing
	if candidate.ArtworkMissing && strings.TrimSpace(metadata.artworkURL) != "" {
		var existing bool
		var err error
		if downloaded {
			_, selected, saveErr := s.repo.Artwork.SaveCatalogSelection(ctx, candidate.MetadataID, artworkType, "tmdb", strings.TrimSpace(metadata.artworkURL), prepared)
			existing, err = !selected, saveErr
		} else {
			_, existing, err = s.artwork.importCatalogRemote(ctx, candidate.MetadataID, artworkType, "tmdb", strings.TrimSpace(metadata.artworkURL))
		}
		if err != nil {
			return []string{"❌ " + subject + "，动作=保存 " + artworkType + "，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
		}
		artworkSatisfied = true
		if existing {
			metrics["concurrent_skipped"]++
			details = append(details, "⏭️ "+subject+"，动作=保存 "+artworkType+"，结果=已有并发选择")
		} else {
			metrics[artworkType+"_saved"]++
			details = append(details, "✅ "+subject+"，动作=保存 "+artworkType+"，结果=已保存到本地")
		}
	}
	item, err := s.repo.Metadata.FindByID(ctx, candidate.MetadataID)
	if err != nil || item == nil {
		if err == nil {
			err = errors.New("metadata unavailable")
		}
		return []string{"❌ " + subject + "，动作=保存信息，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	changed := changedTMDbMetadataFields(item, metadata.updates)
	applyTMDbMetadataUpdates(item, metadata.updates)
	// 补全占位季、集的真实标识和快照，保留原元数据 ID 与媒体关联。
	if err := s.repo.Metadata.ReplaceIdentifierWithSnapshot(ctx, item.ID, "tmdb", candidate.Kind, strconv.Itoa(metadata.id), metadata.rawJSON, now); err != nil {
		return []string{"❌ " + subject + "，动作=保存标识和快照，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	checkedAt := &item.TMDbEpisodeCheckedAt
	if candidate.Kind == model.MetadataKindSeason {
		checkedAt = &item.TMDbSeasonCheckedAt
	}
	if *checkedAt == nil || (*checkedAt).Before(now) {
		*checkedAt = &now
	}
	if err := s.repo.Metadata.SaveTMDbMetadataRecheck(ctx, item); err != nil {
		return []string{"❌ " + subject + "，动作=保存信息，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	metrics["checked"]++
	metrics[candidate.Kind+"_checked"]++
	if len(changed) > 0 {
		metrics["updated"]++
		details = append(details, "✅ "+subject+"，更新="+strings.Join(changed, "、"))
	}
	missing := missingTMDbMetadataFields(item, artworkSatisfied, artworkType)
	if len(missing) > 0 {
		metrics["incomplete"]++
		details = append(details, "⚠️ "+subject+"，TMDb 仍缺="+strings.Join(missing, "、")+"，72 小时后再查")
	}
	return details, nil
}

func changedTMDbMetadataFields(item *model.MetadataItem, updates map[string]any) []string {
	changed := []string{}
	if value, ok := updates["title"].(string); ok && item.Title != value {
		changed = append(changed, "标题")
	}
	if value, ok := updates["overview"].(string); ok && item.Overview != value {
		changed = append(changed, "简介")
	}
	if value, ok := updates["release_date"].(string); ok && item.ReleaseDate != value {
		changed = append(changed, "播出日期")
	}
	if value, ok := updates["rating"].(float32); ok && item.Rating != value {
		changed = append(changed, "评分")
	}
	if value, ok := updates["year"].(int); ok && item.Year != value {
		changed = append(changed, "年份")
	}
	return changed
}

func missingTMDbMetadataFields(item *model.MetadataItem, artworkSatisfied bool, artworkType string) []string {
	missing := []string{}
	if strings.TrimSpace(item.Overview) == "" {
		missing = append(missing, "简介")
	}
	if strings.TrimSpace(item.ReleaseDate) == "" {
		missing = append(missing, "播出日期")
	}
	if !artworkSatisfied {
		missing = append(missing, artworkType)
	}
	return missing
}

// tmdbMetadataRecheckDetails 将季、集响应投影到共用的补全保存流程。
type tmdbMetadataRecheckDetails struct {
	id                int
	updates           map[string]any
	credits           []PersonCredit
	loadedCreditTypes []string
	artworkURL        string
	rawJSON           []byte
}

func (s *ScraperService) fetchTMDbMetadataRecheck(ctx context.Context, candidate repository.TMDbMetadataRecheckCandidate, seriesTMDbID int) (*tmdbMetadataRecheckDetails, error) {
	if candidate.Kind == model.MetadataKindSeason {
		season, err := s.tmdb.GetTVSeasonDetails(ctx, seriesTMDbID, candidate.SeasonNum)
		if err != nil || season == nil {
			return nil, err
		}
		if season.SeasonNumber != candidate.SeasonNum {
			return nil, ErrTMDbRefreshIdentity
		}
		return &tmdbMetadataRecheckDetails{id: season.ID, updates: tmdbMetadataUpdates(catalogSeasonItem("", season)), credits: season.Credits,
			loadedCreditTypes: season.LoadedCreditTypes, artworkURL: season.PosterURL, rawJSON: season.RawJSON}, nil
	}
	if candidate.Kind != model.MetadataKindEpisode {
		return nil, ErrTMDbRefreshIdentity
	}
	episode, err := s.tmdb.GetTVEpisodeDetails(ctx, seriesTMDbID, candidate.SeasonNum, candidate.EpisodeNum)
	if err != nil || episode == nil {
		return nil, err
	}
	updates, _ := tmdbEpisodeMetadataUpdates(nil, episode, 0)
	return &tmdbMetadataRecheckDetails{id: episode.ID, updates: updates, credits: episode.Credits,
		loadedCreditTypes: episode.LoadedCreditTypes, artworkURL: episode.CatalogStillURL, rawJSON: episode.RawJSON}, nil
}
