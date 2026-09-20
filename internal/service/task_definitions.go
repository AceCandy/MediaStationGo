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
	TaskDefinitionCatalogScrape              = "catalog_scrape"
	TaskDefinitionPeopleBackfill             = "people_backfill"
	TaskDefinitionPeopleTranslation          = "people_translation"
	TaskDefinitionTMDbArtworkLocalRepair     = "tmdb_artwork_local_repair"
	TaskDefinitionTMDbArtworkMissingRecheck  = "tmdb_artwork_missing_recheck"
	TaskDefinitionDoubanArtworkLocalRepair   = "douban_artwork_local_repair"
	TaskDefinitionTMDbEpisodeMetadataRecheck = "tmdb_episode_metadata_recheck"
	TaskDefinitionDoubanEnrichment           = "douban_movie_enrichment"
	TaskDefinitionAccountCleanup             = "account_cleanup"
	TaskDefinitionTMDbSnapshotBackfill       = "tmdb_snapshot_backfill"
	TaskDefinitionSeriesLocalCorrection      = "series_local_correction"
)

var ErrTaskDefinitionNotFound = errors.New("task definition not found")

// LibraryScanTaskKind 按资料边界隔离执行记录，扫描器仍共用。
func LibraryScanTaskKind(lib *model.Library) string {
	if libraryUsesNFOOnly(lib) {
		return TaskKindNFOScan
	}
	return TaskKindScan
}

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
	{TaskDefinition: TaskDefinition{Key: TaskKindNFOScan, Name: "非常规媒体库扫描", Description: "扫描本地文件和 NFO，不进行网络资料匹配；定时执行沿用公共媒体库扫描周期", Trigger: "定时 / 手动 / 新增后自动", Action: "library_scan"}, filter: repository.TaskExecutionFilter{Kind: TaskKindNFOScan}},
	{TaskDefinition: TaskDefinition{Key: TaskKindNFOWatch, Name: "非常规媒体库变更监听", Description: "处理非常规媒体库的本地文件变更", Trigger: "文件事件"}, filter: repository.TaskExecutionFilter{Kind: TaskKindNFOWatch}},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoAlbum, Name: "红果官方合集补充", Description: "分批补充历史作品的官方合集和季号；已检查项跳过，失败冷却后重试，可取消后续跑", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoAlbum}, schedulerJob: TaskKindHongGuoAlbum},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoDownload, Name: "红果视频下载", Description: "从发现页发起，校验完整视频后发布到下载输出目录", Trigger: "手动"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoDownload}},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoSupplement, Name: "红果补充下载", Description: "按上线时间选取资料齐全且从未入队的作品；每轮新增指定数量，非维持队列数量", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoSupplement}, schedulerJob: TaskKindHongGuoSupplement},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoSync, Name: "红果作品发现", Description: "从检查点持续翻页至各分类结束，保存目录摘要；不抓详情，可与资料刷新并行", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoSync}, schedulerJob: TaskKindHongGuoSync},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoRefresh, Name: "红果资料刷新", Description: "优先补齐本轮开始前的新作品，再重试失败和刷新超过 24 小时的已有资料", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoRefresh}, schedulerJob: TaskKindHongGuoRefresh},
	{TaskDefinition: TaskDefinition{Key: TaskKindHongGuoArtwork, Name: "红果图片下载", Description: "下载海报和人物头像，失败保留独立重试状态", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindHongGuoArtwork}, schedulerJob: TaskKindHongGuoArtwork},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionSeriesLocalCorrection, Name: "剧集本地资料纠正", Description: "用已有 TMDb 快照纠正季与集的名称、简介，跳过手工及并发修改，不联网", Trigger: "规则更新后一次 / 手动", Action: "series_local_correction"}, filter: repository.TaskExecutionFilter{Kind: TaskKindSeriesLocalCorrection}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionOrganize, Name: "媒体整理", Description: "整理、重命名并入库下载目录内容", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindOrganize}, schedulerJob: "organize_source"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionLibraryScan, Name: "媒体库扫描", Description: "扫描媒体库并同步入库变化", Trigger: "定时 / 手动 / 新增后自动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindScan}, schedulerJob: "library_scan"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionLibraryWatch, Name: "媒体库变更监听", Description: "监听本地媒体文件变化并增量同步入库", Trigger: "文件事件"}, filter: repository.TaskExecutionFilter{Kind: TaskKindWatch}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionProbeBackfill, Name: "媒体轨道回填", Description: "补充缺少完整探测信息的媒体轨道，剧集仅手动回填", Trigger: "入库后自动（剧集除外） / 手动", Action: "probe_backfill"}, filter: repository.TaskExecutionFilter{Kind: TaskKindProbe}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionTMDbSnapshotBackfill, Name: "TMDB 快照回填", Description: "补齐有 TMDB 标识但缺少原始详情快照的元数据", Trigger: "启动时补缺 / 手动", Action: "tmdb_snapshot_backfill"}, filter: repository.TaskExecutionFilter{Kind: TaskKindTMDbSnapshotBackfill}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionMediaScrape, Name: "媒体入库刮削", Description: "处理已入库但尚未成功刮削的媒体", Trigger: "事件 / 手动", Action: "media_scrape"}, filter: repository.TaskExecutionFilter{Kind: TaskKindScrape, ExcludeNamePrefix: "发现目录刮削："}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionCatalogScrape, Name: "作品资料补全", Description: "补全作品资料与季集信息，图片交给后台下载", Trigger: "事件触发"}, filter: repository.TaskExecutionFilter{Kind: TaskKindScrape, NamePrefix: "发现目录刮削："}},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionPeopleBackfill, Name: "人物信息补齐", Description: "补齐尚未获取演职员信息的元数据", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindPeople, Name: "人物信息补齐"}, schedulerJob: "people_backfill_periodic"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionPeopleTranslation, Name: "人物翻译", Description: "逐步翻译尚无中文名称和角色名的人物", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindPeople, Name: "人物翻译"}, schedulerJob: "people_translation_periodic"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionTMDbArtworkLocalRepair, Name: "TMDb 图片下载与修复", Description: "自动下载已入库资料的图片与头像；定时或手动巡检修复本地缺失图片", Trigger: "资料入库后自动 / 定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindArtwork, Name: "TMDb 图片本地化修复"}, schedulerJob: "tmdb_artwork_local_repair"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionTMDbArtworkMissingRecheck, Name: "TMDb 无图复查", Description: "仅复查电影和整剧缺失的 TMDb 海报、背景图；季海报和集图片由季/集信息补全/复查处理", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindArtwork, Name: "TMDb 无图复查"}, schedulerJob: "tmdb_artwork_missing_recheck"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionDoubanArtworkLocalRepair, Name: "豆瓣图片本地化修复", Description: "升级豆瓣小海报并恢复本地文件缺失的豆瓣候选", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindArtwork, Name: "豆瓣图片本地化修复"}, schedulerJob: "douban_artwork_local_repair"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionTMDbEpisodeMetadataRecheck, Name: "TMDb 季/集信息补全/复查", Description: "补全有媒体资源的季与集：简介、播出日期及季海报/集图片，并补齐缺失的季标识和快照；标题仅顺带更新，不单独触发复查", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindArtwork, Name: "TMDb 集信息补全/复查"}, schedulerJob: "tmdb_episode_metadata_recheck"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionDoubanEnrichment, Name: "豆瓣信息补齐", Description: "缓慢补齐已有唯一豆瓣 ID 的电影和整剧信息及海报候选，不修改季和集", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindArtwork, Name: "豆瓣电影信息补齐"}, schedulerJob: "douban_movie_enrichment"},
	{TaskDefinition: TaskDefinition{Key: TaskDefinitionAccountCleanup, Name: "账号清理巡检", Description: "按保号规则检查并清理不符合条件的账号", Trigger: "定时 / 手动", Action: "scheduler"}, filter: repository.TaskExecutionFilter{Kind: TaskKindCleanup, Name: "账号清理巡检"}, schedulerJob: "account_cleanup"},
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
		if spec.Key == TaskKindHongGuoDownload {
			continue
		}
		definition := spec.TaskDefinition
		definition.System = model.TaskSystemForKind(spec.filter.Kind)
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
	system := task.System
	if system == "" {
		system = model.TaskSystemForKind(task.Kind)
	}
	return (filter.System == "" || system == filter.System) && (filter.Kind == "" || task.Kind == filter.Kind) &&
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
