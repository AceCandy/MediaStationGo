package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	artworkBackfillPageLimit    = 200
	artworkBackfillScanLimit    = 1000
	artworkBackfillEnqueueLimit = 200
	artworkIntegrityCursorKey   = "internal.metadata_artwork_integrity_cursor"
)

func (s *ScraperService) runMetadataArtworkBackfill(ctx context.Context, trigger string) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.repo.Artwork == nil || s.repo.Setting == nil || s.artwork == nil {
		return errors.New("artwork backfill dependencies unavailable")
	}
	metrics := map[string]int64{}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(TaskKindArtwork, trigger, "元数据图片补齐", TaskUpdate{
			Stage: "discover", Message: "正在查找缺少 TMDb 图片的元数据", Metrics: metrics,
		})
		if task == nil {
			return errors.New("create artwork backfill task execution failed")
		}
	}
	if err := s.scanSelectedArtworkAssets(ctx, metrics); err != nil {
		if task != nil {
			task.Finish(err, TaskUpdate{Stage: "failed", Message: "本地图片完整性巡检失败", Metrics: metrics})
		}
		return err
	}
	manual := trigger == TaskTriggerManual
	afterID := ""
	queued := int64(0)
	for metrics["roots_scanned"] < artworkBackfillScanLimit && queued < artworkBackfillEnqueueLimit {
		page, err := s.repo.Metadata.ListMissingCatalogArtworkRootsAfter(ctx, afterID, artworkBackfillPageLimit, manual)
		if err != nil {
			if task != nil {
				task.Finish(err, TaskUpdate{Stage: "failed", Message: "元数据图片补齐巡检失败", Metrics: metrics})
			}
			return err
		}
		if len(page) == 0 {
			break
		}
		for _, candidate := range page {
			metrics["roots_scanned"]++
			metrics["missing_roots"]++
			result, err := s.repo.Metadata.EnsureCatalogArtworkJob(ctx, candidate, manual)
			if err != nil {
				if task != nil {
					task.Finish(err, TaskUpdate{Stage: "failed", Message: "元数据图片补齐排队失败", Metrics: metrics})
				}
				return err
			}
			switch result {
			case repository.CatalogJobCreated:
				metrics["jobs_created"]++
				queued++
			case repository.CatalogJobRequeued:
				metrics["jobs_requeued"]++
				queued++
			default:
				metrics["jobs_unchanged"]++
			}
			afterID = candidate.MetadataID
			if metrics["roots_scanned"] >= artworkBackfillScanLimit || queued >= artworkBackfillEnqueueLimit {
				break
			}
		}
		if len(page) < artworkBackfillPageLimit {
			break
		}
	}
	if queued > 0 {
		s.wakeCatalogHydration()
	}
	if task != nil {
		task.Finish(nil, TaskUpdate{
			Stage: "completed", Message: "元数据图片补齐巡检完成", Metrics: metrics,
			Details: []string{fmt.Sprintf("ℹ️ 检查 %d 个本地资产，缺失 %d，清除选择 %d；扫描 %d 个缺图根，新增 %d，重排 %d，保持 %d", metrics["assets_scanned"], metrics["assets_missing"], metrics["selections_invalidated"], metrics["roots_scanned"], metrics["jobs_created"], metrics["jobs_requeued"], metrics["jobs_unchanged"])},
		})
	}
	return nil
}

func (s *ScraperService) scanSelectedArtworkAssets(ctx context.Context, metrics map[string]int64) error {
	afterID, err := s.repo.Setting.Get(ctx, artworkIntegrityCursorKey)
	if err != nil {
		return err
	}
	for metrics["assets_scanned"] < artworkBackfillScanLimit {
		limit := min(artworkBackfillPageLimit, artworkBackfillScanLimit-int(metrics["assets_scanned"]))
		assets, err := s.repo.Artwork.ListSelectedAssetsAfter(ctx, afterID, limit)
		if err != nil {
			return err
		}
		if len(assets) == 0 {
			return s.repo.Setting.Set(ctx, artworkIntegrityCursorKey, "")
		}
		for i := range assets {
			metrics["assets_scanned"]++
			missing, invalidated, err := s.artwork.invalidateMissingLocalAsset(ctx, &assets[i])
			if err != nil {
				return fmt.Errorf("check local artwork asset %s: %w", assets[i].ID, err)
			}
			if missing {
				metrics["assets_missing"]++
				metrics["selections_invalidated"] += invalidated
			}
			afterID = assets[i].ID
		}
		if err := s.repo.Setting.Set(ctx, artworkIntegrityCursorKey, afterID); err != nil {
			return err
		}
		if len(assets) < limit {
			return s.repo.Setting.Set(ctx, artworkIntegrityCursorKey, "")
		}
	}
	return nil
}
