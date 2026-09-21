package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var errMediaScrapeClaimConflict = errors.New("media scrape claim conflict")

// processNextMediaScrape 每次只处理一部电影或一部电视剧。
func (s *ScraperService) processNextMediaScrape(ctx context.Context) (bool, error) {
	group, err := s.claimNextPendingMediaGroup(ctx)
	if err != nil || group == nil {
		return false, err
	}
	s.wakeMediaScrapeWorkers()
	startedAt := time.Now()

	name := strings.TrimSpace(group.Representative.Title)
	if name == "" {
		name = group.Representative.ID
	}
	trigger := strings.TrimSpace(group.Representative.ScrapeTrigger)
	if trigger == "" {
		trigger = TaskTriggerEvent
	}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindScrape, trigger, "媒体入库刮削："+name, TaskUpdate{
			Stage: "waiting", SourcePath: group.Representative.Path, Message: "已领取入库任务，等待刮削资源",
		})
		if task == nil {
			_ = s.resetScrapeGroupPending(context.Background(), *group)
			return false, errors.New("create scrape task execution failed")
		}
	}
	s.scrapeRunMu.RLock()
	defer s.scrapeRunMu.RUnlock()
	if ctx.Err() != nil {
		_ = s.resetScrapeGroupPending(context.Background(), *group)
		task.Finish(ctx.Err(), TaskUpdate{Stage: "completed", Message: "入库任务已取消"})
		return false, ctx.Err()
	}
	task.Update(TaskUpdate{Stage: "scrape", Message: "正在处理已入库媒体刮削"})
	options := ScrapeOptions{}
	options.DeferEpisodeDetails = true
	options.timings = &scrapeTimings{}
	options.result = &scrapeResult{}
	err = s.enrichCandidateGroup(ctx, *group, options)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		_ = s.resetScrapeGroupPending(context.Background(), *group)
	}
	metrics := map[string]int64{"processed": 1}
	if s.mediaIsMatched(ctx, group.Representative.ID) {
		metrics["matched"] = 1
	}
	if err != nil {
		metrics["errors"] = 1
		if s.log != nil {
			s.log.Warn("enrich pending media failed", zap.String("media", group.Representative.ID), zap.Error(err))
		}
	}
	if task != nil {
		media, _ := s.repo.Media.FindByID(ctx, group.Representative.ID)
		safeErr := sanitizeTaskLogError(err)
		task.Finish(safeErr, TaskUpdate{Stage: "completed", Message: "已入库媒体刮削结束", Metrics: metrics, Details: []string{mediaScrapeTaskDetail(*group, media, options.result.Source, safeErr)}})
	}
	if s.log != nil {
		s.log.Info("auto media scrape timing",
			zap.String("media_id", group.Representative.ID),
			zap.Int("file_count", len(group.MediaIDs)),
			zap.Int64("candidate_generation_ms", options.timings.CandidateGeneration.Milliseconds()),
			zap.Int64("provider_lookup_ms", options.timings.ProviderLookup.Milliseconds()),
			zap.Int64("metadata_persist_ms", options.timings.MetadataPersist.Milliseconds()),
			zap.Int64("artwork_ms", options.timings.Artwork.Milliseconds()),
			zap.Int64("tmdb_extended_details_ms", options.timings.TMDbExtendedDetails.Milliseconds()),
			zap.Int64("total_ms", time.Since(startedAt).Milliseconds()),
			zap.Bool("success", err == nil))
	}
	return true, nil
}

func mediaScrapeTaskDetail(group scrapeCandidateGroup, media *model.Media, source string, scrapeErr error) string {
	current := group.Representative
	if media != nil {
		current = *media
	}
	name := strings.TrimSpace(current.Title)
	if name == "" {
		name = current.ID
	}
	prefix := fmt.Sprintf("媒体 %s（%s，共 %d 个文件）", name, current.ID, len(group.MediaIDs))
	if scrapeErr != nil {
		return fmt.Sprintf("❌ %s: 刮削失败: %v", prefix, scrapeErr)
	}
	switch current.ScrapeStatus {
	case "matched":
		switch source {
		case "existing_metadata":
			return "✅ " + prefix + ": 命中已有元数据"
		case "local_nfo":
			return "✅ " + prefix + ": 本地 NFO 入库"
		case "":
			return "✅ " + prefix + ": 已匹配元数据"
		default:
			return fmt.Sprintf("✅ %s: 网络刮削（%s）", prefix, scrapeProviderLabel(source))
		}
	case "no_match":
		return "⚠️ " + prefix + ": 未找到匹配元数据"
	case "error":
		if strings.TrimSpace(current.ScrapeError) != "" {
			return fmt.Sprintf("❌ %s: 刮削失败: %s", prefix, sanitizeTaskLogError(errors.New(current.ScrapeError)).Error())
		}
		return "❌ " + prefix + ": 刮削失败"
	default:
		return fmt.Sprintf("ℹ️ %s: 刮削状态 %s", prefix, current.ScrapeStatus)
	}
}

