package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	TaskKindTMDbPeopleSupplement   = "tmdb_data_people"
	TaskKindTMDbMovieSupplement    = "tmdb_data_movie"
	TaskKindTMDbSeriesSupplement   = "tmdb_data_series"
	TaskKindTMDbSeasonSupplement   = "tmdb_data_season"
	TaskKindTMDbEpisodeSupplement  = "tmdb_data_episode"
	TaskKindTMDbInventory          = "tmdb_data_inventory"
	TaskKindTMDbImages             = "tmdb_data_images"
	TaskKindDoubanMovieSupplement  = "douban_data_movie"
	TaskKindDoubanSeriesSupplement = "douban_data_series"
	TaskKindDoubanMovieRating      = "douban_data_movie_rating"
	TaskKindDoubanSeriesRating     = "douban_data_series_rating"
	TaskKindDoubanImages           = "douban_data_images"
)

type catalogMaintenanceTask struct {
	key, name, kind   string
	rating, inventory bool
	people            bool
}

var catalogMaintenanceTasks = []catalogMaintenanceTask{
	{key: TaskKindTMDbPeopleSupplement, name: "TMDb 演职员与人物资料补全", people: true},
	{key: TaskKindTMDbMovieSupplement, name: "TMDb 电影资料补全", kind: "movie"},
	{key: TaskKindTMDbSeriesSupplement, name: "TMDb 电视剧资料补全", kind: "series"},
	{key: TaskKindTMDbSeasonSupplement, name: "TMDb 季资料补全", kind: "season"},
	{key: TaskKindTMDbEpisodeSupplement, name: "TMDb 集资料补全/复查", kind: "episode"},
	{key: TaskKindTMDbInventory, name: "TMDb 在播季集目录检查", inventory: true},
	{key: TaskKindDoubanMovieSupplement, name: "豆瓣电影资料补全", kind: "movie"},
	{key: TaskKindDoubanSeriesSupplement, name: "豆瓣电视剧资料补全", kind: "series"},
	{key: TaskKindDoubanMovieRating, name: "豆瓣电影评分更新", kind: "movie", rating: true},
	{key: TaskKindDoubanSeriesRating, name: "豆瓣电视剧评分更新", kind: "series", rating: true},
}

func (s *SchedulerService) runCatalogMaintenance(ctx context.Context, spec catalogMaintenanceTask) error {
	value, err := s.repo.Setting.Get(ctx, "metadata."+spec.key+"_count")
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(value)
	maxCount := 100
	if model.TaskSystemForKind(spec.key) == model.TaskSystemTMDb {
		maxCount = 1000
	}
	if limit < 1 || limit > maxCount {
		limit = 100
	}
	metrics := map[string]int64{"limit": int64(limit)}
	var task *TaskHandle
	if s.tasks != nil {
		task = s.tasks.StartTriggered(spec.key, schedulerTaskTrigger(ctx), spec.name, TaskUpdate{Stage: "refresh", Message: "正在处理到期的来源待办", Metrics: metrics})
		if task == nil {
			return errors.New("create source maintenance task failed")
		}
	}
	if model.TaskSystemForKind(spec.key) == model.TaskSystemTMDb {
		if s.tmdbCenter == nil {
			err = errors.New("TMDb center unavailable")
		} else {
			budget := &tmdbBudget{limit: int64(limit)}
			ctx = context.WithValue(ctx, tmdbBudgetKey{}, budget)
			ctx = context.WithValue(ctx, tmdbAutomaticKey{}, true)
			if spec.people {
				err = s.tmdbCenter.maintainPeople(ctx, limit, metrics, task)
			} else if spec.kind == "season" || spec.kind == "episode" {
				if s.scraper == nil {
					err = errors.New("TMDb source recheck unavailable")
				} else {
					err = s.scraper.runTMDbSourceRechecks(ctx, spec.kind, limit, metrics, task)
				}
			}
			if err == nil && !spec.people && spec.kind != "episode" && budget.used.Load() < int64(limit) {
				err = s.tmdbCenter.maintainSubjects(ctx, spec, limit, metrics)
			}
			metrics["requested"] = budget.used.Load()
		}
	} else if s.scraper == nil || s.scraper.douban == nil {
		err = errors.New("Douban center unavailable")
	} else {
		err = s.scraper.douban.maintainSubjects(ctx, spec, limit, metrics, task)
	}
	if errors.Is(err, errTMDbBudget) {
		err = nil
	}
	if task != nil {
		stage := "completed"
		if err != nil {
			stage = "failed"
		}
		task.Finish(sanitizeTaskLogError(err), TaskUpdate{Stage: stage, Message: "本轮来源维护结束，剩余待办下轮继续", Metrics: metrics})
	}
	return err
}

