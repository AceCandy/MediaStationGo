package service

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type sourceImagePendingKey struct{}

// sourceImageWorker 恢复新来源图片及失败待办；完整资产的巡检仍由定时开关控制。
func (s *SchedulerService) sourceImageWorker(ctx context.Context) {
	if s.repo == nil {
		return
	}
	ctx = context.WithValue(ctx, sourceImagePendingKey{}, true)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, kind := range []string{TaskKindTMDbImages, TaskKindDoubanImages} {
				var pending bool
				query := `SELECT EXISTS(SELECT 1 FROM douban_subjects WHERE (payload IS NOT NULL OR summary IS NOT NULL) AND poster_checked_at IS NULL AND (poster_retry_at IS NULL OR poster_retry_at<=now()))`
				if kind == TaskKindTMDbImages {
					query = `SELECT EXISTS(SELECT 1 FROM tmdb_artworks WHERE source_path<>'' AND (retry_at IS NULL OR retry_at<=now()) AND (checked_at IS NULL OR downloaded_path<>source_path OR status='error')) OR EXISTS(SELECT 1 FROM tmdb_person_profiles WHERE profile_path<>'' AND (image_retry_at IS NULL OR image_retry_at<=now()) AND (image_checked_at IS NULL OR image_source_path<>profile_path OR image_status='error'))`
				}
				if err := s.repo.DB.WithContext(ctx).Raw(query).Scan(&pending).Error; err == nil && pending {
					_ = s.runSourceImages(ctx, kind)
				}
			}
		}
	}
}

// runSourceImages 只维护来源图片，缺少远程路径时由资料补全任务处理。
func (s *SchedulerService) runSourceImages(ctx context.Context, kind string) error {
	metrics := map[string]int64{}
	var task *TaskHandle
	trigger := schedulerTaskTrigger(ctx)
	if pending, _ := ctx.Value(sourceImagePendingKey{}).(bool); pending {
		trigger = TaskTriggerEvent
	}
	if s.tasks != nil {
		task = s.tasks.StartTriggeredIfKindIdle(kind, trigger, map[string]string{TaskKindTMDbImages: "TMDb 图片下载与修复", TaskKindDoubanImages: "豆瓣图片下载与修复"}[kind], TaskUpdate{Stage: "images", Message: "检查来源图片，下载缺图并修复失效资产"})
		if task == nil {
			return ErrSchedulerJobAlreadyRunning
		}
	}
	err := s.maintainSourceImages(ctx, kind, metrics)
	if task != nil {
		stage := "completed"
		if err != nil {
			stage = "failed"
		}
		task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: stage, Message: "本轮图片维护结束", Metrics: metrics})
	}
	return err
}

func (s *SchedulerService) maintainSourceImages(ctx context.Context, kind string, metrics map[string]int64) error {
	now := time.Now().UTC()
	before := now.Add(-7 * 24 * time.Hour)
	if pending, _ := ctx.Value(sourceImagePendingKey{}).(bool); pending {
		before = time.Unix(0, 0)
	}
	var failures error
	if kind == TaskKindTMDbImages {
		if s.tmdbCenter == nil || s.tmdbCenter.artwork == nil {
			return errors.New("TMDb image store unavailable")
		}
		var rows []model.TMDbArtwork
		if err := s.repo.DB.WithContext(ctx).Where("source_path<>'' AND (retry_at IS NULL OR retry_at<=?) AND (checked_at IS NULL OR checked_at<=? OR downloaded_path<>source_path OR status='error')", now, before).Order("checked_at NULLS FIRST,id").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			row := &rows[i]
			_, err := s.tmdbCenter.ensureSubjectArtwork(ctx, row, false)
			metrics["processed"]++
			if err != nil || row.Status == "error" {
				metrics["failed"]++
				failures = errors.New("部分图片下载失败，已保留旧图并延期重试")
			}
			if err := s.repo.DB.WithContext(ctx).Model(row).Where("source_path=?", row.SourcePath).UpdateColumn("checked_at", now).Error; err != nil {
				return err
			}
		}
		if s.tmdbCenter.people == nil {
			return failures
		}
		var people []model.TMDbPersonProfile
		if err := s.repo.DB.WithContext(ctx).Where("profile_path<>'' AND (image_retry_at IS NULL OR image_retry_at<=?) AND (image_checked_at IS NULL OR image_checked_at<=? OR image_source_path<>profile_path OR image_status='error')", now, before).Order("image_checked_at NULLS FIRST,id").Limit(100 - len(rows)).Find(&people).Error; err != nil {
			return err
		}
		for i := range people {
			p := &people[i]
			err := s.tmdbCenter.ensurePersonImage(ctx, p, false)
			metrics["processed"]++
			if err != nil || p.ImageStatus == "error" {
				metrics["failed"]++
				failures = errors.New("部分人物图片下载失败，已延期重试")
			}
			if err := s.repo.DB.WithContext(ctx).Model(p).Where("profile_path=?", p.ProfilePath).UpdateColumn("image_checked_at", now).Error; err != nil {
				return err
			}
		}
		return failures
	}
	if s.scraper == nil || s.scraper.douban == nil || s.scraper.artwork == nil {
		return errors.New("Douban image store unavailable")
	}
	var rows []model.DoubanSubject
	if err := s.repo.DB.WithContext(ctx).Where("(payload IS NOT NULL OR summary IS NOT NULL) AND (poster_retry_at IS NULL OR poster_retry_at<=?) AND (poster_checked_at IS NULL OR poster_checked_at<=?)", now, before).Order("poster_checked_at NULLS FIRST,id").Limit(100).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		updates := map[string]any{"poster_checked_at": now, "poster_retry_at": nil}
		if s.scraper.douban.posterSource(ctx, &row) != "" {
			_, cache, err := s.scraper.douban.subjectPoster(ctx, row.ID, s.scraper.artwork)
			metrics["processed"]++
			if err != nil || cache == "no-store" {
				metrics["failed"]++
				updates["poster_retry_at"] = now.Add(time.Hour)
				updates["poster_checked_at"] = nil
				failures = errors.New("部分豆瓣海报下载失败，已保留旧图并延期重试")
			}
		}
		if err := s.repo.DB.WithContext(ctx).Model(&model.DoubanSubject{}).Where("id=? AND payload IS NOT DISTINCT FROM ?::jsonb", row.ID, row.Payload).UpdateColumns(updates).Error; err != nil {
			return err
		}
	}
	return failures
}
