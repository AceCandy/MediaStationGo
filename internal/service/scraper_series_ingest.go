package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// syncScrapeSeriesGroup 共享整剧识别结果，但每个文件仅关联自己的季集。
func (s *ScraperService) syncScrapeSeriesGroup(ctx context.Context, group scrapeCandidateGroup, fresh *model.Media, seriesID string) (resultErr error) {
	defer func() {
		if resultErr != nil && ctx.Err() == nil {
			// 共享准备阶段失败时也必须结束已领取的文件，不能留下永久 running。
			message := []rune(sanitizeTaskLogError(resultErr).Error())
			if len(message) > 1024 {
				message = message[:1024]
			}
			err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
				Where("id = ANY(?) AND scrape_status = ?", &group.MediaIDs, "running").
				Updates(map[string]any{"scrape_status": "error", "scrape_error": string(message)}).Error
			resultErr = errors.Join(resultErr, err)
		}
	}()
	series, err := s.repo.Metadata.FindByID(ctx, seriesID)
	if err != nil {
		return err
	}
	if series == nil {
		return errors.New("series metadata not found")
	}
	identifiers, err := s.repo.Metadata.ListIdentifiers(ctx, seriesID)
	if err != nil {
		return err
	}
	tmdbID := 0
	for _, id := range identifiers {
		if id.Provider == "tmdb" && id.EntityKind == model.MetadataKindSeries {
			tmdbID, _ = strconv.Atoi(id.ExternalID)
		}
	}
	// 先持久化完整补全任务，基础入库不等待单集详情、演职员或图片。
	if tmdbID > 0 && s.tmdb != nil && s.tmdb.Enabled() {
		if err := s.QueueCatalogHydrationContext(ctx, []ExternalMediaResult{{Source: "tmdb", MediaType: "tv", TMDbID: tmdbID}}); err != nil {
			return err
		}
		// 已完成的目录也可能新增本地季，重新检查子项；已有检查点仍然复用。
		if err := s.repo.DB.WithContext(ctx).Model(&model.CatalogHydrationJob{}).
			Where("provider = ? AND entity_kind = ? AND external_id = ? AND status = ?", "tmdb", model.MetadataKindSeries, strconv.Itoa(tmdbID), model.CatalogJobStatusCompleted).
			Updates(map[string]any{"status": model.CatalogJobStatusPending, "completed_at": nil, "attempts": 0}).Error; err != nil {
			return err
		}
	}
	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).Where("id = ANY(?)", &group.MediaIDs).Order("season_num, episode_num, id").Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		if err := s.repairInvalidScrapeSeason(ctx, &rows[i]); err != nil {
			return err
		}
	}
	seasons := make(map[int]*model.MetadataItem)
	seasonErrors := make(map[int]error)
	wanted := make(map[int][]int)
	for _, row := range rows {
		if row.EpisodeNum > 0 {
			wanted[row.SeasonNum] = append(wanted[row.SeasonNum], row.EpisodeNum)
		}
	}
	var failures []error
	for i := range rows {
		row := &rows[i]
		if err := ctx.Err(); err != nil {
			return err
		}
		var bindErr error
		// 同一已确认的 TMDB 整剧以在线编号为准；其他分组仍保留完整冲突检查。
		sameTMDb := series.Source == "tmdb" && tmdbID > 0 && row.TMDbID == tmdbID && fresh.TMDbID == tmdbID
		if row.TMDbID > 0 && fresh.TMDbID > 0 && row.TMDbID != fresh.TMDbID ||
			!sameTMDb && (row.DoubanID != "" && fresh.DoubanID != "" && row.DoubanID != fresh.DoubanID ||
				row.BangumiID > 0 && fresh.BangumiID > 0 && row.BangumiID != fresh.BangumiID ||
				row.TheTVDBID != "" && fresh.TheTVDBID != "" && row.TheTVDBID != fresh.TheTVDBID) {
			bindErr = errors.New("series group has conflicting provider identifiers")
		}
		targetID := seriesID
		if bindErr == nil && row.EpisodeNum > 0 {
			if row.SeasonNum < 0 {
				bindErr = errors.New("episode season number is unknown")
			} else {
				season, loaded := seasons[row.SeasonNum]
				if !loaded {
					season, bindErr = s.upsertSeasonMetadata(ctx, series, row.SeasonNum, series.Source)
					if bindErr == nil && tmdbID > 0 && s.tmdb != nil && s.tmdb.Enabled() {
						bindErr = s.ingestSeasonInventory(ctx, season, tmdbID, wanted[row.SeasonNum]...)
					}
					seasons[row.SeasonNum], seasonErrors[row.SeasonNum] = season, bindErr
				}
				bindErr = seasonErrors[row.SeasonNum]
				if bindErr == nil {
					episode, err := s.repo.Metadata.FindEpisode(ctx, seriesID, row.SeasonNum, row.EpisodeNum)
					bindErr = err
					if bindErr == nil && episode == nil {
						// 季清单可能滞后于本地文件；按季集号占位，不伪造单集 provider 标识或快照。
						episode, bindErr = s.repo.Metadata.UpsertEpisode(ctx, &model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: row.EpisodeNum, Title: preferredTMDbEntityTitle(row.EpisodeTitle, nil, model.MetadataKindEpisode, row.EpisodeNum), Source: series.Source})
					}
					if bindErr == nil {
						targetID = episode.ID
					}
				}
			}
		}
		if bindErr != nil {
			failures = append(failures, s.markScrapeError(ctx, row.ID, bindErr))
			continue
		}
		lookup := *fresh
		if sameTMDb {
			if lookup.TheTVDBID == "" {
				lookup.TheTVDBID = row.TheTVDBID
			}
			if lookup.DoubanID == "" {
				lookup.DoubanID = row.DoubanID
			}
			if lookup.BangumiID == 0 {
				lookup.BangumiID = row.BangumiID
			}
		}
		lookup.SeriesID = row.SeriesID
		if err := s.markMetadataMatched(ctx, row, &lookup, targetID); err != nil {
			failures = append(failures, s.markScrapeError(ctx, row.ID, err))
		}
	}
	return errors.Join(failures...)
}

