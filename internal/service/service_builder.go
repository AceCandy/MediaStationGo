package service

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type serviceContainerBuilder struct {
	cfg   *config.Config
	log   *zap.Logger
	repos *repository.Container
	c     *Container
}

func newServiceContainer(cfg *config.Config, log *zap.Logger, repos *repository.Container) *Container {
	ApplyRuntimeSettings(context.Background(), cfg, repos, log)

	builder := &serviceContainerBuilder{
		cfg:   cfg,
		log:   log,
		repos: repos,
		c: &Container{
			Cfg:  cfg,
			Log:  log,
			Repo: repos,
		},
	}
	builder.startRealtimeServices()
	builder.initProviderServices()
	builder.initContentServices()
	builder.initAccessAndStorageServices()
	builder.initIdentityServices()
	builder.initImageProxy()
	builder.attachRuntimeContext()
	builder.c.MediaProbe.SetTaskTracker(log, builder.c.Tasks, builder.c.stopCtx)
	return builder.c
}

func (b *serviceContainerBuilder) startRealtimeServices() {
	b.c.WSHub = NewHub(b.log)
	go b.c.WSHub.Run()
	b.c.Tasks = NewTaskTrackerService(b.log, b.c.WSHub)
	b.c.Tasks.ConfigurePersistence(b.repos.TaskExecution, b.cfg.App.DataDir)

	b.c.SSEHub = NewSSEHub(b.log)
	go b.c.SSEHub.Run()
	b.c.PlayerLogs = NewPlayerRequestLogService(b.repos.PlayerLog, b.c.SSEHub)
}

func (b *serviceContainerBuilder) initProviderServices() {
	b.c.FFprobe = NewFFprobeService(b.cfg, b.log)
	b.c.Cache = NewRuntimeCacheService(b.cfg, b.log)
	b.configureMediaSearchBackend()

	b.c.Crypto = NewCryptoService(b.cfg.Secrets.JWTSecret, b.log)
	b.c.APIConfig = NewAPIConfigService(b.log, b.repos, b.c.Crypto)
	b.c.ProxyPool = NewProxyPoolService(b.repos, b.c.Crypto)
	b.c.TMDb = NewTMDbProvider(b.cfg, b.log, b.c.APIConfig)
	b.c.Bangumi = NewBangumiProvider(b.cfg, b.log)
	b.c.TheTVDB = NewTheTVDBProvider(b.cfg, b.log)
	b.c.Douban = NewDoubanProvider(b.c.APIConfig)
	b.c.Douban.setProxyPool(b.c.ProxyPool)
	b.c.Fanart = NewFanartProvider(b.cfg, b.log)
	b.c.RecognitionWords = NewRecognitionWordsService(b.log, b.repos)

	adult := NewAdultProvider(b.log, b.c.APIConfig)
	b.c.Scraper = NewScraperService(
		b.cfg, b.log, b.repos,
		b.c.TMDb, b.c.Bangumi, b.c.TheTVDB, b.c.Fanart,
		b.c.WSHub, adult,
	)
	b.c.Scraper.SetRuntimeCache(b.c.Cache)
	b.c.Scraper.SetDouban(b.c.Douban)
	b.c.Scraper.SetTaskTracker(b.c.Tasks)
}

func (b *serviceContainerBuilder) configureMediaSearchBackend() {
	searchBackend := repository.NewOpenSearchMediaBackend(b.cfg.Search)
	if searchBackend == nil || b.repos == nil || b.repos.MediaView == nil {
		return
	}
	b.repos.MediaView.SetSearchBackend(searchBackend)
	if b.log != nil {
		b.log.Info("opensearch metadata search enabled", zap.String("index", b.cfg.Search.Index), zap.String("url", b.cfg.Search.OpenSearchURL))
	}
}

func (b *serviceContainerBuilder) initContentServices() {
	b.c.Organizer = NewOrganizerService(b.cfg, b.log, b.repos)
	b.c.Organizer.SetProbe(b.c.FFprobe)
	b.c.Organizer.SetScraper(b.c.Scraper)
	b.c.Discover = NewDiscoverService(b.log, b.c.TMDb)
	b.c.Scan = NewScannerService(b.cfg, b.log, b.repos, b.c.WSHub, b.c.FFprobe, b.c.Scraper)
	b.c.Scan.SetRuntimeCache(b.c.Cache)
	b.c.OrganizePipeline = NewOrganizePipelineService(b.log, b.repos, b.c.Organizer, b.c.Scan, b.c.Tasks)
	b.c.Watcher = NewWatcherService(b.log, b.repos, b.c.Scan, b.c.Tasks)
	b.c.AI = NewAIService(b.cfg, b.log, b.c.APIConfig)
	b.c.Scraper.SetAI(b.c.AI)
	b.c.Duplicate = NewDuplicateService(b.log, b.repos, b.c.WSHub)
	b.c.FileManager = NewFileManagerService(b.cfg, b.log, b.repos)
	b.c.DLNA = NewDLNAService(b.log)
	b.c.Storage = NewStorageService(b.cfg, b.log, b.repos)
	b.c.Emby = NewEmbyService(b.cfg, b.log, b.repos)
	b.c.Notifier = NewNotifierService(b.log, b.repos)
	b.c.NotifyChannels = NewNotifyChannelService(b.log, b.repos)
	b.c.Scan.SetNotifyChannels(b.c.NotifyChannels)
	b.c.Scraper.SetNotifyChannels(b.c.NotifyChannels)
	b.c.Media = NewMediaService(b.cfg, b.log, b.repos).SetRuntimeCache(b.c.Cache).SetTMDbProvider(b.c.TMDb)
	b.c.Stream = NewStreamService(b.cfg, b.log, b.repos)
	b.c.Playback = NewPlaybackService(b.log, b.repos)
	b.c.Subtitle = NewSubtitleService(b.log, b.repos)
	b.c.Emby.SetSubtitle(b.c.Subtitle)
	b.c.Stats = NewStatsService(b.log, b.repos).SetRuntimeCache(b.c.Cache)
	b.c.Profile = NewProfileService(b.log, b.repos)
	b.c.Audit = NewAuditService(b.log, b.repos)
}

