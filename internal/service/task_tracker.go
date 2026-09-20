package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	TaskStatusRunning     = "running"
	TaskStatusCompleted   = "completed"
	TaskStatusFailed      = "failed"
	TaskStatusInterrupted = "interrupted"

	TaskTriggerManual    = "manual"
	TaskTriggerScheduled = "scheduled"
	TaskTriggerEvent     = "event"

	TaskKindOrganize              = "organize"
	TaskKindProbe                 = "probe"
	TaskKindScan                  = "scan"
	TaskKindWatch                 = "watch"
	TaskKindNFOScan               = "nfo_scan"
	TaskKindNFOWatch              = "nfo_watch"
	TaskKindScrape                = "scrape"
	TaskKindPeople                = "people"
	TaskKindArtwork               = "artwork"
	TaskKindCleanup               = "cleanup"
	TaskKindTMDbSnapshotBackfill  = "tmdb_snapshot_backfill"
	TaskKindSeriesLocalCorrection = "series_local_correction"
)

// BackgroundTask 是任务中心展示的一次后台执行摘要。
type BackgroundTask struct {
	System     string           `json:"system"`
	ID         string           `json:"id"`
	Kind       string           `json:"kind"`
	Trigger    string           `json:"trigger"`
	Name       string           `json:"name"`
	Status     string           `json:"status"`
	Stage      string           `json:"stage,omitempty"`
	SourcePath string           `json:"source_path,omitempty"`
	DestPath   string           `json:"dest_path,omitempty"`
	Message    string           `json:"message,omitempty"`
	Error      string           `json:"error,omitempty"`
	Details    []string         `json:"details,omitempty"`
	Metrics    map[string]int64 `json:"metrics,omitempty"`
	StartedAt  time.Time        `json:"started_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	FinishedAt *time.Time       `json:"finished_at,omitempty"`
}

type TaskUpdate struct {
	Stage               string
	SourcePath          string
	DestPath            string
	Message             string
	Details             []string
	DetailsWithoutLevel bool
	Metrics             map[string]int64
}

type TaskSnapshot struct {
	Active []BackgroundTask `json:"active"`
	Recent []BackgroundTask `json:"recent"`
}

type TaskPage struct {
	Items    []BackgroundTask `json:"items"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int64            `json:"total"`
}

type TaskLog struct {
	Date      string   `json:"date"`
	Dates     []string `json:"dates"`
	Content   string   `json:"content"`
	Truncated bool     `json:"truncated"`
}

type TaskTrackerService struct {
	log *zap.Logger
	hub *Hub

	mu        sync.Mutex
	startMu   sync.Mutex
	active    map[string]*BackgroundTask
	recent    []BackgroundTask
	maxRecent int
	now       func() time.Time

	repo *repository.TaskExecutionRepository
	logs *taskLogStore
	ctx  context.Context
}

type TaskHandle struct {
	tracker *TaskTrackerService
	id      string
}

func NewTaskTrackerService(log *zap.Logger, hub *Hub) *TaskTrackerService {
	return &TaskTrackerService{
		log: log, hub: hub, active: make(map[string]*BackgroundTask), maxRecent: 30,
		now: time.Now, ctx: context.Background(),
	}
}

// ConfigurePersistence 启用数据库摘要和按任务分日的详细日志。
func (t *TaskTrackerService) ConfigurePersistence(repo *repository.TaskExecutionRepository, dataDir string) {
	if t == nil {
		return
	}
	t.repo = repo
	t.logs = newTaskLogStore(dataDir, t.currentTime)
}

func (t *TaskTrackerService) Recover(ctx context.Context) error {
	if t == nil || t.repo == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.ctx = ctx
	return t.repo.MarkRunningInterrupted(ctx, t.currentTime())
}

func (t *TaskTrackerService) Start(kind, name string, update TaskUpdate) *TaskHandle {
	return t.StartTriggered(kind, TaskTriggerManual, name, update)
}

func (t *TaskTrackerService) StartTriggered(kind, trigger, name string, update TaskUpdate) *TaskHandle {
	if t == nil {
		return nil
	}
	t.startMu.Lock()
	defer t.startMu.Unlock()
	return t.startTriggered(kind, trigger, name, update)
}

