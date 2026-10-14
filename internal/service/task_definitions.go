package service

import (
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	TaskDefinitionOrganize                   = "organize"
	TaskDefinitionLibraryScan                = "library_scan"
	TaskDefinitionLibraryWatch               = "library_watch"
	TaskDefinitionProbeBackfill              = "probe_backfill"
	TaskDefinitionMediaScrape                = "media_scrape"
	TaskDefinitionPeopleTranslation          = "people_translation"
	TaskDefinitionTMDbArtworkLocalRepair     = "tmdb_artwork_local_repair"
	TaskDefinitionTMDbArtworkMissingRecheck  = "tmdb_artwork_missing_recheck"
	TaskDefinitionDoubanArtworkLocalRepair   = "douban_artwork_local_repair"
	TaskDefinitionTMDbEpisodeMetadataRecheck = "tmdb_episode_metadata_recheck"
	TaskDefinitionDoubanDataRefresh          = "douban_data_refresh"
	TaskDefinitionDoubanEnrichment           = "douban_movie_enrichment"
	TaskDefinitionAccountCleanup             = "account_cleanup"
	TaskDefinitionSeriesLocalCorrection      = "series_local_correction"
)

var ErrTaskDefinitionNotFound = errors.New("task definition not found")

type TaskDefinition struct {
	System         string              `json:"system"`
	Key            string              `json:"key"`
	Name           string              `json:"name"`
	Description    string              `json:"description"`
	Trigger        string              `json:"trigger"`
	Schedule       string              `json:"schedule,omitempty"`
	CurrentState   string              `json:"current_state"`
	NextRun        *time.Time          `json:"next_run,omitempty"`
	Action         string              `json:"action,omitempty"`
	Current        *BackgroundTask     `json:"current,omitempty"`
	Latest         *BackgroundTask     `json:"latest,omitempty"`
	ScheduleConfig *TaskScheduleConfig `json:"schedule_config,omitempty"`
}

type TaskScheduleConfig struct {
	Count              int   `json:"count,omitempty"`
	Enabled            bool  `json:"enabled"`
	IntervalSeconds    int64 `json:"interval_seconds"`
	MinIntervalSeconds int64 `json:"min_interval_seconds"`
	MaxIntervalSeconds int64 `json:"max_interval_seconds"`
}

type taskDefinitionSpec struct {
	TaskDefinition
	filter       repository.TaskExecutionFilter
	schedulerJob string
}