// repairInvalidScrapeSeason 让重试入口也能纠正旧负数季号，未知文件名不猜测。
func (s *ScraperService) repairInvalidScrapeSeason(ctx context.Context, media *model.Media) error {
	if media.SeasonNum >= 0 {
		return nil
	}
	season, episode := parseStandardEpisode(media.Path)
	if episode <= 0 {
		return nil
	}
	result := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("id = ? AND path = ? AND season_num = ? AND episode_num = ?", media.ID, media.Path, media.SeasonNum, media.EpisodeNum).
		Updates(map[string]any{"season_num": season, "episode_num": episode})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("media coordinates changed during scrape; retry required")
	}
	media.SeasonNum, media.EpisodeNum = season, episode
	return nil
}

// ingestSeasonInventory 每季读取一次快照或网络响应，只填基础信息，不推进完整补全检查点。
func (s *ScraperService) ingestSeasonInventory(ctx context.Context, season *model.MetadataItem, tmdbID int, wanted ...int) error {
	snapshot, err := s.repo.Metadata.FindProviderSnapshot(ctx, season.ID, "tmdb")
	if err != nil {
		return err
	}
	var details *TMDbSeasonDetails
	if snapshot != nil {
		details, err = s.tmdb.parseTVSeasonDetails([]byte(snapshot.Payload))
		if err != nil {
			return err
		}
		available := make(map[int]bool, len(details.Episodes))
		for _, episode := range details.Episodes {
			available[episode.EpisodeNumber] = true
		}
		for _, number := range wanted {
			if !available[number] {
				// 新入库集未包含在旧快照中时，每季重新请求一次。
				snapshot = nil
				break
			}
		}
	}
	if snapshot == nil {
		details, err = s.tmdb.GetTVSeasonDetails(ctx, tmdbID, season.SeasonNum)
	}
	if err != nil {
		return err
	}
	if details == nil || details.ID <= 0 || details.SeasonNumber != season.SeasonNum {
		return fmt.Errorf("invalid season inventory for season %d", season.SeasonNum)
	}
	if snapshot == nil {
		if err := s.repo.Metadata.UpsertProviderSnapshot(ctx, season.ID, "tmdb", details.RawJSON, time.Now().UTC()); err != nil {
			return err
		}
	}
	if season.Source == "tmdb" && season.CatalogMetadataHydratedAt == nil {
		item := catalogSeasonItem(*season.ParentID, details)
		preserveMissingLocalEpisodeDetails(item, season)
		if _, err := s.repo.Metadata.UpsertSeasonWithIdentifiers(ctx, item, catalogIdentifiers(model.MetadataKindSeason, details.ID, details.ExternalIDs)); err != nil {
			return err
		}
	}
	for _, summary := range details.Episodes {
		if _, err := s.upsertCatalogEpisodeShell(ctx, season, summary); err != nil {
			return err
		}
	}
	if season.CatalogHydratedAt != nil {
		child, err := s.repo.Metadata.FindIncompleteCatalogChild(ctx, season.ID, model.MetadataKindEpisode)
		if err != nil {
			return err
		}
		if child != nil {
			return s.repo.DB.WithContext(ctx).Model(&model.MetadataItem{}).
				Where("id IN ?", []string{season.ID, *season.ParentID}).Update("catalog_hydrated_at", nil).Error
		}
	}
	return nil
}
