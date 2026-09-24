// Package service 包含 MediaStationGo 的业务逻辑。
// Handler 反序列化 HTTP 请求，调用 Service 方法，然后序列化响应。
// Services 拥有所有横切策略（认证、扫描等）且不直接处理 HTTP 类型。
package service

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// Container 持有在启动时初始化的每个服务。Handler 接收指向它的指针并选择相关字段。
type Container struct {
	Cfg              *config.Config
	Log              *zap.Logger
	Repo             *repository.Container
	WSHub            *Hub
	SSEHub           *SSEHub
	Tasks            *TaskTrackerService
	HongGuo          *HongGuoService
	HongGuoDownloads *HongGuoDownloadService
	Auth             *AuthService
	Media            *MediaService
	Scan             *ScannerService
	Stream           *StreamService
	FFprobe          *FFprobeService
	MediaProbe       *MediaProbeService
	TMDb             *TMDbProvider
	Bangumi          *BangumiProvider
	TheTVDB          *TheTVDBProvider
	Fanart           *FanartProvider
	Scraper          *ScraperService
	Discover         *DiscoverService
	Playback         *PlaybackService
	ImageProxy       *ImageProxy
	Artwork          *ArtworkStore
	PeopleImages     *PeopleImageStore
	Watcher          *WatcherService
	Subtitle         *SubtitleService
	Stats            *StatsService
	Profile          *ProfileService
	Audit            *AuditService
	AI               *AIService
	APIConfig        *APIConfigService
	ProxyPool        *ProxyPoolService
	Crypto           *CryptoService
	Duplicate        *DuplicateService
	FileManager      *FileManagerService
	DLNA             *DLNAService
	Scheduler        *SchedulerService
	Storage          *StorageService
	Emby             *EmbyService
	Notifier         *NotifierService
	NotifyChannels   *NotifyChannelService
	TelegramBot      *TelegramBotService
	PlayProfiles     *PlayProfileService
	Permissions      *PermissionService
	STRM             *STRMService
	Assistant        *AssistantService
	Organizer        *OrganizerService
	OrganizePipeline *OrganizePipelineService
	Douban           *DoubanProvider
	Token            *TokenService
	ApiConfig        *ApiConfigService
	Notify           *NotifyService
	Device           *DeviceService
	Cache            *RuntimeCacheService
	Sessions         *SessionTrackerService
	RecognitionWords *RecognitionWordsService
	PlayerLogs       *PlayerRequestLogService
	Startup          *StartupState

	stopCtx    context.Context
	stopCancel context.CancelFunc
	bootMu     sync.Mutex
	bootWG     sync.WaitGroup
	booted     bool
	closing    bool
}

// New 构建服务容器。
func New(cfg *config.Config, log *zap.Logger, repos *repository.Container) *Container {
	return newServiceContainer(cfg, log, repos)
}

// Boot 启动后台工作进程（watcher、媒体扫描与调度任务）。
// 在 AutoMigrate 后调用一次。
func (c *Container) Boot() {
	c.bootMu.Lock()
	if c.booted || c.closing {
		c.bootMu.Unlock()
		return
	}
	c.booted = true
	c.bootWG.Add(1)
	c.bootMu.Unlock()
	defer c.bootWG.Done()
	ready := false
	defer func() {
		if !ready {
			c.Startup.finish("failed")
		}
	}()
	if c.Tasks != nil {
		if err := c.startupStep("恢复任务执行状态", func() error { return c.Tasks.Recover(c.stopCtx) }); err != nil {
			return
		}
	}
	if c.HongGuoDownloads != nil {
		_ = c.startupStep("启动红果下载服务", func() error { c.HongGuoDownloads.Start(c.stopCtx); return nil })
	}
	if err := c.startupStep("检查媒体库路径", func() error { return c.NormalizeLocalLibraryPaths(c.stopCtx) }); err != nil {
		return
	}
	_ = c.startupStep("建立媒体库目录监听", func() error { return c.Watcher.Start(c.stopCtx) })
	_ = c.startupStep("加载资料源默认配置", func() error { return c.APIConfig.SeedDefaults(c.stopCtx) })
	if c.MediaProbe != nil {
		_ = c.startupStep("启动媒体轨道回填", func() error { c.MediaProbe.WakeBackfill(); return nil })
	}
	if c.Scraper != nil {
		_ = c.startupStep("启动资料快照回填", func() error { return c.Scraper.StartTMDbSnapshotBackfill(c.stopCtx, true) })
		_ = c.startupStep("恢复资料刮削队列", func() error { return c.Scraper.StartCatalogHydrationWorker(c.stopCtx) })
		_ = c.startupStep("启动资料图片下载", func() error { c.Scraper.StartCatalogArtworkWorker(c.stopCtx); return nil })
		_ = c.startupStep("启动剧集本地资料纠正", func() error { return c.Scraper.StartSeriesLocalCorrection(c.stopCtx, true) })
	}
	_ = c.startupStep("启动搜索索引预热", func() error { go c.warmMediaSearchIndex(c.stopCtx); return nil })

	// 全部同步初始化结束后才注册任务，避免手动执行与恢复过程交叉。
	if err := c.startupStep("启动任务调度器", func() error { c.Scheduler.Start(c.stopCtx); return c.stopCtx.Err() }); err != nil {
		return
	}
	c.Startup.finish("ready")
	ready = true
}

// Context is canceled when the service container is closing.
func (c *Container) Context() context.Context {
	if c == nil || c.stopCtx == nil {
		return context.Background()
	}
	return c.stopCtx
}

// Close 释放 services 持有的任何资源（websocket hub、fsnotify、后台轮询器）。
func (c *Container) Close() {
	c.bootMu.Lock()
	c.closing = true
	if c.stopCancel != nil {
		c.stopCancel()
	}
	c.bootMu.Unlock()
	c.bootWG.Wait()
	if c.Scheduler != nil {
		c.Scheduler.Stop()
	}
	if c.HongGuo != nil {
		c.HongGuo.Wait()
	}
	if c.HongGuoDownloads != nil {
		c.HongGuoDownloads.Wait()
	}
	if c.Scraper != nil {
		c.Scraper.WaitCatalogHydrationWorker()
	}
	if c.Watcher != nil {
		c.Watcher.Stop()
	}
	if c.Cache != nil {
		_ = c.Cache.Close()
	}
	if c.WSHub != nil {
		c.WSHub.Stop()
	}
	if c.SSEHub != nil {
		c.SSEHub.Stop()
	}
}
