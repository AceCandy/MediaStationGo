package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	TaskKindHuangGuoAISync    = "huangguoai_sync"
	TaskKindHuangGuoAIRefresh = "huangguoai_refresh"
	TaskKindHuangGuoAIArtwork = "huangguoai_artwork"
	TaskKindHuangGuoAIRank    = "huangguoai_rank"
)

type huangGuoAIEventKey struct{}

var ErrHuangGuoAIRunning = errors.New("黄果 AI 任务正在运行")
var ErrHuangGuoAIDisabled = errors.New("黄果 AI 来源已停用")

// HuangGuoAIService keeps source jobs and cancellation independent of other catalogs.
type HuangGuoAIService struct {
	repo              *repository.Container
	client            *huangguoai.Client
	tasks             *TaskTrackerService
	downloads         *HuangGuoAIDownloadService
	images            *ImageProxy
	imageRoot         string
	mu                sync.Mutex
	running           map[string]context.CancelFunc
	closed            bool
	refreshRequested  bool
	autoRefreshCancel context.CancelFunc
	wg                sync.WaitGroup
}

func NewHuangGuoAIService(repo *repository.Container, tasks *TaskTrackerService, images *ImageProxy, dataDir string) *HuangGuoAIService {
	return &HuangGuoAIService{repo: repo, client: huangguoai.NewClient(nil), tasks: tasks, images: images, imageRoot: filepath.Join(dataDir, "catalogs", "huangguoai", "artwork"), running: map[string]context.CancelFunc{}}
}

func (s *HuangGuoAIService) Enabled(ctx context.Context) (bool, error) {
	value, err := s.repo.Setting.Get(ctx, "huangguoai.enabled")
	return value != "false", err
}
func (s *HuangGuoAIService) SetEnabled(ctx context.Context, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	if err := s.repo.Setting.Set(ctx, "huangguoai.enabled", value); err != nil {
		return err
	}
	if !enabled {
		s.Cancel()
	}
	return nil
}
func (s *HuangGuoAIService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshRequested = false
	if s.autoRefreshCancel != nil {
		s.autoRefreshCancel()
	}
	for _, cancel := range s.running {
		cancel()
	}
}
func (s *HuangGuoAIService) Wait() {
	s.mu.Lock()
	s.closed = true
	s.refreshRequested = false
	if s.autoRefreshCancel != nil {
		s.autoRefreshCancel()
	}
	for _, cancel := range s.running {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *HuangGuoAIService) Run(ctx context.Context, kind, id string) error {
	names := map[string]string{TaskKindHuangGuoAISync: "黄果 AI 作品发现", TaskKindHuangGuoAIRefresh: "黄果 AI 资料刷新", TaskKindHuangGuoAIArtwork: "黄果 AI 图片下载", TaskKindHuangGuoAIRank: "黄果 AI 排行榜刷新"}
	name, ok := names[kind]
	if !ok || (id != "" && (kind != TaskKindHuangGuoAIRefresh || !huangguoai.ValidID(id))) {
		return errors.New("黄果 AI 任务参数无效")
	}
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return context.Canceled
	}
	if s.running[kind] != nil {
		s.mu.Unlock()
		cancel()
		return ErrHuangGuoAIRunning
	}
	s.running[kind] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.running, kind)
		s.mu.Unlock()
		s.startRequestedRefresh()
		s.wg.Done()
	}()
	enabled, err := s.Enabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrHuangGuoAIDisabled
	}
	trigger := schedulerTaskTrigger(ctx)
	if event, _ := ctx.Value(huangGuoAIEventKey{}).(bool); event {
		trigger = TaskTriggerEvent
	}
	task := s.tasks.StartTriggeredIfKindIdle(kind, trigger, name, TaskUpdate{Message: name})
	if task == nil {
		return errors.New("黄果 AI 任务记录创建失败")
	}
	var processed, failed int64
	report := func(key string, e error) {
		processed++
		status := "✅ 黄果 AI " + key
		if e != nil {
			failed++
			status = "❌ 黄果 AI " + key + "：处理失败，等待重试"
		}
		task.Update(TaskUpdate{Message: fmt.Sprintf("已处理 %d 项，失败 %d 项", processed, failed), Details: []string{status}, Metrics: map[string]int64{"processed": processed, "failed": failed, "succeeded": processed - failed}})
	}
	switch kind {
	case TaskKindHuangGuoAISync:
		err = s.discover(ctx, report)
	case TaskKindHuangGuoAIRank:
		for _, key := range huangguoai.Ranks {
			rows, e := s.client.Rank(ctx, key)
			if e == nil {
				_, e = s.repo.HuangGuoAI.ReplaceRank(ctx, key, rows)
			}
			report(key, e)
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
			if e != nil {
				err = errors.New("部分榜单刷新失败，已保留上一份榜单")
			}
		}
	case TaskKindHuangGuoAIRefresh:
		if id != "" {
			err = s.refresh(ctx, id)
			report(id, err)
		} else {
			err = s.refreshBatch(ctx, report)
		}
		if s.downloads != nil && ctx.Err() == nil {
			if catchUpErr := s.downloads.catchUp(ctx, id, report); catchUpErr != nil && err == nil {
				err = catchUpErr
			}
		}
	case TaskKindHuangGuoAIArtwork:
		err = s.downloadArtwork(ctx, report)
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	safeErr := err
	if err != nil && !errors.Is(err, context.Canceled) {
		safeErr = errors.New("黄果 AI 任务失败，保留已完成结果和重试状态")
	}
	task.Finish(safeErr, TaskUpdate{Message: fmt.Sprintf("本次处理 %d 项，失败 %d 项", processed, failed), Metrics: map[string]int64{"processed": processed, "failed": failed, "succeeded": processed - failed}})
	return err
}

