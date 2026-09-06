package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const catalogSnapshotLocalizationBatchSize = 200

const seriesLocalCorrectionVersion = "1"
const seriesLocalCorrectionVersionKey = "internal.series_local_correction_version"

var ErrSeriesLocalCorrectionRunning = errors.New("series local correction already running")
var ErrSeriesLocalCorrectionUnavailable = errors.New("series local correction unavailable")

// StartSeriesLocalCorrection 按规则版本启动本地纠正；手动执行忽略完成标记。
func (s *ScraperService) StartSeriesLocalCorrection(ctx context.Context, automatic bool) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil || s.repo.Setting == nil || s.tasks == nil {
		return ErrSeriesLocalCorrectionUnavailable
	}
	if automatic {
		version, err := s.repo.Setting.Get(ctx, seriesLocalCorrectionVersionKey)
		if err != nil {
			return err
		}
		if version == seriesLocalCorrectionVersion {
			return nil
		}
	}
	trigger := TaskTriggerManual
	if automatic {
		trigger = TaskTriggerEvent
	}
	task := s.tasks.StartTriggeredIfKindIdle(TaskKindSeriesLocalCorrection, trigger, "剧集本地资料纠正", TaskUpdate{Stage: "prepare", Message: "正在统计本地季与集快照"})
	if task == nil {
		if s.tasks.IsKindRunning(TaskKindSeriesLocalCorrection) {
			return ErrSeriesLocalCorrectionRunning
		}
		return errors.New("create local correction task failed")
	}
	s.catalogHydrationWG.Add(1)
	go func() {
		defer s.catalogHydrationWG.Done()
		err := s.localizeTMDbCatalogSnapshots(ctx, task)
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			err = s.repo.Setting.Set(ctx, seriesLocalCorrectionVersionKey, seriesLocalCorrectionVersion)
		}
		message := "剧集本地资料纠正完成"
		if err != nil {
			message = "剧集本地资料纠正未完成，可重试"
		}
		safeErr := sanitizeTaskLogError(err)
		if errors.Is(err, context.Canceled) {
			safeErr = context.Canceled
		}
		task.Finish(safeErr, TaskUpdate{Stage: "completed", Message: message})
	}()
	return nil
}

type tmdbCatalogLocalizedSnapshot struct {
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Translations struct {
		Translations []tmdbTranslation `json:"translations"`
	} `json:"translations"`
}

func (s *ScraperService) localizeTMDbCatalogSnapshots(ctx context.Context, task *TaskHandle) error {
	if s == nil || s.repo == nil || s.repo.Metadata == nil {
		return nil
	}
	total, err := s.repo.Metadata.CountLocalCorrectionSnapshots(ctx)
	if err != nil {
		return err
	}
	metrics := map[string]int64{"total": total, "processed": 0, "succeeded": 0, "failed": 0, "remaining": total, "updated": 0, "skipped": 0}
	progress := func(details []string) {
		task.Update(TaskUpdate{Stage: "correct", Message: fmt.Sprintf("已检查 %d / %d，纠正 %d，跳过 %d，失败 %d", metrics["processed"], metrics["total"], metrics["updated"], metrics["skipped"], metrics["failed"]), Metrics: metrics, Details: details})
	}
	progress(nil)
	afterID := ""
	for {
		snapshots, err := s.repo.Metadata.ListProviderSnapshotsAfter(ctx, "tmdb", []string{model.MetadataKindSeason, model.MetadataKindEpisode}, afterID, catalogSnapshotLocalizationBatchSize)
		if err != nil {
			return err
		}
		var details []string
		for i := range snapshots {
			updated, err := s.localizeTMDbCatalogSnapshot(ctx, &snapshots[i])
			metrics["processed"]++
			metrics["total"] = max(metrics["total"], metrics["processed"])
			metrics["remaining"] = metrics["total"] - metrics["processed"]
			if err != nil {
				if ctx.Err() != nil {
					progress(details)
					return ctx.Err()
				}
				metrics["failed"]++
				details = append(details, fmt.Sprintf("❌ 元数据 %s：%v", snapshots[i].MetadataID, sanitizeTaskLogError(err)))
			} else {
				metrics["succeeded"]++
				if updated {
					metrics["updated"]++
				} else {
					metrics["skipped"]++
				}
			}
		}
		progress(details)
		if len(snapshots) < catalogSnapshotLocalizationBatchSize {
			metrics["total"] = metrics["processed"]
			metrics["remaining"] = 0
			progress(nil)
			if metrics["failed"] > 0 {
				return fmt.Errorf("%d local correction items failed", metrics["failed"])
			}
			return nil
		}
		afterID = snapshots[len(snapshots)-1].ID
	}
}

func (s *ScraperService) localizeTMDbCatalogSnapshot(ctx context.Context, snapshot *model.MetadataProviderSnapshot) (bool, error) {
	if snapshot == nil || snapshot.Metadata.ID == "" || snapshot.Metadata.Source != "tmdb" {
		return false, nil
	}
	var payload tmdbCatalogLocalizedSnapshot
	if err := json.Unmarshal([]byte(snapshot.Payload), &payload); err != nil {
		return false, fmt.Errorf("decode tmdb %s snapshot %s: %w", snapshot.Metadata.Kind, snapshot.MetadataID, err)
	}
	item := &snapshot.Metadata
	number := item.SeasonNum
	if item.Kind == model.MetadataKindEpisode {
		number = item.EpisodeNum
	}
	title := preferredTMDbEntityTitle(payload.Name, payload.Translations.Translations, item.Kind, number)
	overview := preferredTMDbEntityOverview(payload.Overview, payload.Translations.Translations)
	changed := false
	if item.Title != title {
		item.Title = title
		changed = true
	}
	if item.Kind == model.MetadataKindEpisode && item.OriginalName != "" {
		item.OriginalName = ""
		changed = true
	}
	if item.Overview != overview {
		item.Overview = overview
		changed = true
	}
	if !changed {
		return false, nil
	}
	return s.repo.Metadata.UpdateLocalizedMetadata(ctx, item)
}