// StartTriggeredIfKindIdle 仅在同类任务未运行时原子创建任务。
func (t *TaskTrackerService) StartTriggeredIfKindIdle(kind, trigger, name string, update TaskUpdate) *TaskHandle {
	if t == nil {
		return nil
	}
	t.startMu.Lock()
	defer t.startMu.Unlock()
	if t.IsKindRunning(kind) {
		return nil
	}
	return t.startTriggered(kind, trigger, name, update)
}

func (t *TaskTrackerService) startTriggered(kind, trigger, name string, update TaskUpdate) *TaskHandle {
	if strings.TrimSpace(trigger) == "" {
		trigger = TaskTriggerManual
	}
	now := t.currentTime()
	task := &BackgroundTask{
		System: model.TaskSystemForKind(kind),
		ID:     uuid.NewString(), Kind: kind, Trigger: trigger, Name: name, Status: TaskStatusRunning,
		Stage: update.Stage, SourcePath: update.SourcePath, DestPath: update.DestPath,
		Message: update.Message, Metrics: cloneTaskMetrics(update.Metrics), StartedAt: now, UpdatedAt: now,
	}
	if t.repo != nil {
		row := taskExecutionFromBackground(*task)
		if err := t.repo.Create(t.context(), &row); err != nil {
			t.logError("create task execution failed", err)
			return nil
		}
	}
	t.mu.Lock()
	t.active[task.ID] = task
	snapshot := cloneBackgroundTask(*task)
	t.mu.Unlock()
	if snapshot.Trigger != TaskTriggerEvent {
		t.appendLog(snapshot, "", taskLogLine("🔻", task.Message))
	}
	t.appendDetails(snapshot, update.Details, update.DetailsWithoutLevel)
	t.publish(snapshot)
	return &TaskHandle{tracker: t, id: task.ID}
}

func (h *TaskHandle) Update(update TaskUpdate) {
	if h != nil && h.tracker != nil {
		h.tracker.update(h.id, update)
	}
}

func (h *TaskHandle) Finish(err error, update TaskUpdate) {
	if h != nil && h.tracker != nil {
		h.tracker.finish(h.id, err, update)
	}
}

func (t *TaskTrackerService) Snapshot() TaskSnapshot {
	if t == nil {
		return TaskSnapshot{}
	}
	if t.repo != nil {
		page, err := t.List(1, t.maxRecent+len(t.active))
		if err == nil {
			snapshot := TaskSnapshot{}
			for _, task := range page.Items {
				if task.Status == TaskStatusRunning {
					snapshot.Active = append(snapshot.Active, task)
				} else if len(snapshot.Recent) < t.maxRecent {
					snapshot.Recent = append(snapshot.Recent, task)
				}
			}
			return snapshot
		}
		t.logError("list task executions failed", err)
	}
	return t.memorySnapshot()
}

// IsKindRunning 判断同类后台任务是否正在当前进程中执行。
func (t *TaskTrackerService) IsKindRunning(kind string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, task := range t.active {
		if task.Kind == kind {
			return true
		}
	}
	return false
}