func (s *HuangGuoAIService) discover(ctx context.Context, report func(string, error)) error {
	for _, category := range huangguoai.Categories {
		state, err := s.repo.HuangGuoAI.SyncState(ctx, category)
		if err != nil {
			return err
		}
		for page := state.NextPage; page <= huangguoai.MaxPage; page++ {
			result, e := s.client.Category(ctx, category, page)
			if e != nil {
				return e
			}
			state.NextPage = page + 1
			last := page >= result.Pages
			if last {
				state.NextPage = 1
				state.Round++
			}
			ids, e := s.repo.HuangGuoAI.SaveDiscoveryPage(ctx, result.Items, state)
			if e != nil {
				return e
			}
			if len(ids) > 0 {
				s.requestRefresh(ctx)
			}
			for _, id := range ids {
				report(id, nil)
			}
			if last {
				break
			}
		}
	}
	return nil
}
func (s *HuangGuoAIService) refresh(ctx context.Context, id string) error {
	input, err := s.client.Detail(ctx, id)
	if err == nil {
		_, _, err = s.repo.HuangGuoAI.SaveDetail(ctx, input)
		if err == nil {
			err = s.repo.HuangGuoAI.RebindWork(ctx, id)
		}
	}
	if err != nil && ctx.Err() == nil {
		if saveErr := s.repo.HuangGuoAI.RecordFailure(ctx, "detail", id, "detail_unavailable"); saveErr != nil {
			return saveErr
		}
	}
	return err
}
func (s *HuangGuoAIService) refreshBatch(ctx context.Context, report func(string, error)) error {
	cutoff := time.Now()
	after := ""
	failed := 0
	for {
		ids, err := s.repo.HuangGuoAI.Pending(ctx, after, cutoff)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			e := s.refresh(ctx, id)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if e != nil {
				failed++
			}
			report(id, e)
			after = id
		}
	}
	if failed > 0 {
		return errors.New("部分黄果 AI 资料刷新失败")
	}
	return nil
}
func (s *HuangGuoAIService) Search(ctx context.Context, keyword string, page int) ([]huangguoai.Summary, error) {
	enabled, err := s.Enabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrHuangGuoAIDisabled
	}
	rows, err := s.client.Search(ctx, keyword, page)
	if err != nil {
		return nil, err
	}
	if err = s.repo.HuangGuoAI.RegisterSummaries(ctx, rows); err != nil {
		return nil, err
	}
	s.requestRefresh(ctx)
	return rows, nil
}
func (s *HuangGuoAIService) downloadArtwork(ctx context.Context, report func(string, error)) error {
	cutoff := time.Now()
	after := ""
	failed := 0
	for {
		rows, err := s.repo.HuangGuoAI.DueArtwork(ctx, after, cutoff)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			key, e := s.storeArtwork(ctx, row)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var retry *time.Time
			if e != nil {
				failed++
				at := time.Now().Add(time.Duration(min(row.Attempts+1, 24)) * time.Hour)
				retry = &at
			}
			if err := s.repo.HuangGuoAI.FinishArtwork(ctx, row, key, retry); err != nil {
				return err
			}
			after = row.ID
			report(row.ID, e)
		}
	}
	if failed > 0 {
		return errors.New("部分黄果 AI 图片下载失败")
	}
	return nil
}
func (s *HuangGuoAIService) storeArtwork(ctx context.Context, row model.HuangGuoAIArtwork) (string, error) {
	if s.images == nil {
		return "", errors.New("图片下载不可用")
	}
	data, err := s.client.Artwork(ctx, row.SourceURL)
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
	if err = writeStoredImage(path, data, ".huangguoai-*.tmp"); err != nil {
		return "", err
	}
	return stored.StorageKey, nil
}
func (s *HuangGuoAIService) ServeArtwork(ctx context.Context, w http.ResponseWriter, r *http.Request, id string) error {
	row, err := s.repo.HuangGuoAI.Artwork(ctx, id)
	if err != nil {
		return err
	}
	path, err := storedImagePath(s.imageRoot, row.LocalKey)
	if err != nil || s.images == nil || !s.images.serveImageFile(w, r, row.LocalKey, path, imageBrowserCacheControl) {
		if e := s.repo.HuangGuoAI.ScheduleMissingArtwork(ctx, row.ID); e != nil {
			return e
		}
		return ErrArtworkNotFound
	}
	return nil
}

// requestRefresh 合并目录和搜索唤醒，业务表保留真正的待补齐状态。
func (s *HuangGuoAIService) requestRefresh(ctx context.Context) {
	s.mu.Lock()
	if s.closed || ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.refreshRequested = true
	s.mu.Unlock()
	s.startRequestedRefresh()
}
func (s *HuangGuoAIService) startRequestedRefresh() {
	s.mu.Lock()
	if s.closed || !s.refreshRequested || s.autoRefreshCancel != nil || s.running[TaskKindHuangGuoAIRefresh] != nil {
		s.mu.Unlock()
		return
	}
	s.refreshRequested = false
	ctx, cancel := context.WithCancel(context.Background())
	s.autoRefreshCancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			s.autoRefreshCancel = nil
			s.mu.Unlock()
			s.startRequestedRefresh()
			s.wg.Done()
		}()
		if err := s.Run(context.WithValue(ctx, huangGuoAIEventKey{}, true), TaskKindHuangGuoAIRefresh, ""); errors.Is(err, ErrHuangGuoAIRunning) {
			s.mu.Lock()
			if !s.closed && ctx.Err() == nil {
				s.refreshRequested = true
			}
			s.mu.Unlock()
		}
	}()
}