var taskDefinitionSpecs = []taskDefinitionSpec{
	{TaskDefinition: TaskDefinition{Key: TaskKindTMDbDataSupplement, Name: "TMDb 资料补全", Description: "处理发现页提交的缺失来源资料；共享缓存、限速及失败退避，与媒体项无关", Trigger: "浏览详情后自动"}, filter: repository.TaskExecutionFilter{Kind: TaskKindTMDbDataSupplement}},
	{TaskDefinition: TaskDefinition{Key: TaskKindDoubanDataSupplement, Name: "豆瓣资料补全", Description: "补齐缺失基础资料和全量演职员，仅调用 API；共享缓存、限速及失败退避", Trigger: "浏览详情后自动"}, filter: repository.TaskExecutionFilter{Kind: TaskKindDoubanDataSupplement}},
	{TaskDefinition: TaskDefinition{Key: TaskKindHuangGuoAISupplement, Name: "黄果 AI 补充下载", Description: "按本地发现时间选取资料齐全且从未入队的作品；每轮新增指定数量，非维持队列数量", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHuangGuoAISupplement}, schedulerJob: TaskKindHuangGuoAISupplement},
	{TaskDefinition: TaskDefinition{Key: TaskKindHuangGuoAIDownload, Name: "黄果 AI 视频下载", Description: "完整校验后发布到下载目录", Trigger: "手动"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHuangGuoAIDownload}},
	{TaskDefinition: TaskDefinition{Key: TaskKindHuangGuoAISync, Name: "黄果 AI 作品发现", Description: "分页扫描四类作品，保存摘要与断点", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHuangGuoAISync}, schedulerJob: TaskKindHuangGuoAISync},
	{TaskDefinition: TaskDefinition{Key: TaskKindHuangGuoAIRefresh, Name: "黄果 AI 资料刷新", Description: "补齐新作品，重试失败资料，刷新未完结作品", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHuangGuoAIRefresh}, schedulerJob: TaskKindHuangGuoAIRefresh},
	{TaskDefinition: TaskDefinition{Key: TaskKindHuangGuoAIArtwork, Name: "黄果 AI 图片下载", Description: "保存本地海报，失败保留旧图并独立重试", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHuangGuoAIArtwork}, schedulerJob: TaskKindHuangGuoAIArtwork},
	{TaskDefinition: TaskDefinition{Key: TaskKindHuangGuoAIRank, Name: "黄果 AI 排行榜刷新", Description: "原子替换热播、推荐与潜力榜，保留官网顺序", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHuangGuoAIRank}, schedulerJob: TaskKindHuangGuoAIRank},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoAlbum, Name: "红果官方合集补充", Description: "分批补充历史作品的官方合集和季号；已检查项跳过，失败冷却后重试，可取消后续跑", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoAlbum}, schedulerJob: TaskKindHongGuoAlbum},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoDownload, Name: "红果视频下载", Description: "从发现页发起，校验完整视频后发布到下载输出目录", Trigger: "手动"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoDownload}},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoSupplement, Name: "红果补充下载", Description: "按上线时间选取资料齐全且从未入队的作品；每轮新增指定数量，非维持队列数量", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoSupplement}, schedulerJob: TaskKindHongGuoSupplement},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoSync, Name: "红果作品发现", Description: "扫描分类，只统计真正新发现的作品；详情交由资料刷新补齐", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoSync}, schedulerJob: TaskKindHongGuoSync},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoRank, Name: "红果排行榜刷新", Description: "独立更新四类官方榜单，完整拉取后原子替换并保留官网顺序", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoRank}, schedulerJob: TaskKindHongGuoRank},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoRefresh, Name: "红果资料刷新", Description: "补齐新作品并重试失败项；仅定期刷新超过 24 小时仍未完结的作品", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoRefresh}, schedulerJob: TaskKindHongGuoRefresh},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoArtwork, Name: "红果图片下载", Description: "下载海报和人物头像，失败保留独立重试状态", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoArtwork}, schedulerJob: TaskKindHongGuoArtwork},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionLibraryScan, Name: "媒体库扫描", Description: "扫描媒体库并同步入库变化", Trigger: "定时 / 手动 / 新增后自动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindScan}, schedulerJob: "library_scan"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionLibraryWatch, Name: "媒体库变更监听", Description: "监听本地媒体文件变化并增量同步入库", Trigger: "文件事件"}, filter: repository.TaskExecutionFilter{Kind: TaskKindWatch}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionProbeBackfill, Name: "媒体轨道回填", Description: "补充缺少完整探测信息的媒体轨道，剧集仅手动回填", Trigger: "入库后自动（剧集除外） / 手动", Action: "probe_backfill"}, filter: repository.TaskExecutionFilter{Kind: TaskKindProbe}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionMediaScrape, Name: "媒体入库刮削", Description: "处理已入库但尚未成功刮削的媒体", Trigger: "事件 / 手动", Action: "media_scrape"}, filter: repository.TaskExecutionFilter{Kind: TaskKindScrape, ExcludeNamePrefix: "发现目录刮削："}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionPeopleTranslation, Name: "人物翻译", Description: "逐步翻译尚无中文名称和角色名的人物", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindPeople, Name: "人物翻译"}, schedulerJob: "people_translation_periodic"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionAccountCleanup, Name: "账号清理巡检", Description: "按保号规则检查并清理不符合条件的账号", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindCleanup, Name: "账号清理巡检"}, schedulerJob: "account_cleanup"},
}