func (t *TaskTrackerService) List(page, pageSize int) (TaskPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 30
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if t == nil {
		return TaskPage{Items: []BackgroundTask{}, Page: page, PageSize: pageSize}, nil
	}
	if t.repo == nil {
		snapshot := t.memorySnapshot()
		items := append(snapshot.Active, snapshot.Recent...)
		return TaskPage{Items: items, Page: page, PageSize: pageSize, Total: int64(len(items))}, nil
	}
	rows, total, err := t.repo.List(t.context(), (page-1)*pageSize, pageSize)
	if err != nil {
		return TaskPage{}, err
	}
	items := make([]BackgroundTask, 0, len(rows))
	for _, row := range rows {
		items = append(items, backgroundFromTaskExecution(row))
	}
	return TaskPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (t *TaskTrackerService) ReadDefinitionLog(key, date string, tailBytes int64) (TaskLog, error) {
	if !isTaskDefinitionKey(key) {
		return TaskLog{}, ErrTaskDefinitionNotFound
	}
	if date != "" && !validTaskLogDate(date) {
		return TaskLog{}, ErrTaskLogDateNotFound
	}
	dates, err := t.logs.dates(key)
	if err != nil {
		return TaskLog{}, err
	}
	if len(dates) == 0 {
		return TaskLog{Dates: []string{}}, nil
	}
	if date == "" {
		date = dates[0]
	} else if !slices.Contains(dates, date) {
		return TaskLog{}, ErrTaskLogDateNotFound
	}
	content, truncated, err := t.logs.read(key, date, tailBytes)
	return TaskLog{Date: date, Dates: dates, Content: content, Truncated: truncated}, err
}

func (t *TaskTrackerService) update(id string, update TaskUpdate) {
	now := t.currentTime()
	t.mu.Lock()
	task, ok := t.active[id]
	if !ok {
		t.mu.Unlock()
		return
	}
	messageChanged := strings.TrimSpace(update.Message) != "" && strings.TrimSpace(update.Message) != strings.TrimSpace(task.Message)
	applyTaskUpdate(task, update)
	task.UpdatedAt = now
	snapshot := cloneBackgroundTask(*task)
	t.mu.Unlock()
	if t.repo != nil {
		if err := t.repo.Update(t.context(), id, taskExecutionUpdates(snapshot)); err != nil {
			t.logError("update task execution failed", err)
		}
	}
	if messageChanged {
		t.appendLog(snapshot, "", taskLogLine("🔄", update.Message))
	}
	t.appendDetails(snapshot, update.Details, update.DetailsWithoutLevel)
	t.publish(snapshot)
}

func (t *TaskTrackerService) finish(id string, finishErr error, update TaskUpdate) {
	now := t.currentTime()
	t.mu.Lock()
	task, ok := t.active[id]
	if !ok {
		t.mu.Unlock()
		return
	}
	applyTaskUpdate(task, update)
	task.UpdatedAt = now
	task.FinishedAt = &now
	if errors.Is(finishErr, context.Canceled) {
		task.Status = TaskStatusInterrupted
		task.Error = finishErr.Error()
	} else if finishErr != nil {
		task.Status = TaskStatusFailed
		task.Error = finishErr.Error()
	} else {
		task.Status = TaskStatusCompleted
	}
	delete(t.active, id)
	snapshot := cloneBackgroundTask(*task)
	t.recent = append([]BackgroundTask{snapshot}, t.recent...)
	if len(t.recent) > t.maxRecent {
		t.recent = t.recent[:t.maxRecent]
	}
	t.mu.Unlock()
	if t.repo != nil {
		if err := t.repo.Update(t.context(), id, taskExecutionUpdates(snapshot)); err != nil {
			t.logError("finish task execution failed", err)
		}
	}
	t.appendDetails(snapshot, update.Details, update.DetailsWithoutLevel)
	if finishErr != nil {
		t.appendLog(snapshot, "", taskLogLine("❌", finishErr.Error()))
	}
	if snapshot.Trigger != TaskTriggerEvent {
		t.appendLog(snapshot, "", taskLogLine("🔺", update.Message))
	}
	t.publish(snapshot)
}

func (t *TaskTrackerService) memorySnapshot() TaskSnapshot {
	if t == nil {
		return TaskSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	active := make([]BackgroundTask, 0, len(t.active))
	for _, task := range t.active {
		active = append(active, cloneBackgroundTask(*task))
	}
	recent := make([]BackgroundTask, 0, len(t.recent))
	for _, task := range t.recent {
		recent = append(recent, cloneBackgroundTask(task))
	}
	return TaskSnapshot{Active: active, Recent: recent}
}

func (t *TaskTrackerService) currentTime() time.Time {
	if t != nil && t.now != nil {
		return t.now()
	}
	return time.Now()
}

func (t *TaskTrackerService) context() context.Context {
	if t != nil && t.ctx != nil {
		return t.ctx
	}
	return context.Background()
}

func (t *TaskTrackerService) publish(task BackgroundTask) {
	if t != nil && t.hub != nil {
		t.hub.Publish("task", task)
	}
}

func (t *TaskTrackerService) appendLog(task BackgroundTask, level, message string) {
	key := taskDefinitionKeyForTask(task)
	if key == "" {
		return
	}
	if err := t.logs.append(key, level, message); err != nil {
		t.logError("append task log failed", err)
	}
}

func (t *TaskTrackerService) appendDetails(task BackgroundTask, details []string, _ bool) {
	for _, detail := range details {
		t.appendLog(task, "", taskLogLine("ℹ️", detail))
	}
}

const taskLogMarkerRunes = "🔺🔻➕🗑🔄✅❌⏭⚠ℹ"

func taskLogLine(marker, message string) string {
	message = strings.TrimSpace(message)
	for _, first := range message {
		if strings.ContainsRune(taskLogMarkerRunes, first) {
			return message
		}
		break
	}
	if message == "" {
		return ""
	}
	return marker + " " + message
}

func (t *TaskTrackerService) logError(message string, err error) {
	if t != nil && t.log != nil && err != nil {
		t.log.Error(message, zap.Error(err))
	}
}

func applyTaskUpdate(task *BackgroundTask, update TaskUpdate) {
	if update.Stage != "" {
		task.Stage = update.Stage
	}
	if update.SourcePath != "" {
		task.SourcePath = update.SourcePath
	}
	if update.DestPath != "" {
		task.DestPath = update.DestPath
	}
	if update.Message != "" {
		task.Message = update.Message
	}
	if update.Details != nil {
		task.Details = append([]string(nil), update.Details...)
	}
	if update.Metrics != nil {
		task.Metrics = cloneTaskMetrics(update.Metrics)
	}
}

func taskExecutionFromBackground(task BackgroundTask) model.TaskExecution {
	metrics, _ := json.Marshal(task.Metrics)
	return model.TaskExecution{
		System: task.System,
		Base:   model.Base{ID: task.ID}, Kind: task.Kind, Trigger: task.Trigger, Name: task.Name,
		Status: task.Status, Stage: task.Stage, SourcePath: task.SourcePath, DestPath: task.DestPath,
		Message: task.Message, Error: task.Error, Metrics: string(metrics), StartedAt: task.StartedAt,
		FinishedAt: task.FinishedAt,
	}
}

func taskExecutionUpdates(task BackgroundTask) map[string]any {
	metrics, _ := json.Marshal(task.Metrics)
	return map[string]any{
		"status": task.Status, "stage": task.Stage, "source_path": task.SourcePath,
		"dest_path": task.DestPath, "message": task.Message, "error": task.Error,
		"metrics": string(metrics), "finished_at": task.FinishedAt, "updated_at": task.UpdatedAt,
	}
}

func backgroundFromTaskExecution(row model.TaskExecution) BackgroundTask {
	if row.System == "" {
		row.System = model.TaskSystemForKind(row.Kind)
	}
	metrics := map[string]int64{}
	_ = json.Unmarshal([]byte(row.Metrics), &metrics)
	if len(metrics) == 0 {
		metrics = nil
	}
	return BackgroundTask{
		System: row.System,
		ID:     row.ID, Kind: row.Kind, Trigger: row.Trigger, Name: row.Name, Status: row.Status,
		Stage: row.Stage, SourcePath: row.SourcePath, DestPath: row.DestPath, Message: row.Message,
		Error: row.Error, Metrics: metrics, StartedAt: row.StartedAt, UpdatedAt: row.UpdatedAt,
		FinishedAt: row.FinishedAt,
	}
}

func cloneBackgroundTask(task BackgroundTask) BackgroundTask {
	task.Metrics = cloneTaskMetrics(task.Metrics)
	if task.Details != nil {
		task.Details = append([]string(nil), task.Details...)
	}
	if task.FinishedAt != nil {
		finishedAt := *task.FinishedAt
		task.FinishedAt = &finishedAt
	}
	return task
}

func cloneTaskMetrics(metrics map[string]int64) map[string]int64 {
	if len(metrics) == 0 {
		return nil
	}
	out := make(map[string]int64, len(metrics))
	for key, value := range metrics {
		out[key] = value
	}
	return out
}
