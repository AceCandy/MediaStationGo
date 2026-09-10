package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	artworkRepairPageLimit = 200
	artworkRecheckCooldown = 24 * time.Hour
)

func (s *ScraperService) runDoubanArtworkLocalRepair(ctx context.Context, trigger string) error {
	if s == nil || s.repo == nil || s.repo.Artwork == nil || s.artwork == nil || s.douban == nil {
		return errors.New("Douban artwork local repair dependencies unavailable")
	}
	metrics := map[string]int64{}
	task, err := s.startArtworkTask(trigger, "豆瓣图片本地化修复", "正在检查豆瓣海报候选", metrics)
	if err != nil {
		return err
	}
	afterID := ""
	for {
		page, err := s.repo.Artwork.ListDoubanArtworkCandidatesAfter(ctx, afterID, artworkRepairPageLimit)
		if err != nil {
			return finishArtworkTask(task, err, "豆瓣图片本地化修复失败", metrics)
		}
		if len(page) == 0 {
			break
		}
		for _, item := range page {
			metrics["scanned"]++
			detail, failed := s.repairDoubanArtworkCandidate(ctx, item, metrics)
			if failed {
				metrics["failed"]++
			}
			if task != nil && detail != "" {
				task.Update(TaskUpdate{Stage: "repair", Metrics: metrics, Details: []string{detail}})
			}
			afterID = item.CandidateID
		}
		if len(page) < artworkRepairPageLimit {
			break
		}
	}
	if metrics["failed"] > 0 {
		return finishArtworkTask(task, fmt.Errorf("%d Douban artwork repairs failed", metrics["failed"]), "豆瓣图片本地化修复完成，但存在失败", metrics)
	}
	return finishArtworkTask(task, nil, "豆瓣图片本地化修复完成", metrics)
}

func (s *ScraperService) repairDoubanArtworkCandidate(ctx context.Context, item repository.DoubanArtworkCandidate, metrics map[string]int64) (string, bool) {
	subject := fmt.Sprintf("《%s》（%s）", strings.TrimSpace(item.Title), item.MetadataID)
	available, err := s.artwork.doubanCandidateFileAvailable(item)
	if err != nil {
		return "❌ " + subject + "，动作=检查本地文件，结果=失败：" + sanitizeTaskLogError(err).Error(), true
	}
	small := strings.Contains(item.SourceURL, "s_ratio_poster") || item.Width <= 300
	if available && !small {
		metrics["large_skipped"]++
		return "", false
	}
	sourceURL := doubanRepairPosterURL(item.SnapshotPayload, item.SourceURL)
	if !validRemoteArtworkURL(sourceURL) {
		return "❌ " + subject + "，动作=选择豆瓣大图，结果=没有可用图片链接", true
	}
	sourceURL = s.douban.ResolveArtworkURL(ctx, sourceURL)
	if available && strings.TrimSpace(item.RepairCheckedURL) == sourceURL {
		metrics["checked_skipped"]++
		return "", false
	}
	if available {
		metrics["small"]++
	} else {
		metrics["missing"]++
	}
	_, updated, err := s.artwork.repairDoubanCandidate(ctx, item, sourceURL)
	if err != nil {
		if available && isRemoteImageHTTPStatus(err, http.StatusNotFound) {
			updated, saveErr := s.repo.Artwork.MarkDoubanCandidateRepairChecked(ctx, item, sourceURL)
			if saveErr != nil {
				return "❌ " + subject + "，动作=记录豆瓣大图状态，结果=失败：" + sanitizeTaskLogError(saveErr).Error(), true
			}
			if !updated {
				metrics["concurrent_skipped"]++
				return "⏭️ " + subject + "，动作=记录豆瓣大图状态，结果=候选已并发变更", false
			}
			metrics["small_retained"]++
			return "⏭️ " + subject + "，动作=检查豆瓣大图，结果=大图不存在，保留现有小图", false
		}
		return "❌ " + subject + "，动作=下载豆瓣大图，结果=可重试失败：" + sanitizeTaskLogError(err).Error(), true
	}
	if !updated {
		metrics["concurrent_skipped"]++
		return "⏭️ " + subject + "，动作=切换豆瓣大图，结果=候选已并发变更", false
	}
	metrics["repaired"]++
	return "✅ " + subject + "，动作=切换豆瓣大图，结果=已保存到本地", false
}