func (s *TMDbDataCenter) maintainSubjects(ctx context.Context, spec catalogMaintenanceTask, limit int, metrics map[string]int64) error {
	rows, err := s.repo.TMDb.MaintenanceCandidates(ctx, spec.kind, spec.inventory, limit)
	if err != nil {
		return err
	}
	metrics["selected"] += int64(len(rows))
	ctx = withTMDbSeasonBatch(ctx)
	ctx = context.WithValue(ctx, tmdbForceKey{}, true)
	var batchErr error
	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var title, overview string
		if spec.inventory {
			err = s.refreshInventory(ctx, row)
		} else if row.Kind == "movie" || row.Kind == "series" {
			var match *Match
			if row.Kind == "movie" {
				match, err = s.provider.GetMovieMatch(ctx, row.TMDbID)
			} else {
				match, err = s.provider.GetTVMatch(ctx, row.TMDbID)
			}
			if err == nil {
				if match == nil {
					err = errors.New("TMDb 未返回资料，请检查来源配置")
				} else {
					title, overview = match.Title, match.Overview
				}
			}
		} else {
			var parent *model.TMDbSubject
			if row.ParentID == nil || row.Number == nil {
				return ErrTMDbRefreshIdentity
			}
			parent, err = s.repo.TMDb.Subject(ctx, *row.ParentID)
			if err == nil {
				var season *TMDbSeasonDetails
				season, err = s.provider.GetTVSeasonDetails(ctx, parent.TMDbID, *row.Number)
				if err == nil && season == nil {
					err = errors.New("TMDb 未返回季资料，请检查来源配置")
				}
				if err == nil {
					title, overview = season.Name, season.Overview
				}
			}
		}
		if errors.Is(err, errTMDbBudget) || errors.Is(err, context.Canceled) {
			return err
		}
		metrics["processed"]++
		delay := repository.TMDbMaintenanceDelay(&row, time.Now())
		if err != nil {
			metrics["failed"]++
			delay = 5 * time.Minute
			batchErr = errors.New("部分来源补全失败，已保留旧资料")
		} else {
			metrics["updated"]++
			if !spec.inventory {
				if e := saveLocalizedSourceBasics(ctx, s.repo, row.Kind, row.TMDbID, title, overview); e != nil {
					return e
				}
			}
		}
		if e := s.repo.TMDb.FinishMaintenance(ctx, &row, spec.inventory, time.Now().Add(delay)); e != nil {
			return e
		}
		if isTMDbHTTPStatus(err, 401) || isTMDbHTTPStatus(err, 403) || isTMDbHTTPStatus(err, 429) || errors.Is(err, errTMDbDeferred) {
			return err
		}
	}
	return batchErr
}

// refreshInventory 只请求基础目录，使用独立组件避免覆盖正文缓存或复用正文请求。
func (s *TMDbDataCenter) refreshInventory(ctx context.Context, row model.TMDbSubject) error {
	apiKey := s.provider.resolveAPIKey(ctx)
	if apiKey == "" {
		return errors.New("TMDb 未配置来源凭据")
	}
	path := fmt.Sprintf("/tv/%d", row.TMDbID)
	if row.Kind == "season" {
		if row.ParentID == nil || row.Number == nil {
			return ErrTMDbRefreshIdentity
		}
		parent, err := s.repo.TMDb.Subject(ctx, *row.ParentID)
		if err != nil {
			return err
		}
		if parent == nil {
			return ErrTMDbRefreshIdentity
		}
		path = fmt.Sprintf("/tv/%d/season/%d", parent.TMDbID, *row.Number)
	}
	query := url.Values{"api_key": {apiKey}, "language": {"zh-CN"}}
	ctx = context.WithValue(ctx, tmdbInventoryKey{}, true)
	ctx = context.WithValue(ctx, tmdbAutomaticKey{}, true)
	_, err := s.Fetch(ctx, s.provider.resolveBaseURL(ctx)+path+"?"+query.Encode(), &json.RawMessage{})
	return err
}