func scrapeProviderLabel(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "tmdb":
		return "TMDB"
	case "douban":
		return "豆瓣"
	case "bangumi":
		return "Bangumi"
	case "thetvdb":
		return "TheTVDB"
	case "adult":
		return "成人数据源"
	default:
		return source
	}
}

func (s *ScraperService) claimNextPendingMediaGroup(ctx context.Context) (*scrapeCandidateGroup, error) {
	var claimed *scrapeCandidateGroup
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先选代表，再加载其完整作品组；领取一部作品不应载入整个待办库。
		var seed model.Media
		pending := tx.Where("scrape_status IS NULL OR scrape_status = '' OR scrape_status = 'pending'").
			Where("COALESCE(catalog_source, '') = ''").
			Where(`NOT EXISTS (
				SELECT 1 FROM media AS running_media
				WHERE running_media.scrape_status = 'running' AND COALESCE(running_media.catalog_source, '') = '' AND (
					(BTRIM(COALESCE(media.series_hint, '')) <> '' AND BTRIM(running_media.series_hint) = BTRIM(media.series_hint)) OR
					(BTRIM(COALESCE(media.series_hint, '')) = '' AND BTRIM(COALESCE(media.metadata_id, '')) <> '' AND BTRIM(COALESCE(running_media.series_hint, '')) = '' AND BTRIM(running_media.metadata_id) = BTRIM(media.metadata_id)) OR
					(BTRIM(COALESCE(media.series_hint, '')) = '' AND BTRIM(COALESCE(media.metadata_id, '')) = '' AND running_media.id = media.id)
				)
			)`)
		result := pending.Session(&gorm.Session{}).
			Order("CASE WHEN COALESCE(season_num, 0) > 0 OR COALESCE(episode_num, 0) > 0 THEN 1 ELSE 0 END").
			Order("id ASC").Limit(1).Find(&seed)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		// 两次 SELECT 之间别的 worker 可能已领取；完整组查询也必须排除正在运行的作品。
		q := pending.Session(&gorm.Session{})
		switch {
		case strings.TrimSpace(seed.SeriesID) != "":
			q = q.Where("BTRIM(COALESCE(series_hint, '')) = ?", strings.TrimSpace(seed.SeriesID))
		case strings.TrimSpace(seed.MetadataID) != "":
			q = q.Where("BTRIM(COALESCE(series_hint, '')) = '' AND BTRIM(COALESCE(metadata_id, '')) = ?", strings.TrimSpace(seed.MetadataID))
		default:
			q = q.Where("id = ?", seed.ID)
		}
		var rows []model.Media
		if err := q.Order("CASE WHEN COALESCE(season_num, 0) > 0 OR COALESCE(episode_num, 0) > 0 THEN 1 ELSE 0 END").Order("id").Find(&rows).Error; err != nil {
			return err
		}
		groups, err := groupScrapeCandidateRows(rows)
		if err != nil || len(groups) == 0 {
			return err
		}
		group := groups[0]
		res := tx.Model(&model.Media{}).
			Where("id = ANY(?) AND (scrape_status IS NULL OR scrape_status = '' OR scrape_status = ?)", &group.MediaIDs, "pending").
			Where("COALESCE(catalog_source, '') = ''").
			Update("scrape_status", "running")
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(group.MediaIDs)) {
			return errMediaScrapeClaimConflict
		}
		group.Representative.ScrapeStatus = "running"
		claimed = &group
		return nil
	})
	if errors.Is(err, errMediaScrapeClaimConflict) {
		return nil, nil
	}
	return claimed, err
}