func doubanRepairPosterURL(snapshotPayload, fallbackURL string) string {
	sourceURL := ""
	if strings.TrimSpace(snapshotPayload) != "" {
		if match, err := doubanMatchFromRawJSON("", []byte(snapshotPayload)); err == nil && match != nil {
			sourceURL = strings.TrimSpace(match.PosterURL)
		}
	}
	if sourceURL == "" {
		sourceURL = strings.TrimSpace(fallbackURL)
	}
	if largeURL := deriveDoubanLargePosterURL(sourceURL); largeURL != "" {
		return largeURL
	}
	return sourceURL
}

func (s *ScraperService) runTMDbArtworkLocalRepair(ctx context.Context, trigger string) error {
	ctx = withTMDbSeasonBatch(ctx)
	if s == nil || s.repo == nil || s.repo.Artwork == nil || s.artwork == nil {
		return errors.New("TMDb artwork local repair dependencies unavailable")
	}
	metrics := map[string]int64{}
	task, err := s.startArtworkTask(trigger, "TMDb 图片本地化修复", "正在检查 TMDb 图片本地文件", metrics)
	if err != nil {
		return err
	}
	afterID := ""
	for {
		page, err := s.repo.Artwork.ListTMDbArtworkSelectionsAfter(ctx, afterID, artworkRepairPageLimit)
		if err != nil {
			return finishArtworkTask(task, err, "TMDb 图片本地化修复失败", metrics)
		}
		if len(page) == 0 {
			break
		}
		for _, item := range page {
			metrics["scanned"]++
			detail, failed := s.repairTMDbArtworkSelection(ctx, item, metrics)
			if failed {
				metrics["failed"]++
			}
			if task != nil && detail != "" {
				task.Update(TaskUpdate{Stage: "repair", Metrics: metrics, Details: []string{detail}})
			}
			afterID = item.SelectionID
		}
		if len(page) < artworkRepairPageLimit {
			break
		}
	}
	if metrics["failed"] > 0 {
		return finishArtworkTask(task, fmt.Errorf("%d TMDb artwork repairs failed", metrics["failed"]), "TMDb 图片本地化修复完成，但存在失败", metrics)
	}
	return finishArtworkTask(task, nil, "TMDb 图片本地化修复完成", metrics)
}