func init() {
	for _, spec := range catalogMaintenanceTasks {
		description := "补全到期且基础资料不全的来源条目，齐全后停止；手动运行仍只处理补缺候选"
		if spec.people {
			description = "按 TMDb 来源补齐未获取的演职员和人物详情；不依赖媒体文件，空列表不重复抓取"
		}
		if spec.rating {
			description = "独立更新到期评分，保留已有标题、简介、图片和基础资料检查时间"
		}
		if spec.inventory {
			description = "检查活跃电视剧和季的新增季集，基础资料齐全不影响目录检查"
		}
		taskDefinitionSpecs = append(taskDefinitionSpecs, taskDefinitionSpec{TaskDefinition: TaskDefinition{Key: spec.key, Name: spec.name, Description: description, Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: spec.key}, schedulerJob: spec.key})
	}
	for _, spec := range []struct{ key, name string }{{TaskKindTMDbImages, "TMDb 图片下载与修复"}, {TaskKindDoubanImages, "豆瓣图片下载与修复"}} {
		taskDefinitionSpecs = append(taskDefinitionSpecs, taskDefinitionSpec{TaskDefinition: TaskDefinition{Key: spec.key, Name: spec.name, Description: "维护资料中心图片资产；下载失败独立重试，不刷新完整详情", Trigger: "资料入库后自动 / 定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: spec.key}, schedulerJob: spec.key})
	}
}

func isTaskDefinitionKey(key string) bool {
	_, ok := taskDefinitionSpecForKey(key)
	return ok
}

func TaskDefinitionExists(key string) bool {
	return isTaskDefinitionKey(key)
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
	return t.DefinitionsForSystem(scheduler, "")
}

func (t *TaskTrackerService) DefinitionsForSystem(scheduler []JobStatus, system string) ([]TaskDefinition, error) {
	statuses := make(map[string]JobStatus, len(scheduler))
	for _, status := range scheduler {
		statuses[status.Name] = status
	}
	definitions := make([]TaskDefinition, 0, len(taskDefinitionSpecs))
	active := t.memorySnapshot().Active
	for _, spec := range taskDefinitionSpecs {
		// 下载执行记录由下载空间展示，保留定义以兼容历史与日志接口。
		if spec.Key == TaskKindHongGuoDownload || spec.Key == TaskKindHuangGuoAIDownload {
			continue
		}
		definition := spec.TaskDefinition
		definition.System = model.TaskSystemForExecution(spec.filter.Kind, spec.filter.Name)
		if system != "" && definition.System != system {
			continue
		}
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
				current := task
				definition.Current = &current
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
			if status.Configurable {
				definition.ScheduleConfig = &TaskScheduleConfig{
					Count:   status.Count,
					Enabled: status.Enabled, IntervalSeconds: status.IntervalSeconds,
					MinIntervalSeconds: status.MinIntervalSeconds, MaxIntervalSeconds: status.MaxIntervalSeconds,
				}
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
	if spec, ok := taskDefinitionSpecForKey(key); ok && spec.schedulerJob != "" && spec.Action == "scheduler" {
		return spec.schedulerJob, true
	}
	return "", false
}

func TaskDefinitionScheduleJob(key string) (string, bool) {
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
	return (filter.System == "" || task.System == filter.System) && (filter.Kind == "" || task.Kind == filter.Kind) &&
		(filter.Name == "" || task.Name == filter.Name) &&
		(filter.NamePrefix == "" || strings.HasPrefix(task.Name, filter.NamePrefix)) &&
		(filter.ExcludeNamePrefix == "" || !strings.HasPrefix(task.Name, filter.ExcludeNamePrefix)) &&
		(filter.Status == "" || task.Status == filter.Status) &&
		(filter.ExcludeStatus == "" || task.Status != filter.ExcludeStatus) &&
		(filter.SourcePath == "" || task.SourcePath == filter.SourcePath) &&
		(filter.ExcludeSourcePath == "" || task.SourcePath != filter.ExcludeSourcePath)
}

func (t *TaskTrackerService) ListSystem(system string, page, pageSize int) (TaskPage, error) {
	return t.listFiltered(repository.TaskExecutionFilter{System: system}, page, pageSize)
}