func (s *ScraperService) recoverRunningMediaScrapes(ctx context.Context) error {
	return s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("scrape_status = ?", "running").
		Where("COALESCE(catalog_source, '') = ''").
		Update("scrape_status", "pending").Error
}

func (s *ScraperService) hasActiveMediaScrapes(ctx context.Context) (bool, error) {
	var active bool
	err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Select("COUNT(*) > 0").
		Where("COALESCE(catalog_source, '') = ''").
		Where("scrape_status IS NULL OR scrape_status = '' OR scrape_status IN ?", []string{"pending", "running"}).
		Scan(&active).Error
	return active, err
}

func (s *ScraperService) resetScrapeGroupPending(ctx context.Context, group scrapeCandidateGroup) error {
	// 只退回本次实际领取的文件，避免空白身份漏退或重置同作品其他来源/已完成文件。
	return s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("id = ANY(?) AND COALESCE(catalog_source, '') = ''", &group.MediaIDs).
		Updates(map[string]any{"scrape_status": "pending", "scrape_error": ""}).Error
}

func (s *ScraperService) enrichCandidateGroup(ctx context.Context, group scrapeCandidateGroup, options ScrapeOptions) error {
	lib, err := s.repo.Library.FindByID(ctx, group.Representative.LibraryID)
	if err != nil {
		return err
	}
	// NFO 剧集的每个文件都有独立单集 NFO，不能把代表集的 metadata 同步给整组。
	if libraryUsesNFOOnly(lib) {
		var scrapeErrors []error
		for _, mediaID := range group.MediaIDs {
			media, err := s.repo.Media.FindByID(ctx, mediaID)
			if err != nil {
				scrapeErrors = append(scrapeErrors, err)
				continue
			}
			if media == nil {
				scrapeErrors = append(scrapeErrors, fmt.Errorf("media %s not found", mediaID))
				continue
			}
			if err := s.enrichOneWithOptions(ctx, media, options); err != nil {
				scrapeErrors = append(scrapeErrors, err)
			}
		}
		return errors.Join(scrapeErrors...)
	}
	representative := &group.Representative
	err = s.enrichOneWithOptions(ctx, representative, options)
	if syncErr := s.syncScrapeCandidateGroup(ctx, group); syncErr != nil {
		return syncErr
	}
	return err
}

// ResetLibraryScrape 将目标库重新置为待刮削并唤醒统一 worker。
func (s *ScraperService) ResetLibraryScrape(ctx context.Context, libraryID string, includeMatched bool) (int64, error) {
	statuses := []string{"pending", "no_match", "error"}
	if includeMatched {
		statuses = append(statuses, "matched")
	}
	res := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("COALESCE(catalog_source, '') = ''").
		Where("library_id = ? AND (scrape_status IS NULL OR scrape_status = '' OR scrape_status IN ?)", libraryID, statuses).
		Updates(map[string]any{"scrape_status": "pending", "scrape_trigger": TaskTriggerManual, "scrape_error": ""})
	if res.Error == nil && res.RowsAffected > 0 {
		s.wakeMediaScrapeWorkers()
	}
	return res.RowsAffected, res.Error
}

func (s *ScraperService) ResetMediaScrape(ctx context.Context, mediaID string) (*model.Media, error) {
	media, err := s.repo.Media.FindByID(ctx, mediaID)
	if err != nil || media == nil {
		return media, err
	}
	if media.CatalogSource != "" {
		return nil, errors.New("独立资料媒体请使用对应来源的扫描或刷新任务")
	}
	q := s.repo.DB.WithContext(ctx).Model(&model.Media{})
	if strings.TrimSpace(media.SeriesID) != "" {
		q = q.Where("series_hint = ?", media.SeriesID)
	} else if strings.TrimSpace(media.MetadataID) != "" {
		q = q.Where("metadata_id = ?", media.MetadataID)
	} else {
		q = q.Where("id = ?", media.ID)
	}
	if err := q.Updates(map[string]any{"scrape_status": "pending", "scrape_trigger": TaskTriggerManual, "scrape_error": ""}).Error; err != nil {
		return nil, err
	}
	s.WakeScrapeWorker()
	return media, nil
}
