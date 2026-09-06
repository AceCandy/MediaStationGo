package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindArtwork, trigger, "TMDb 集信息补全/复查", TaskUpdate{Stage: "scan", Message: "正在补全或复查 TMDb 集信息", Metrics: metrics})
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
	now := time.Now().UTC()
	afterID := ""
	for {
		page, err := s.repo.Metadata.ListTMDbEpisodeMetadataRecheckAfter(ctx, afterID, now.Add(-tmdbEpisodeMetadataRecheckCooldown), tmdbEpisodeMetadataRecheckPageLimit)
		if err != nil {
			return fail(err, "TMDb 集信息补全/复查失败")
		}
		if len(page) == 0 {
			break
		}
		for _, candidate := range page {
			afterID = candidate.MetadataID
			if !tmdbEpisodeCandidateNeedsRecheck(candidate) {
				continue
			}
			metrics["scanned"]++
			details, err := s.recheckTMDbEpisodeMetadata(ctx, candidate, now, metrics)
			if err != nil {
				metrics["failed"]++
			}
			if task != nil && len(details) > 0 {
				task.Update(TaskUpdate{Stage: "recheck", Metrics: metrics, Details: details})
			}
		}
		if len(page) < tmdbEpisodeMetadataRecheckPageLimit {
			break
		}
	}
	if metrics["failed"] > 0 {
		return fail(fmt.Errorf("%d TMDb episode metadata rechecks failed", metrics["failed"]), "TMDb 集信息补全/复查完成，但存在失败")
	}
	if metrics["checked"] > 0 {
		s.invalidateMediaCache(ctx)
	}
	if task != nil {
		task.Finish(nil, TaskUpdate{Stage: "completed", Message: "TMDb 集信息补全/复查完成", Metrics: metrics})
	}
	return nil
}

func tmdbEpisodeCandidateNeedsRecheck(item repository.TMDbEpisodeMetadataRecheckCandidate) bool {
	return strings.TrimSpace(item.Title) == "" || tmdbEntityTitleIsGenerated(item.Title, model.MetadataKindEpisode) ||
		strings.TrimSpace(item.Overview) == "" || strings.TrimSpace(item.ReleaseDate) == "" || item.StillMissing
}

func (s *ScraperService) recheckTMDbEpisodeMetadata(ctx context.Context, candidate repository.TMDbEpisodeMetadataRecheckCandidate, now time.Time, metrics map[string]int64) ([]string, error) {
	subject := fmt.Sprintf("%s，S%02dE%02d，集=%s，TMDb=%s", strings.TrimSpace(candidate.SeriesTitle), candidate.SeasonNum, candidate.EpisodeNum, strings.TrimSpace(candidate.Title), candidate.SeriesTMDbID)
	seriesTMDbID, err := strconv.Atoi(candidate.SeriesTMDbID)
	if err != nil || seriesTMDbID <= 0 {
		err = errors.New("invalid Series TMDb identity")
		return []string{"❌ " + subject + "，结果=失败：" + err.Error()}, err
	}
	detailCtx, cancel := context.WithTimeout(ctx, tmdbDetailsTimeout)
	episode, err := s.tmdb.GetTVEpisodeDetails(detailCtx, seriesTMDbID, candidate.SeasonNum, candidate.EpisodeNum)
	cancel()
	metrics["requests"]++
	if err != nil || episode == nil || episode.ID <= 0 {
		if err == nil {
			err = errors.New("TMDb episode details unavailable")
		}
		return []string{"❌ " + subject + "，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	if err := s.persistCredits(ctx, candidate.MetadataID, episode.LoadedCreditTypes, episode.Credits); err != nil {
		return []string{"❌ " + subject + "，动作=保存演职员，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	details := []string{}
	stillSatisfied := !candidate.StillMissing
	if candidate.StillMissing && strings.TrimSpace(episode.CatalogStillURL) != "" {
		_, existing, err := s.artwork.importCatalogRemote(ctx, candidate.MetadataID, model.ArtworkTypeStill, "tmdb", strings.TrimSpace(episode.CatalogStillURL))
		if err != nil {
			return []string{"❌ " + subject + "，动作=保存 still，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
		}
		stillSatisfied = true
		if existing {
			metrics["concurrent_skipped"]++
			details = append(details, "⏭️ "+subject+"，动作=保存 still，结果=已有并发选择")
		} else {
			metrics["still_saved"]++
			details = append(details, "✅ "+subject+"，动作=保存 still，结果=已保存到本地")
		}
	}
	item, err := s.repo.Metadata.FindByID(ctx, candidate.MetadataID)
	if err != nil || item == nil {
		if err == nil {
			err = errors.New("episode metadata unavailable")
		}
		return []string{"❌ " + subject + "，动作=保存集信息，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	updates, _ := tmdbEpisodeMetadataUpdates(nil, episode, 0)
	changed := changedTMDbEpisodeFields(item, updates)
	applyTMDbEpisodeMetadataUpdates(item, updates)
	// 补全占位集的真实标识和快照，让详情页缺失提示随成功补全恢复。
	if err := s.repo.Metadata.ReplaceIdentifierWithSnapshot(ctx, item.ID, "tmdb", model.MetadataKindEpisode, strconv.Itoa(episode.ID), episode.RawJSON, now); err != nil {
		return []string{"❌ " + subject + "，动作=保存集标识和快照，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	if item.TMDbEpisodeCheckedAt == nil || item.TMDbEpisodeCheckedAt.Before(now) {
		item.TMDbEpisodeCheckedAt = &now
	}
	if err := s.repo.Metadata.Update(ctx, item); err != nil {
		return []string{"❌ " + subject + "，动作=保存集信息，结果=可重试失败：" + sanitizeTaskLogError(err).Error()}, err
	}
	metrics["checked"]++
	if len(changed) > 0 {
		metrics["updated"]++
		details = append(details, "✅ "+subject+"，更新="+strings.Join(changed, "、"))
	}
	missing := missingTMDbEpisodeFields(item, stillSatisfied)
	if len(missing) > 0 {
		metrics["incomplete"]++
		details = append(details, "⚠️ "+subject+"，TMDb 仍缺="+strings.Join(missing, "、")+"，72 小时后再查")
	}
	return details, nil
}

func changedTMDbEpisodeFields(item *model.MetadataItem, updates map[string]any) []string {
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

func missingTMDbEpisodeFields(item *model.MetadataItem, stillSatisfied bool) []string {
	missing := []string{}
	if strings.TrimSpace(item.Title) == "" || tmdbEntityTitleIsGenerated(item.Title, model.MetadataKindEpisode) {
		missing = append(missing, "标题")
	}
	if strings.TrimSpace(item.Overview) == "" {
		missing = append(missing, "简介")
	}
	if strings.TrimSpace(item.ReleaseDate) == "" {
		missing = append(missing, "播出日期")
	}
	if !stillSatisfied {
		missing = append(missing, "still")
	}
	return missing
}