func (s *ScraperService) repairTMDbArtworkSelection(ctx context.Context, item repository.TMDbArtworkSelection, metrics map[string]int64) (string, bool) {
	subject := artworkTaskSubject(item.Title, item.Kind, item.TMDbID, item.ArtworkType)
	available, err := s.artwork.tmdbSelectionFileAvailable(item)
	if err != nil {
		return "❌ " + subject + "，动作=检查本地文件，结果=失败：" + sanitizeTaskLogError(err).Error(), true
	}
	if available {
		metrics["available"]++
		return "", false
	}
	metrics["missing"]++
	if !validRemoteArtworkURL(item.SourceURL) {
		return "❌ " + subject + "，动作=按旧链接修复，结果=无效 TMDb 图片链接", true
	}
	_, updated, err := s.artwork.repairTMDbRemote(ctx, item, item.SourceURL, false)
	if err == nil {
		if !updated {
			metrics["concurrent_skipped"]++
			return "⏭️ " + subject + "，动作=按旧链接修复，结果=选择已并发变更", false
		}
		_ = s.repo.Artwork.DeleteArtworkRecheck(ctx, item.MetadataID, item.ArtworkType)
		metrics["old_url_repaired"]++
		return "✅ " + subject + "，动作=按旧链接修复，结果=已保存到本地", false
	}
	if !isRemoteImageHTTPStatus(err, http.StatusNotFound) {
		return "❌ " + subject + "，动作=按旧链接修复，结果=可重试失败：" + sanitizeTaskLogError(err).Error(), true
	}
	metrics["old_url_404"]++
	urls, err := s.tmdbArtworkURLs(ctx, item.Kind, item.TMDbID, item.SeriesTMDbID, item.SeasonNum, item.EpisodeNum)
	if err != nil {
		return "❌ " + subject + "，动作=404 后重查 TMDb，结果=可重试失败：" + sanitizeTaskLogError(err).Error(), true
	}
	newURL := strings.TrimSpace(urls[item.ArtworkType])
	if newURL == "" {
		if err := s.repo.Artwork.UpsertArtworkRecheck(ctx, item.MetadataID, item.ArtworkType, time.Now().UTC()); err != nil {
			return "❌ " + subject + "，动作=404 后重查 TMDb，结果=状态保存失败：" + sanitizeTaskLogError(err).Error(), true
		}
		metrics["still_missing"]++
		return "⚠️ " + subject + "，动作=404 后重查 TMDb，结果=TMDb 仍无图，24 小时后复查", false
	}
	_, updated, err = s.artwork.repairTMDbRemote(ctx, item, newURL, true)
	if err != nil {
		return "❌ " + subject + "，动作=使用 TMDb 新链接修复，结果=可重试失败：" + sanitizeTaskLogError(err).Error(), true
	}
	if !updated {
		metrics["concurrent_skipped"]++
		return "⏭️ " + subject + "，动作=使用 TMDb 新链接修复，结果=选择已并发变更", false
	}
	_ = s.repo.Artwork.DeleteArtworkRecheck(ctx, item.MetadataID, item.ArtworkType)
	metrics["tmdb_repaired"]++
	return "✅ " + subject + "，动作=使用 TMDb 新链接修复，结果=已保存到本地", false
}

func (s *ScraperService) runTMDbArtworkMissingRecheck(ctx context.Context, trigger string) error {
	ctx = withTMDbSeasonBatch(ctx)
	if s == nil || s.repo == nil || s.repo.Artwork == nil || s.artwork == nil {
		return errors.New("TMDb artwork missing recheck dependencies unavailable")
	}
	metrics := map[string]int64{}
	task, err := s.startArtworkTask(trigger, "TMDb 无图复查", "正在复查 TMDb 仍无图片的元数据", metrics)
	if err != nil {
		return err
	}
	afterID := ""
	now := time.Now().UTC()
	for {
		page, err := s.repo.Artwork.ListTMDbArtworkRecheckMetadataAfter(ctx, afterID, artworkRepairPageLimit)
		if err != nil {
			return finishArtworkTask(task, err, "TMDb 无图复查失败", metrics)
		}
		if len(page) == 0 {
			break
		}
		for _, item := range page {
			metrics["scanned"]++
			details, requested, failures := s.recheckTMDbArtwork(ctx, item, now, metrics)
			if requested {
				metrics["requests"]++
			}
			metrics["failed"] += failures
			if task != nil && len(details) > 0 {
				task.Update(TaskUpdate{Stage: "recheck", Metrics: metrics, Details: details})
			}
			afterID = item.MetadataID
		}
		if len(page) < artworkRepairPageLimit {
			break
		}
	}
	if metrics["failed"] > 0 {
		return finishArtworkTask(task, fmt.Errorf("%d TMDb artwork rechecks failed", metrics["failed"]), "TMDb 无图复查完成，但存在失败", metrics)
	}
	return finishArtworkTask(task, nil, "TMDb 无图复查完成", metrics)
}

