package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	TaskKindHongGuoSync    = "hongguo_sync"
	TaskKindHongGuoRefresh = "hongguo_refresh"
	TaskKindHongGuoArtwork = "hongguo_artwork"
	TaskKindHongGuoAlbum   = "hongguo_album"
	hongGuoRetryDelay      = time.Hour
	hongGuoNotFoundDelay   = 72 * time.Hour
)

var ErrHongGuoRunning = errors.New("红果任务正在运行")
var ErrHongGuoDisabled = errors.New("HongGuoDB 已停用")
var errHongGuoCheckpoint = errors.New("红果同步检查点保存失败")

// HongGuoService 持有来源任务互斥与取消，复用公共执行日志，不写旧资料表。
type HongGuoService struct {
	repo              *repository.Container
	client            *hongguo.Client
	tasks             *TaskTrackerService
	images            *ImageProxy
	imageRoot         string
	runMu             sync.Mutex
	artworkMu         sync.Mutex
	discoveryMu       sync.Mutex
	mu                sync.Mutex
	cancel            context.CancelFunc
	artworkCancel     context.CancelFunc
	discoveryCancel   context.CancelFunc
	closed            bool
	refreshRequested  bool
	autoRefreshCancel context.CancelFunc
}

func NewHongGuoService(repo *repository.Container, tasks *TaskTrackerService, images *ImageProxy, dataDir string) *HongGuoService {
	return &HongGuoService{repo: repo, tasks: tasks, client: hongguo.NewClient(nil), images: images, imageRoot: filepath.Join(dataDir, "catalogs", "hongguo", "artwork")}
}

func (s *HongGuoService) Enabled(ctx context.Context) (bool, error) {
	value, err := s.repo.Setting.Get(ctx, "hongguo.enabled")
	return value != "false", err
}

func (s *HongGuoService) SetEnabled(ctx context.Context, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	if err := s.repo.Setting.Set(ctx, "hongguo.enabled", value); err != nil {
		return err
	}
	if !enabled {
		s.Cancel()
	}
	return nil
}

