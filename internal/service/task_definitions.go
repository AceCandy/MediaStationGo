package service

import (
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	TaskDefinitionOrganize          = "organize"
	TaskDefinitionLibraryScan       = "library_scan"
	TaskDefinitionProbeBackfill     = "probe_backfill"
	TaskDefinitionMediaScrape       = "media_scrape"
	TaskDefinitionCatalogScrape     = "catalog_scrape"
	TaskDefinitionPeopleBackfill    = "people_backfill"
	TaskDefinitionPeopleTranslation = "people_translation"
	TaskDefinitionRecyclePurge      = "recycle_purge"
)

var ErrTaskDefinitionNotFound = errors.New("task definition not found")

type TaskDefinition struct {
	Key          string          `json:"key"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Trigger      string          `json:"trigger"`
	Schedule     string          `json:"schedule,omitempty"`
	CurrentState string          `json:"current_state"`
	NextRun      *time.Time      `json:"next_run,omitempty"`
	Action       string          `json:"action,omitempty"`
	Latest       *BackgroundTask `json:"latest,omitempty"`
}

type taskDefinitionSpec struct {
	TaskDefinition
	filter       repository.TaskExecutionFilter
	schedulerJob string
}

var taskDefinitionSpecs = []taskDefinitionSpec{
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionOrganize, Name: "媒体整理", Description: "整理、重命名并入库下载目录内容", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindOrganize}, schedulerJob: "organize_source"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionLibraryScan, Name: "媒体库扫描", Description: "扫描已启用媒体库并同步入库变化", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindScan}, schedulerJob: "library_scan"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionProbeBackfill, Name: "媒体轨道回填", Description: "为媒体库补充视频、音频和字幕轨道", Trigger: "媒体库手动触发"}, filter: repository.TaskExecutionFilter{Kind: TaskKindProbe}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionMediaScrape, Name: "媒体入库刮削", Description: "优先处理已入库媒体的待刮削对象", Trigger: "事件触发"}, filter: repository.TaskExecutionFilter{Kind: TaskKindScrape, ExcludeNamePrefix: "发现目录刮削："}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionCatalogScrape, Name: "发现目录刮削", Description: "后台补全发现目录中的电影和电视剧", Trigger: "事件触发"}, filter: repository.TaskExecutionFilter{Kind: TaskKindScrape, NamePrefix: "发现目录刮削："}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionPeopleBackfill, Name: "人物信息补齐", Description: "补齐尚未获取演职员信息的元数据", Trigger: "事件 / 手动", Action: "people_backfill"}, filter: repository.TaskExecutionFilter{Kind: TaskKindPeople, Name: "人物信息补齐"}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionPeopleTranslation, Name: "人物翻译", Description: "逐步翻译尚无中文名称和角色名的人物", Trigger: "事件触发"}, filter: repository.TaskExecutionFilter{Kind: TaskKindPeople, Name: "人物翻译"}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionRecyclePurge, Name: "回收站清理", Description: "清理超过保留期限的回收站记录", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindRecycle}, schedulerJob: "recycle_purge"},
}

func isTaskDefinitionKey(key string) bool {
	_, ok := taskDefinitionSpecForKey(key)
	return ok
}

func taskDefinitionSpecForKey(key string) (taskDefinitionSpec, bool) {
	for _, spec := range taskDefinitionSpecs {
		if spec.Key == strings.TrimSpace(key) {
			return spec, true
		}
	}
	return taskDefinitionSpec{}, false
}

func taskDefinitionKeyForTask(task BackgroundTask) string {
	for _, spec := range taskDefinitionSpecs {
		if taskMatchesFilter(task, spec.filter) {
			return spec.Key
		}
	}
	return ""
}

func (t *TaskTrackerService) Definitions(scheduler []JobStatus) ([]TaskDefinition, error) {
	statuses := make(map[string]JobStatus, len(scheduler))
	for _, status := range scheduler {
		statuses[status.Name] = status
	}
	definitions := make([]TaskDefinition, 0, len(taskDefinitionSpecs))
	active := t.memorySnapshot().Active
	for _, spec := range taskDefinitionSpecs {
		definition := spec.TaskDefinition
		definition.CurrentState = "idle"
		latestFilter := spec.filter
		latestFilter.ExcludeStatus = TaskStatusRunning
		latest, err := t.latestFiltered(latestFilter)
		if err != nil {
			return nil, err
		}
		if latest != nil {
			definition.Latest = latest
		}
		for _, task := range active {
			if taskMatchesFilter(task, spec.filter) {
				definition.CurrentState = TaskStatusRunning
				break
			}
		}
		if status, ok := statuses[spec.schedulerJob]; ok {
			definition.Schedule = status.Interval
			if !status.NextRun.IsZero() {
				next := status.NextRun
				definition.NextRun = &next
			}
			if status.Running {
				definition.CurrentState = TaskStatusRunning
			}
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func (t *TaskTrackerService) latestFiltered(filter repository.TaskExecutionFilter) (*BackgroundTask, error) {
	if t == nil {
		return nil, nil
	}
	if t.repo == nil {
		snapshot := t.memorySnapshot()
		for _, task := range append(snapshot.Active, snapshot.Recent...) {
			if taskMatchesFilter(task, filter) {
				copy := task
				return &copy, nil
			}
		}
		return nil, nil
	}
	row, err := t.repo.FindLatestFiltered(t.context(), filter)
	if err != nil || row == nil {
		return nil, err
	}
	task := backgroundFromTaskExecution(*row)
	return &task, nil
}

func (t *TaskTrackerService) DefinitionHistory(key string, page, pageSize int) (TaskPage, error) {
	if spec, ok := taskDefinitionSpecForKey(key); ok {
		return t.listFiltered(spec.filter, page, pageSize)
	}
	return TaskPage{}, ErrTaskDefinitionNotFound
}

func TaskDefinitionSchedulerJob(key string) (string, bool) {
	if spec, ok := taskDefinitionSpecForKey(key); ok && spec.schedulerJob != "" {
		return spec.schedulerJob, true
	}
	return "", false
}

func (t *TaskTrackerService) listFiltered(filter repository.TaskExecutionFilter, page, pageSize int) (TaskPage, error) {
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
		filtered := make([]BackgroundTask, 0, len(items))
		for _, item := range items {
			if taskMatchesFilter(item, filter) {
				filtered = append(filtered, item)
			}
		}
		start := min((page-1)*pageSize, len(filtered))
		end := min(start+pageSize, len(filtered))
		return TaskPage{Items: filtered[start:end], Page: page, PageSize: pageSize, Total: int64(len(filtered))}, nil
	}
	rows, total, err := t.repo.ListFiltered(t.context(), filter, (page-1)*pageSize, pageSize)
	if err != nil {
		return TaskPage{}, err
	}
	items := make([]BackgroundTask, 0, len(rows))
	for _, row := range rows {
		items = append(items, backgroundFromTaskExecution(row))
	}
	return TaskPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func taskMatchesFilter(task BackgroundTask, filter repository.TaskExecutionFilter) bool {
	return (filter.Kind == "" || task.Kind == filter.Kind) &&
		(filter.Name == "" || task.Name == filter.Name) &&
		(filter.NamePrefix == "" || strings.HasPrefix(task.Name, filter.NamePrefix)) &&
		(filter.ExcludeNamePrefix == "" || !strings.HasPrefix(task.Name, filter.ExcludeNamePrefix)) &&
		(filter.Status == "" || task.Status == filter.Status) &&
		(filter.ExcludeStatus == "" || task.Status != filter.ExcludeStatus) &&
		(filter.SourcePath == "" || task.SourcePath == filter.SourcePath) &&
		(filter.ExcludeSourcePath == "" || task.SourcePath != filter.ExcludeSourcePath)
}