func (d *DoubanProvider) maintainSubjects(ctx context.Context, spec catalogMaintenanceTask, limit int, metrics map[string]int64, task *TaskHandle) error {
	ctx = withDoubanTaskRouteReset(ctx)
	ctx, requests := withDoubanRequestStats(ctx)
	defer func() { metrics["requested"] = requests.Load() }()
	now := time.Now().UTC()
	rows, err := d.dataRepo.Douban.MaintenanceCandidates(ctx, spec.kind, spec.rating, now.Add(-d.detailTTL(ctx)), now, limit)
	if err != nil {
		return err
	}
	metrics["selected"] = int64(len(rows))
	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		metrics["processed"]++
		var saved bool
		if spec.rating {
			saved, err = d.refreshRating(ctx, row.DoubanID, row.EntityKind)
		} else {
			var outcome doubanDetailObservation
			outcome, err = d.refreshBaseDetail(ctx, row.DoubanID, row.EntityKind)
			saved = outcome.Saved
		}
		if err != nil {
			metrics["failed"]++
			return err
		}
		if saved {
			metrics["updated"]++
		} else {
			metrics["skipped"]++
		}
		if task != nil {
			task.Update(TaskUpdate{Stage: "refresh", Metrics: metrics, Details: []string{fmt.Sprintf("豆瓣 %s：已处理", row.DoubanID)}})
		}
	}
	return nil
}

// refreshRating 与基础资料共用准入锁，锁内复核独立评分时间，避免重复请求和迟到覆盖。
func (d *DoubanProvider) refreshRating(ctx context.Context, id, kind string) (bool, error) {
	release, err := d.enterDataRequest(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	row, err := d.dataRepo.Douban.Find(ctx, id, kind)
	if err != nil || row == nil {
		return false, err
	}
	now := time.Now().UTC()
	if row.RatingCheckedAt != nil && row.RatingCheckedAt.Add(d.detailTTL(ctx)).After(now) || row.RatingRetryAt != nil && row.RatingRetryAt.After(now) || row.NextRetryAt != nil && row.NextRetryAt.After(now) {
		return false, nil
	}
	raw, _, err := d.getRexxarDetailRawJSON(ctx, id, kind)
	if err == nil {
		err = validateDoubanCacheIdentity(raw, id, kind)
	}
	if err != nil {
		if e := d.dataRepo.Douban.FailRating(ctx, row, now.Add(doubanFailureDelay(err))); e != nil {
			return false, e
		}
		return false, err
	}
	saved, err := d.dataRepo.Douban.SaveRating(ctx, row, raw, time.Now().UTC())
	if saved && d.cache != nil {
		d.cache.DeletePrefix(ctx, "media:")
		d.cache.DeletePrefix(ctx, embyItemsCachePrefix)
	}
	return saved, err
}

// saveLocalizedSourceBasics 将解析得到的中文回退写入共享投影，不修改原始响应。
func saveLocalizedSourceBasics(ctx context.Context, repos *repository.Container, kind string, id int, title, overview string) error {
	if model.HasChineseText(title) && (kind != "episode" || !tmdbEntityTitleIsGenerated(title, kind)) {
		if err := repos.DB.WithContext(ctx).Model(&model.TMDbSubject{}).Where("kind=? AND tmdb_id=?", kind, id).
			Where("title !~ '[一-鿿]' OR title ~ '[ぁ-ヿ가-힣]' OR (kind='episode' AND title ~ '^第[[:space:]]*[0-9]+[[:space:]]*(集|话|話)$')").Update("title", title).Error; err != nil {
			return err
		}
	}
	if overview != "" {
		return repos.DB.WithContext(ctx).Model(&model.TMDbSubject{}).Where("kind=? AND tmdb_id=? AND btrim(overview)=''", kind, id).Update("overview", overview).Error
	}
	return nil
}