func (s *HongGuoService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshRequested = false
	if s.autoRefreshCancel != nil {
		s.autoRefreshCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.artworkCancel != nil {
		s.artworkCancel()
	}
	if s.discoveryCancel != nil {
		s.discoveryCancel()
	}
}

// Wait 在容器取消后等待来源任务退出，防止关闭数据库时仍在写入。
func (s *HongGuoService) Wait() {
	s.mu.Lock()
	s.closed = true
	s.refreshRequested = false
	if s.autoRefreshCancel != nil {
		s.autoRefreshCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.artworkCancel != nil {
		s.artworkCancel()
	}
	if s.discoveryCancel != nil {
		s.discoveryCancel()
	}
	s.mu.Unlock()
	s.runMu.Lock()
	s.runMu.Unlock()
	s.artworkMu.Lock()
	s.artworkMu.Unlock()
	s.discoveryMu.Lock()
	s.discoveryMu.Unlock()
}

// Run 的 sourceID 非空时只刷新指定作品，否则执行有界维护批次。
func (s *HongGuoService) Run(ctx context.Context, kind, sourceID string) error {
	name := ""
	switch kind {
	case TaskKindHongGuoSync:
		name = "红果作品发现"
	case TaskKindHongGuoRefresh:
		name = "红果资料刷新"
	case TaskKindHongGuoArtwork:
		name = "红果图片下载"
	case TaskKindHongGuoAlbum:
		name = "红果官方合集补充"
	default:
		return errors.New("未知红果任务")
	}
	if sourceID != "" && (kind != TaskKindHongGuoRefresh || !hongguo.ValidID(sourceID)) {
		return errors.New("红果作品 ID 无效")
	}
	// 发现只写摘要，详情与图片各自维护独立数据；每类任务只允许一个执行者。
	runMu, activeCancel := &s.runMu, &s.cancel
	if kind == TaskKindHongGuoArtwork {
		runMu, activeCancel = &s.artworkMu, &s.artworkCancel
	} else if kind == TaskKindHongGuoSync {
		runMu, activeCancel = &s.discoveryMu, &s.discoveryCancel
	}
	if !runMu.TryLock() {
		return ErrHongGuoRunning
	}
	defer func() {
		runMu.Unlock()
		if kind == TaskKindHongGuoRefresh || kind == TaskKindHongGuoAlbum {
			s.startRequestedRefresh()
		}
	}()
	return s.runLocked(ctx, kind, sourceID, name, activeCancel)
}

// runLocked 的调用方持有对应任务锁，自动与手动刷新共用同一执行记录和互斥。
func (s *HongGuoService) runLocked(ctx context.Context, kind, sourceID, name string, activeCancel *context.CancelFunc) error {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return errors.New("红果服务已关闭")
	}
	*activeCancel = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); *activeCancel = nil; s.mu.Unlock() }()
	enabled, err := s.Enabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrHongGuoDisabled
	}
	trigger := schedulerTaskTrigger(ctx)
	if event, _ := ctx.Value(hongGuoEventRefreshKey{}).(bool); event {
		trigger = TaskTriggerEvent
	}
	if sourceID != "" {
		trigger = TaskTriggerManual
	}
	task := s.tasks.StartTriggeredIfKindIdle(kind, trigger, name, TaskUpdate{Message: name})
	if task == nil {
		return errors.New("红果任务记录创建失败")
	}
	processed := int64(0)
	failed := int64(0)
	deferred := int64(0)
	newCount := int64(0)
	updatedCount := int64(0)
	unchangedCount := int64(0)
	albumWarnings := int64(0)
	reportAlbumWarning := func(id string, albumErr error) {
		albumWarnings++
		task.Update(TaskUpdate{Details: []string{fmt.Sprintf("⚠️ 红果 官方合集 %s：%v；已交给红果官方合集补充任务重试", id, sanitizeTaskLogError(albumErr))}})
	}
	metrics := func() map[string]int64 {
		return map[string]int64{"processed": processed, "failed": failed, "deferred": deferred, "succeeded": processed - failed - deferred, "new": newCount, "updated": updatedCount, "unchanged": unchangedCount, "album_warnings": albumWarnings}
	}
	report := func(id, change string, itemErr error) {
		processed++
		detail := "✅ 红果 " + id
		if errors.Is(itemErr, hongguo.ErrNotFound) {
			deferred++
			detail = fmt.Sprintf("⚠️ 红果 %s：资料不存在，已暂缓 3 天后重试", id)
		} else if itemErr != nil {
			failed++
			detail = fmt.Sprintf("❌ 红果 %s：%v", id, sanitizeTaskLogError(itemErr))
		} else if kind == TaskKindHongGuoRefresh {
			switch change {
			case "new":
				newCount++
				detail += "：新作品资料已补齐"
			case "updated":
				updatedCount++
				detail += "：资料有更新"
			case "unchanged":
				unchangedCount++
				detail += "：刷新后资料无变动"
			}
		}
		message := fmt.Sprintf("已处理 %d 项，失败 %d 项，暂缓 %d 项", processed, failed, deferred)
		if kind == TaskKindHongGuoRefresh {
			message = fmt.Sprintf("已处理 %d 项：新作品补齐 %d、资料更新 %d、无变动 %d；失败 %d、暂缓 %d", processed, newCount, updatedCount, unchangedCount, failed, deferred)
		}
		task.Update(TaskUpdate{Message: message, Details: []string{detail}, Metrics: metrics()})
	}
	switch kind {
	case TaskKindHongGuoSync:
		err = s.discover(ctx, func(ids []string) {
			processed += int64(len(ids))
			newCount += int64(len(ids))
			details := make([]string, 0, len(ids))
			for _, id := range ids {
				details = append(details, "✅ 新发现红果作品 "+id)
			}
			task.Update(TaskUpdate{Message: fmt.Sprintf("本轮新发现 %d 项，详情由资料刷新任务补齐", processed), Details: details, Metrics: metrics()})
			s.requestRefresh(ctx)
		}, func(message string) {
			task.Update(TaskUpdate{Message: message, Metrics: metrics()})
		})
	case TaskKindHongGuoRefresh:
		if sourceID != "" {
			var change string
			change, err = s.refresh(ctx, sourceID, reportAlbumWarning)
			if ctx.Err() == nil {
				report(sourceID, change, err)
			}
		} else {
			err = s.refreshBatch(ctx, report, reportAlbumWarning)
		}
	case TaskKindHongGuoAlbum:
		err = s.backfillAlbums(ctx, func(id string, err error) { report(id, "", err) })
	case TaskKindHongGuoArtwork:
		err = s.downloadArtwork(ctx, func(id string, err error) { report(id, "", err) })
	}
	finishErr := sanitizeTaskLogError(err)
	if errors.Is(err, context.Canceled) {
		finishErr = context.Canceled
	}
	message := fmt.Sprintf("本次处理 %d 项，失败 %d 项，暂缓 %d 项", processed, failed, deferred)
	if kind == TaskKindHongGuoSync {
		message = fmt.Sprintf("本次新增 %d 项，失败 %d 项，暂缓 %d 项", processed, failed, deferred)
	} else if kind == TaskKindHongGuoRefresh {
		message = fmt.Sprintf("本次处理 %d 项：新作品补齐 %d、资料更新 %d、无变动 %d；失败 %d、暂缓 %d", processed, newCount, updatedCount, unchangedCount, failed, deferred)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		message = fmt.Sprintf("任务失败：已处理 %d 项；具体原因见错误日志", processed)
		if failed > 0 {
			message = fmt.Sprintf("任务失败：已处理 %d 项，其中 %d 项失败", processed, failed)
		}
		if kind == TaskKindHongGuoRefresh {
			message += fmt.Sprintf("；新作品补齐 %d、资料更新 %d、无变动 %d", newCount, updatedCount, unchangedCount)
		}
	}
	if albumWarnings > 0 {
		message += fmt.Sprintf("；%d 项合集查询失败，交由红果官方合集补充任务重试", albumWarnings)
	}
	task.Finish(finishErr, TaskUpdate{Message: message, Metrics: metrics()})
	return err
}