func (b *serviceContainerBuilder) initAccessAndStorageServices() {
	b.c.PlayProfiles = NewPlayProfileService(b.log, b.repos)
	b.c.Permissions = NewPermissionService(b.log, b.repos)
	b.c.MediaProbe = NewMediaProbeService(b.repos, b.c.FFprobe).SetRuntimeCache(b.c.Cache)
	b.c.Media.SetMediaProbe(b.c.MediaProbe)
	b.c.Scan.SetMediaProbe(b.c.MediaProbe)
	b.c.Stream.SetMediaProbe(b.c.MediaProbe)
	b.c.Emby.SetMediaProbe(b.c.MediaProbe)
	b.c.Subtitle.SetMediaProbe(b.c.MediaProbe)
	b.c.STRM = NewSTRMService(b.log, b.repos, b.cfg)
	b.c.Emby.SetRuntimeCache(b.c.Cache)
	b.c.Assistant = NewAssistantService(b.log, b.repos, b.c.AI)
	b.c.Scheduler = NewSchedulerService(
		b.log, b.repos, b.c.Scan, b.c.Organizer, b.c.WSHub,
	)
	b.c.Scheduler.SetTaskTracker(b.c.Tasks)
	b.c.Scheduler.SetOrganizePipeline(b.c.OrganizePipeline)
}

func (b *serviceContainerBuilder) initIdentityServices() {
	b.c.Token = NewTokenService(b.cfg, b.log, b.repos)
	b.c.Auth = NewAuthService(b.cfg, b.log, b.repos, b.c.Token, b.c.Permissions)
	b.c.Sessions = NewSessionTrackerService(b.log)
	b.c.Device = NewDeviceService(b.log, b.repos)
	b.c.Device.SetSessionTracker(b.c.Sessions)
	b.c.Scheduler.SetPeriodicWorkers(b.c.Scraper, b.c.Device)
	b.c.TelegramBot = NewTelegramBotService(b.log, b.repos, b.c.Crypto, b.c.Auth)
	b.c.TelegramBot.SetDeviceService(b.c.Device)
	// Device enforcement notifies users through their Telegram binding before destructive actions.
	b.c.Device.SetNotifier(b.c.TelegramBot.NotifyUserByID)
	b.c.ApiConfig = NewApiConfigService(b.cfg, b.log, b.repos, b.c.Crypto)
	b.c.Notify = NewNotifyService(b.log, b.repos, b.c.Crypto)
}

func (b *serviceContainerBuilder) initImageProxy() {
	b.c.ImageProxy = NewImageProxy(b.cfg, b.log)
	b.c.ImageProxy.setAPIConfigService(b.c.APIConfig)
	b.c.ImageProxy.SetLibraryRootsProvider(b.libraryRoots)
	b.c.Artwork = NewArtworkStore(b.cfg, b.repos.Artwork, b.c.ImageProxy)
	b.c.PeopleImages = NewPeopleImageStore(b.cfg, b.repos.Person, b.c.ImageProxy)
	b.c.Media.SetArtworkStore(b.c.Artwork)
	b.c.Scan.SetImageProxy(b.c.ImageProxy)
	b.c.Scraper.SetImageProxy(b.c.ImageProxy)
	b.c.Scraper.SetArtworkStore(b.c.Artwork)
	b.c.Scraper.SetPeopleImageStore(b.c.PeopleImages)
	b.c.Discover.SetImageProxy(b.c.ImageProxy)
}

func (b *serviceContainerBuilder) libraryRoots() []string {
	libs, err := b.repos.Library.List(context.Background())
	if err != nil {
		return nil
	}
	roots := make([]string, 0, len(libs))
	for _, l := range libs {
		if len(l.Roots) > 0 {
			for _, root := range l.Roots {
				if !root.Enabled || strings.TrimSpace(root.Path) == "" {
					continue
				}
				roots = append(roots, resolveMappedDestinationPath(root.Path))
			}
			continue
		}
		if strings.TrimSpace(l.Path) != "" {
			roots = append(roots, resolveMappedDestinationPath(l.Path))
		}
	}
	return roots
}

func (b *serviceContainerBuilder) attachRuntimeContext() {
	b.c.stopCtx, b.c.stopCancel = context.WithCancel(context.Background())
}