func (s *ScraperService) recheckTMDbArtwork(ctx context.Context, item repository.TMDbArtworkRecheckCandidate, now time.Time, metrics map[string]int64) ([]string, bool, int64) {
	details := make([]string, 0, len(item.Types))
	due := make([]repository.TMDbArtworkRecheckType, 0, len(item.Types))
	var failures int64
	for _, typ := range item.Types {
		// 没有实体 checkpoint 时，只处理已有逐类型无图状态，避免接管首次入库刮削。
		if item.CatalogArtworkHydratedAt == nil && typ.LastNoImageAt == nil {
			continue
		}
		subject := artworkTaskSubject(item.Title, item.Kind, item.TMDbID, typ.ArtworkType)
		if typ.SelectionID != "" {
			if typ.StorageKey != "" {
				path, err := s.artwork.pathForStorageKey(typ.StorageKey)
				if err != nil {
					failures++
					details = append(details, "❌ "+subject+"，动作=检查当前选择，结果=失败："+sanitizeTaskLogError(err).Error())
					continue
				}
				available, err := localArtworkFileAvailable(path)
				if err != nil {
					failures++
					details = append(details, "❌ "+subject+"，动作=检查当前选择，结果=失败："+sanitizeTaskLogError(err).Error())
					continue
				}
				if available {
					_ = s.repo.Artwork.DeleteArtworkRecheck(ctx, item.MetadataID, typ.ArtworkType)
					metrics["existing_skipped"]++
					continue
				}
			}
			if typ.SourceProvider != "tmdb" {
				_ = s.repo.Artwork.DeleteArtworkRecheck(ctx, item.MetadataID, typ.ArtworkType)
				metrics["existing_skipped"]++
				details = append(details, "⏭️ "+subject+"，动作=检查当前选择，结果=非 TMDb 选择不处理")
				continue
			}
		}
		baseline := item.CatalogArtworkHydratedAt
		if typ.LastNoImageAt != nil {
			baseline = typ.LastNoImageAt
		}
		if baseline != nil && now.Sub(*baseline) < artworkRecheckCooldown {
			metrics["cooldown_skipped"]++
			details = append(details, "⏭️ "+subject+"，动作=检查复查冷却，结果=24 小时内不再查询")
			continue
		}
		due = append(due, typ)
	}
	if len(due) == 0 {
		return details, false, failures
	}
	urls, err := s.tmdbArtworkURLs(ctx, item.Kind, item.TMDbID, item.SeriesTMDbID, item.SeasonNum, item.EpisodeNum)
	if err != nil {
		safeErr := sanitizeTaskLogError(err)
		for _, typ := range due {
			details = append(details, "❌ "+artworkTaskSubject(item.Title, item.Kind, item.TMDbID, typ.ArtworkType)+"，动作=重查 TMDb，结果=可重试失败："+safeErr.Error())
		}
		return details, true, failures + int64(len(due))
	}
	for _, typ := range due {
		subject := artworkTaskSubject(item.Title, item.Kind, item.TMDbID, typ.ArtworkType)
		sourceURL := strings.TrimSpace(urls[typ.ArtworkType])
		if sourceURL == "" {
			if err := s.repo.Artwork.UpsertArtworkRecheck(ctx, item.MetadataID, typ.ArtworkType, now); err != nil {
				failures++
				details = append(details, "❌ "+subject+"，动作=重查 TMDb，结果=状态保存失败："+sanitizeTaskLogError(err).Error())
				continue
			}
			metrics["still_missing"]++
			details = append(details, "⚠️ "+subject+"，动作=重查 TMDb，结果=仍无图片，24 小时后再查")
			continue
		}
		if typ.SelectionID != "" {
			snapshot := repository.TMDbArtworkSelection{SelectionID: typ.SelectionID, MetadataID: item.MetadataID, ArtworkType: typ.ArtworkType, AssetID: typ.AssetID, SourceURL: typ.SourceURL}
			_, updated, err := s.artwork.repairTMDbRemote(ctx, snapshot, sourceURL, true)
			if err != nil {
				failures++
				details = append(details, "❌ "+subject+"，动作=保存复查图片，结果=可重试失败："+sanitizeTaskLogError(err).Error())
				continue
			}
			if !updated {
				metrics["concurrent_skipped"]++
				details = append(details, "⏭️ "+subject+"，动作=保存复查图片，结果=选择已并发变更")
				continue
			}
		} else {
			_, existing, err := s.artwork.importCatalogRemote(ctx, item.MetadataID, typ.ArtworkType, "tmdb", sourceURL)
			if err != nil {
				failures++
				details = append(details, "❌ "+subject+"，动作=保存复查图片，结果=可重试失败："+sanitizeTaskLogError(err).Error())
				continue
			}
			if existing {
				metrics["concurrent_skipped"]++
				details = append(details, "⏭️ "+subject+"，动作=保存复查图片，结果=已有并发选择")
				continue
			}
		}
		_ = s.repo.Artwork.DeleteArtworkRecheck(ctx, item.MetadataID, typ.ArtworkType)
		metrics["saved"]++
		details = append(details, "✅ "+subject+"，动作=保存复查图片，结果=已保存到本地")
	}
	return details, true, failures
}