func (s *HongGuoService) refresh(ctx context.Context, id string, reportAlbumWarning func(string, error)) (change string, err error) {
	defer func() {
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			delay := hongGuoRetryDelay
			if errors.Is(err, hongguo.ErrNotFound) {
				delay = hongGuoNotFoundDelay
			}
			if saveErr := s.repo.HongGuo.RecordSyncFailure(ctx, id, time.Now().Add(delay)); saveErr != nil {
				err = errors.Join(errHongGuoCheckpoint, saveErr)
			}
		} else {
			err = s.repo.HongGuo.ClearSyncFailure(ctx, id)
		}
	}()
	work, err := s.client.Detail(ctx, id)
	if err != nil {
		return "", err
	}
	_, change, err = s.repo.HongGuo.SaveDetailWithChange(ctx, work)
	if err != nil {
		return "", err
	}
	if err := s.repo.HongGuo.RebindWork(ctx, id); err != nil {
		return "", err
	}
	// 先保留待补充状态，取消或退出也不会丢失本次合集检查。
	if err := s.repo.HongGuo.RetryAlbum(ctx, id, time.Now()); err != nil {
		return "", errors.Join(errHongGuoCheckpoint, err)
	}
	if albumErr := s.refreshAlbum(ctx, id); albumErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(albumErr, errHongGuoCheckpoint) {
			return "", albumErr
		}
		reportAlbumWarning(id, albumErr)
	}
	return change, nil
}

