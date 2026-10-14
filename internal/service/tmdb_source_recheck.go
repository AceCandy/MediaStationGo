package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type sourceRecheckKey struct{}
type sourceRecheckSelection struct {
	kind      string
	remaining int
}

func sourceRecheckScope(ctx context.Context) *sourceRecheckSelection {
	s, _ := ctx.Value(sourceRecheckKey{}).(*sourceRecheckSelection)
	return s
}

// runTMDbSourceRechecks 复用持久坐标、季租约和 404 证据；按类型限量维护来源，不写本地展示。
func (s *ScraperService) runTMDbSourceRechecks(ctx context.Context, kind string, limit int, metrics map[string]int64, task *TaskHandle) error {
	ctx = repository.WithTMDbSourceRecheck(ctx)
	ctx = context.WithValue(ctx, sourceRecheckKey{}, &sourceRecheckSelection{kind: kind, remaining: limit})
	ctx = context.WithValue(withTMDbSeasonBatch(ctx), tmdbForceKey{}, true)
	for {
		more, _, err := s.repo.Metadata.ScanTMDbRecheckFiles(ctx)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	for {
		more, err := s.repo.Metadata.ExpandTMDbRecheckChange(ctx)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	cutoff, err := s.repo.Metadata.TMDbRecheckPassBoundary(ctx)
	if err != nil {
		return err
	}
	seasons, err := s.repo.Metadata.ListTMDbRecheckSeasonOrder(ctx, cutoff)
	if err != nil {
		return err
	}
	for _, id := range seasons {
		if sourceRecheckScope(ctx).remaining <= 0 {
			break
		}
		lease, err := s.repo.Metadata.ClaimTMDbRecheckSeasonByID(ctx, id, cutoff)
		if err != nil {
			return err
		}
		if lease == nil {
			continue
		}
		err = s.processTMDbRecheckSeason(ctx, lease, cutoff, func(local map[string]int64, details []string) {
			for k, v := range local {
				metrics[k] += v
			}
			if task != nil {
				task.Update(TaskUpdate{Stage: "recheck", Metrics: metrics, Details: details})
			}
		})
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *ScraperService) commitTMDbSourceRecheck(ctx context.Context, job *model.TMDbRecheckJob, state *repository.TMDbRecheckState, data *tmdbMetadataRecheckDetails, timing repository.TMDbRecheckTiming, metrics map[string]int64) ([]string, error) {
	started := time.Now()
	now := time.Now().UTC()
	title, _ := data.updates["title"].(string)
	overview, _ := data.updates["overview"].(string)
	if err := saveLocalizedSourceBasics(ctx, s.repo, state.Kind, data.id, title, overview); err != nil {
		return s.retryTMDbRecheck(ctx, job, metrics, recordTMDbRecheckStage(metrics, "save", "数据库保存", started, err))
	}
	// 请求已经写入共享来源，重取来源缺项状态；媒体请求坐标必须仍与发起时一致。
	current, err := s.repo.Metadata.TMDbRecheckState(ctx, state.MetadataID)
	if err != nil {
		return s.retryTMDbRecheck(ctx, job, metrics, recordTMDbRecheckStage(metrics, "save", "数据库保存", started, err))
	}
	if current == nil || !current.Playable || current.RequestIdentity() != state.RequestIdentity() {
		return s.retryTMDbRecheck(ctx, job, metrics, recordTMDbRecheckStage(metrics, "save", "数据库保存", started, repository.ErrTMDbRecheckChanged))
	}
	state = current
	var nextCheck *time.Time
	err = s.repo.Metadata.CommitTMDbRecheck(ctx, job, state, func(repos *repository.Container) error {
		// 替换来源标识仍须原有唯一身份校验；来源内容由共享中心保存。
		if err := repos.Metadata.ReplaceIdentifierWithData(ctx, state.MetadataID, "tmdb", state.Kind, strconv.Itoa(data.id), data.rawJSON, now); err != nil {
			return err
		}
		column := "tmdb_episode_checked_at"
		if state.Kind == "season" {
			column = "tmdb_season_checked_at"
		}
		if err := repos.DB.WithContext(ctx).Model(&model.MetadataItem{}).Where("id=?", state.MetadataID).UpdateColumn(column, now).Error; err != nil {
			return err
		}
		current, err := repos.Metadata.TMDbRecheckState(ctx, state.MetadataID)
		if err != nil {
			return err
		}
		var due *time.Time
		status := "done"
		if current != nil && current.NeedsData() {
			status = "pending"
			next := now.Add(timing.Cooldown(now))
			due = &next
		}
		nextCheck = due
		return repos.Metadata.FinishTMDbRecheck(ctx, job, status, "", due, 0)
	})
	if err != nil {
		return s.retryTMDbRecheck(ctx, job, metrics, recordTMDbRecheckStage(metrics, "save", "数据库保存", started, err))
	}
	recordTMDbRecheckStage(metrics, "save", "数据库保存", started, nil)
	metrics["updated"]++
	message := "来源已更新，关联媒体直接读取最新资料：" + job.MetadataID
	if nextCheck != nil {
		message += fmt.Sprintf("，来源仍有缺项，%d 天后复核", int(nextCheck.Sub(now)/(24*time.Hour)))
	}
	return []string{message}, nil
}