func (s *ScraperService) tmdbArtworkURLs(ctx context.Context, kind, tmdbID, seriesTMDbID string, seasonNum, episodeNum int) (map[string]string, error) {
	if s.tmdb == nil {
		return nil, errors.New("TMDb provider unavailable")
	}
	id, err := strconv.Atoi(strings.TrimSpace(tmdbID))
	if err != nil || id <= 0 {
		return nil, errors.New("invalid TMDb identity")
	}
	urls := map[string]string{}
	switch kind {
	case model.MetadataKindMovie:
		match, err := s.tmdb.GetMovieMatch(ctx, id)
		if err != nil || match == nil {
			return nil, tmdbArtworkLookupError(err)
		}
		urls[model.ArtworkTypePoster], urls[model.ArtworkTypeBackdrop] = match.CatalogPosterURL, match.CatalogBackdropURL
	case model.MetadataKindSeries:
		match, err := s.tmdb.GetTVMatch(ctx, id)
		if err != nil || match == nil {
			return nil, tmdbArtworkLookupError(err)
		}
		urls[model.ArtworkTypePoster], urls[model.ArtworkTypeBackdrop] = match.CatalogPosterURL, match.CatalogBackdropURL
	case model.MetadataKindSeason, model.MetadataKindEpisode:
		seriesID, err := strconv.Atoi(strings.TrimSpace(seriesTMDbID))
		if err != nil || seriesID <= 0 {
			return nil, errors.New("invalid parent Series TMDb identity")
		}
		if kind == model.MetadataKindSeason {
			details, err := s.tmdb.GetTVSeasonDetails(ctx, seriesID, seasonNum)
			if err != nil || details == nil {
				return nil, tmdbArtworkLookupError(err)
			}
			urls[model.ArtworkTypePoster] = details.PosterURL
		} else {
			details, err := s.tmdb.GetTVEpisodeDetails(ctx, seriesID, seasonNum, episodeNum)
			if err != nil || details == nil {
				return nil, tmdbArtworkLookupError(err)
			}
			urls[model.ArtworkTypeStill] = details.CatalogStillURL
		}
	default:
		return nil, errors.New("unsupported metadata kind")
	}
	return urls, nil
}

func tmdbArtworkLookupError(err error) error {
	if err != nil {
		return err
	}
	return errors.New("TMDb details unavailable")
}

func validRemoteArtworkURL(raw string) bool {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func artworkTaskSubject(title, kind, tmdbID, artworkType string) string {
	if strings.TrimSpace(tmdbID) == "" {
		tmdbID = "-"
	}
	return fmt.Sprintf("%s，kind=%s，TMDb=%s，图片类型=%s", strings.TrimSpace(title), kind, tmdbID, artworkType)
}

func (s *ScraperService) startArtworkTask(trigger, name, message string, metrics map[string]int64) (*TaskHandle, error) {
	if s.tasks == nil {
		return nil, nil
	}
	task := s.tasks.StartTriggered(TaskKindArtwork, trigger, name, TaskUpdate{Stage: "scan", Message: message, Metrics: metrics})
	if task == nil {
		return nil, errors.New("create artwork task execution failed")
	}
	return task, nil
}

func finishArtworkTask(task *TaskHandle, err error, message string, metrics map[string]int64) error {
	if task != nil {
		safeErr := sanitizeTaskLogError(err)
		stage := "completed"
		if err != nil {
			stage = "failed"
		}
		task.Finish(safeErr, TaskUpdate{Stage: stage, Message: message, Metrics: metrics})
	}
	return err
}