func (s *HongGuoService) discover(ctx context.Context, report func([]string), notice func(string)) error {
	for _, category := range hongguo.Categories {
		state, err := s.repo.HongGuo.SyncState(ctx, category)
		if err != nil {
			return err
		}
		seen := make(map[string]bool)
		incremental := state.NextPage == 1 && state.AfterID != ""
		boundaryID := state.AfterID
		for {
			page := state.NextPage
			if incremental {
				page = 1
			}
			works, itemCount, err := s.client.Category(ctx, category, page)
			if err != nil {
				if page > 1 && errors.Is(err, hongguo.ErrNotFound) {
					previous, previousCount, previousErr := s.client.Category(ctx, category, page-1)
					if previousErr == nil && previousCount < hongguo.CategoryPageSize {
						state.NextPage = 1
						newIDs, saveErr := s.repo.HongGuo.SaveDiscoveryPageWithNew(ctx, previous, state)
						if saveErr != nil {
							return saveErr
						}
						report(newIDs)
						notice(fmt.Sprintf("⚠ 红果分类 %s 第 %d 页（/category/%s?page=%d）返回 HTTP 404；回查第 %d 页确认是尾页，已重置检查点", category, page, category, page, page-1))
						break
					}
				}
				return err
			}
			complete := itemCount < hongguo.CategoryPageSize
			added := 0
			for _, work := range works {
				if !seen[work.SourceID] {
					seen[work.SourceID] = true
					added++
				}
			}
			boundary := false
			for _, work := range works {
				if incremental && work.SourceID == boundaryID {
					boundary = true
				}
			}
			if page == 1 && len(works) > 0 {
				state.AfterID = works[0].SourceID
			}
			if len(works) > 0 && added == 0 {
				if incremental {
					state.NextPage = 1
					if err := s.repo.HongGuo.SaveDiscoveryPage(ctx, nil, state); err != nil {
						return err
					}
					notice(fmt.Sprintf("ℹ️ 红果分类 %s 增量扫描至第 %d 页，未发现新作品", category, page))
					break
				}
				return fmt.Errorf("红果分类 %s 第 %d 页重复返回本轮已见作品，分页未推进；保留检查点，未确认拉取完成", category, state.NextPage)
			}
			if complete {
				state.NextPage = 1
			} else if state.NextPage < hongguo.MaxCategoryPage {
				state.NextPage++
			}
			if incremental && boundary {
				state.NextPage = 1
				newIDs, err := s.repo.HongGuo.SaveDiscoveryPageWithNew(ctx, works, state)
				if err != nil {
					return err
				}
				report(newIDs)
				notice(fmt.Sprintf("ℹ️ 红果分类 %s 增量扫描至第 %d 页，已追平上次检查点", category, page))
				break
			}
			newIDs, err := s.repo.HongGuo.SaveDiscoveryPageWithNew(ctx, works, state)
			if err != nil {
				return err
			}
			report(newIDs)
			if complete {
				notice(fmt.Sprintf("ℹ️ 红果分类 %s 第 %d 页共 %d 项，少于每页 %d 项，目录已到末页并重置检查点", category, page, itemCount, hongguo.CategoryPageSize))
				break
			}
			if page == hongguo.MaxCategoryPage {
				return fmt.Errorf("红果分类 %s 已达到 %d 页安全上限但仍有数据，未确认拉取完成", category, page)
			}
		}
	}
	for _, rank := range hongguo.Ranks {
		works := []hongguo.Work{}
		seen := map[string]bool{}
		for page := 1; ; page++ {
			rows, hasNext, err := s.client.Rank(ctx, rank.Key, page)
			if err != nil {
				return err
			}
			for _, work := range rows {
				if seen[work.SourceID] {
					return fmt.Errorf("红果榜单 %s 重复返回作品 %s，未替换现有榜单", rank.Key, work.SourceID)
				}
				seen[work.SourceID] = true
				works = append(works, work)
			}
			if !hasNext {
				break
			}
			if page == hongguo.MaxRankPage {
				return fmt.Errorf("红果榜单 %s 已达到 %d 页安全上限但仍有下一页", rank.Key, page)
			}
		}
		newIDs, err := s.repo.HongGuo.ReplaceRankWithNew(ctx, rank.Key, rank.SourceCategory, works)
		if err != nil {
			return err
		}
		report(newIDs)
		notice(fmt.Sprintf("ℹ️ %s已按官网名次更新，共 %d 项", rank.Label, len(works)))
	}
	return nil
}

func (s *HongGuoService) refreshBatch(ctx context.Context, report func(string, string, error), reportAlbumWarning func(string, error)) error {
	failures := 0
	retried := map[string]bool{}
	due, err := s.repo.HongGuo.DueSyncFailures(ctx)
	if err != nil {
		return err
	}
	// 固定本轮边界，分页消化已有待补齐项，不无限追赶并行发现的新记录。
	cutoff := time.Now()
	after := ""
	for {
		pending, err := s.repo.HongGuo.PendingDiscoveries(ctx, after, cutoff)
		if err != nil {
			return err
		}
		for _, row := range pending {
			change, err := s.refresh(ctx, row.SourceID, reportAlbumWarning)
			if errors.Is(err, errHongGuoCheckpoint) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil && !errors.Is(err, hongguo.ErrNotFound) {
				failures++
			}
			report(row.SourceID, change, err)
			after = row.SourceID
		}
		if len(pending) < 100 {
			break
		}
	}
	for _, row := range due {
		retried[row.SourceID] = true
		change, err := s.refresh(ctx, row.SourceID, reportAlbumWarning)
		if err != nil {
			if errors.Is(err, errHongGuoCheckpoint) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(err, hongguo.ErrNotFound) {
				failures++
			}
			report(row.SourceID, change, err)
		} else {
			report(row.SourceID, change, nil)
		}
	}
	state, err := s.repo.HongGuo.SyncState(ctx, "refresh")
	if err != nil {
		return err
	}
	works, err := s.repo.HongGuo.WorksAfter(ctx, state.AfterID, 100)
	if err != nil {
		return err
	}
	for _, work := range works {
		if retried[work.SourceID] {
			// 失败队列已经处理过的作品只推进常规游标，不在同一轮重复请求。
		} else if change, err := s.refresh(ctx, work.SourceID, reportAlbumWarning); err != nil {
			if errors.Is(err, errHongGuoCheckpoint) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(err, hongguo.ErrNotFound) {
				failures++
			}
			report(work.SourceID, change, err)
		} else {
			report(work.SourceID, change, nil)
		}
		state.AfterID = work.ID
		if err := s.repo.HongGuo.SaveSyncState(ctx, state); err != nil {
			return err
		}
	}
	if len(works) < 100 {
		state.AfterID = ""
		if err := s.repo.HongGuo.SaveSyncState(ctx, state); err != nil {
			return err
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d 个作品刷新失败，已保留 ID 等待重试", failures)
	}
	return nil
}

func (s *HongGuoService) downloadArtwork(ctx context.Context, report func(string, error)) error {
	cutoff := time.Now()
	after := ""
	var failures int
	for {
		rows, err := s.repo.HongGuo.DueArtwork(ctx, after, cutoff)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			key, err := s.storeArtwork(ctx, row)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var retry *time.Time
			if err != nil {
				failures++
				at := time.Now().Add(time.Duration(min(row.Attempts+1, 24)) * time.Hour)
				retry = &at
			}
			if err := s.repo.HongGuo.FinishArtwork(ctx, row, key, retry); err != nil {
				return err
			}
			after = row.ID
			report(row.ID, err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d 张红果图片下载失败，已安排重试", failures)
	}
	return nil
}

func (s *HongGuoService) storeArtwork(ctx context.Context, row model.HongGuoArtwork) (string, error) {
	if s.images == nil {
		return "", errors.New("图片下载不可用")
	}
	data, _, err := s.images.fetchRemoteImageDirect(ctx, row.SourceURL)
	if err != nil {
		return "", err
	}
	stored, err := prepareStoredImage(data)
	if err != nil {
		return "", err
	}
	path, err := storedImagePath(s.imageRoot, stored.StorageKey)
	if err != nil {
		return "", err
	}
	if err := writeStoredImage(path, data, ".hongguo-*.tmp"); err != nil {
		return "", err
	}
	return stored.StorageKey, nil
}

func (s *HongGuoService) ServeArtwork(ctx context.Context, w http.ResponseWriter, r *http.Request, id string) error {
	row, err := s.repo.HongGuo.Artwork(ctx, id)
	if err != nil {
		return err
	}
	path, err := storedImagePath(s.imageRoot, row.LocalKey)
	if err != nil || !s.images.serveImageFile(w, r, row.LocalKey, path, imageBrowserCacheControl) {
		if scheduleErr := s.repo.HongGuo.ScheduleMissingArtwork(ctx, row.ID); scheduleErr != nil {
			return scheduleErr
		}
		return ErrArtworkNotFound
	}
	return nil
}
