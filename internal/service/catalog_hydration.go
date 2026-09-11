package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const (
	catalogRootBurst           = 4
	catalogMaxAttempts         = 8
	autoMediaScrapeWorkerCount = 3
)

var catalogURLPattern = regexp.MustCompile(`https?://[^\s]+`)

// StartCatalogHydrationWorker 启动发现页目录抓取 worker。
func (s *ScraperService) StartCatalogHydrationWorker(ctx context.Context) {
	if s == nil || s.repo == nil || s.repo.Metadata == nil {
		return
	}
	s.catalogHydrationOnce.Do(func() {
		if s.catalogHydrationWake == nil {
			s.catalogHydrationWake = make(chan struct{}, 1)
		}
		if s.mediaScrapeWake == nil {
			s.mediaScrapeWake = make(chan struct{}, autoMediaScrapeWorkerCount)
		}
		if err := s.recoverRunningMediaScrapes(ctx); err != nil {
			if s.log != nil {
				s.log.Warn("recover running media scrapes failed", zap.Error(err))
			}
			return
		}
		if err := s.repo.Metadata.RecoverCatalogJobs(ctx); err != nil {
			if s.log != nil {
				s.log.Warn("recover catalog hydration jobs failed", zap.Error(err))
			}
			return
		}
		s.catalogHydrationWG.Add(autoMediaScrapeWorkerCount + 1)
		go s.runCatalogHydrationWorker(ctx)
	})
}

// WaitCatalogHydrationWorker 等待 worker 退出。
func (s *ScraperService) WaitCatalogHydrationWorker() {
	if s != nil {
		s.catalogHydrationWG.Wait()
	}
}

// QueueCatalogHydrationContext 幂等写入持久化任务后唤醒 worker。
func (s *ScraperService) QueueCatalogHydrationContext(ctx context.Context, items []ExternalMediaResult) error {
	if s == nil || len(items) == 0 || s.repo == nil || s.repo.Metadata == nil {
		return nil
	}
	queued := false
	for _, item := range items {
		if !isTMDbCatalogItem(item) {
			continue
		}
		entityKind, supported := catalogEntityKind(item.MediaType)
		if !supported {
			continue
		}
		if err := s.repo.Metadata.EnqueueCatalogJob(ctx, "tmdb", entityKind, strconv.Itoa(item.TMDbID)); err != nil {
			return err
		}
		queued = true
	}
	if !queued {
		return nil
	}
	s.wakeCatalogHydration()
	return nil
}

func (s *ScraperService) WakeScrapeWorker() {
	if s == nil {
		return
	}
	s.wakeMediaScrapeWorkers()
	s.wakeCatalogHydration()
}

func (s *ScraperService) wakeMediaScrapeWorkers() {
	if s.mediaScrapeWake == nil {
		s.mediaScrapeWake = make(chan struct{}, autoMediaScrapeWorkerCount)
	}
	for range autoMediaScrapeWorkerCount {
		select {
		case s.mediaScrapeWake <- struct{}{}:
		default:
			return
		}
	}
}

func (s *ScraperService) wakeCatalogHydration() {
	if s.catalogHydrationWake == nil {
		s.catalogHydrationWake = make(chan struct{}, 1)
	}
	select {
	case s.catalogHydrationWake <- struct{}{}:
	default:
	}
}

func (s *ScraperService) runCatalogHydrationWorker(ctx context.Context) {
	defer s.catalogHydrationWG.Done()
	for range autoMediaScrapeWorkerCount {
		go s.runMediaScrapeWorker(ctx)
	}
	rootStreak := 0
	for ctx.Err() == nil {
		active, err := s.hasActiveMediaScrapes(ctx)
		if err != nil {
			if s.log != nil && ctx.Err() == nil {
				s.log.Warn("check pending media scrape failed", zap.Error(err))
			}
			if !waitForContext(ctx, time.Second) {
				return
			}
			continue
		}
		if active {
			if !s.waitForMediaScrapes(ctx) {
				return
			}
			continue
		}
		job, err := s.claimNextCatalogJob(ctx, &rootStreak)
		if err != nil {
			if s.log != nil && ctx.Err() == nil {
				s.log.Warn("claim catalog hydration job failed", zap.Error(err))
			}
			if !waitForContext(ctx, time.Second) {
				return
			}
			continue
		}
		if job == nil {
			if !s.waitCatalogHydration(ctx) {
				return
			}
			continue
		}
		var task *TaskHandle
		if s.tasks != nil {
			kindName := "电影"
			if job.EntityKind == model.MetadataKindSeries {
				kindName = "电视剧"
			}
			task = s.tasks.StartTriggered(TaskKindScrape, TaskTriggerEvent, "发现目录刮削："+kindName+" "+job.ExternalID, TaskUpdate{
				Stage: "waiting", SourcePath: "catalog", Message: "等待刮削资源",
			})
			if task == nil {
				_ = s.finishCatalogFailure(ctx, job, errors.New("create scrape task execution failed"))
				continue
			}
		}
		artworkMetrics := &catalogArtworkMetrics{task: task}
		runErr := s.processCatalogJobWithMetrics(ctx, job, artworkMetrics)
		if task != nil {
			safeErr := sanitizeCatalogError(runErr)
			metrics := map[string]int64{
				"processed":     1,
				"artwork_saved": artworkMetrics.Saved, "artwork_existing": artworkMetrics.Existing,
				"artwork_source_missing": artworkMetrics.SourceMissing,
			}
			if runErr != nil {
				if job.Attempts >= catalogMaxAttempts {
					metrics["failed"] = 1
				} else {
					metrics["retry"] = 1
				}
			}
			task.Finish(safeErr, TaskUpdate{Stage: "completed", Message: "作品资料补全结束", Metrics: metrics, Details: []string{s.catalogScrapeTaskDetail(ctx, job, safeErr)}})
		}
		if runErr != nil && ctx.Err() == nil {
			safeErr := sanitizeCatalogError(runErr)
			_ = s.finishCatalogFailure(ctx, job, safeErr)
			if s.log != nil {
				s.log.Warn("catalog hydration failed", zap.String("provider", job.Provider), zap.String("entity_kind", job.EntityKind), zap.String("external_id", job.ExternalID), zap.Error(safeErr))
			}
		}
	}
}

func (s *ScraperService) finishCatalogFailure(ctx context.Context, job *model.CatalogHydrationJob, err error) error {
	message := sanitizeCatalogError(err).Error()
	if job != nil && job.Attempts >= catalogMaxAttempts {
		return s.repo.Metadata.FailCatalogJob(ctx, job.ID, message)
	}
	return s.repo.Metadata.RetryCatalogJob(ctx, job.ID, message, time.Now().UTC().Add(catalogRetryDelay(job.Attempts)))
}

func (s *ScraperService) runMediaScrapeWorker(ctx context.Context) {
	defer s.catalogHydrationWG.Done()
	for ctx.Err() == nil {
		processed, err := s.processNextMediaScrape(ctx)
		if err != nil {
			if s.log != nil && ctx.Err() == nil {
				s.log.Warn("process pending media scrape failed", zap.Error(err))
			}
			if !waitForContext(ctx, time.Second) {
				return
			}
			continue
		}
		if processed {
			s.wakeCatalogHydration()
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.mediaScrapeWake:
		}
	}
}

func (s *ScraperService) catalogScrapeTaskDetail(ctx context.Context, job *model.CatalogHydrationJob, runErr error) string {
	if job == nil {
		return "❌ 发现目录刮削: 任务信息缺失"
	}
	prefix := fmt.Sprintf("发现目录 %s %s %s（阶段 %s，第 %d 次尝试）", job.Provider, job.EntityKind, job.ExternalID, job.Stage, job.Attempts)
	if runErr != nil {
		return fmt.Sprintf("❌ %s: 刮削失败: %v", prefix, runErr)
	}
	title := ""
	if item, err := s.repo.Metadata.FindByIdentifier(ctx, job.Provider, job.EntityKind, job.ExternalID); err == nil && item != nil {
		title = strings.TrimSpace(item.Title)
		if title == "" {
			title = strings.TrimSpace(item.OriginalName)
		}
	}
	if title != "" {
		return fmt.Sprintf("✅ %s: 已刮削 %s", prefix, title)
	}
	return "✅ " + prefix + ": 刮削完成"
}

func (s *ScraperService) claimNextCatalogJob(ctx context.Context, rootStreak *int) (*model.CatalogHydrationJob, error) {
	now := time.Now().UTC()
	if *rootStreak >= catalogRootBurst {
		if job, err := s.repo.Metadata.ClaimCatalogJob(ctx, model.CatalogJobStageSeasons, now); err != nil || job != nil {
			if job != nil {
				*rootStreak = 0
			}
			return job, err
		}
		*rootStreak = 0
	}
	if job, err := s.repo.Metadata.ClaimCatalogJob(ctx, model.CatalogJobStageRoot, now); err != nil || job != nil {
		if job != nil {
			(*rootStreak)++
		}
		return job, err
	}
	job, err := s.repo.Metadata.ClaimCatalogJob(ctx, model.CatalogJobStageSeasons, now)
	if job != nil {
		*rootStreak = 0
	}
	return job, err
}

func (s *ScraperService) waitCatalogHydration(ctx context.Context) bool {
	next, err := s.repo.Metadata.NextCatalogAttemptAt(ctx)
	if err != nil {
		return waitForContext(ctx, time.Second)
	}
	var timer *time.Timer
	var timerC <-chan time.Time
	if next != nil {
		delay := time.Until(*next)
		if delay < 0 {
			delay = 0
		}
		timer = time.NewTimer(delay)
		timerC = timer.C
		defer timer.Stop()
	}
	select {
	case <-ctx.Done():
		return false
	case <-s.catalogHydrationWake:
		return true
	case <-timerC:
		return true
	}
}

func (s *ScraperService) waitForMediaScrapes(ctx context.Context) bool {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-s.catalogHydrationWake:
		return true
	case <-timer.C:
		return true
	}
}

func waitForContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *ScraperService) processCatalogJob(ctx context.Context, job *model.CatalogHydrationJob) error {
	return s.processCatalogJobWithMetrics(ctx, job, nil)
}

// yieldCatalogToMedia 在实体边界让出目录补全持有的写锁，保留当前任务与已完成检查点。
// 调用方必须持有 scrapeRunMu 写锁；返回前重新取得锁，维持外层解锁约定。
func (s *ScraperService) yieldCatalogToMedia(ctx context.Context, metrics *catalogArtworkMetrics) error {
	active, err := s.hasActiveMediaScrapes(ctx)
	if err != nil || !active {
		return err
	}
	s.scrapeRunMu.Unlock()
	defer func() {
		s.scrapeRunMu.Lock()
		if ctx.Err() == nil && metrics != nil {
			metrics.task.Update(TaskUpdate{Stage: "scrape", Message: "正在补全作品资料"})
		}
	}()
	if metrics != nil {
		metrics.task.Update(TaskUpdate{Stage: "waiting", Message: "等待媒体入库完成，暂停作品资料补全"})
	}
	s.wakeMediaScrapeWorkers()
	for active {
		if !s.waitForMediaScrapes(ctx) {
			return ctx.Err()
		}
		active, err = s.hasActiveMediaScrapes(ctx)
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *ScraperService) processCatalogJobWithMetrics(ctx context.Context, job *model.CatalogHydrationJob, metrics *catalogArtworkMetrics) error {
	if job == nil || job.Provider != "tmdb" {
		return errors.New("unsupported catalog hydration job")
	}
	tmdbID, err := strconv.Atoi(job.ExternalID)
	if err != nil || tmdbID <= 0 {
		return errors.New("invalid tmdb catalog id")
	}
	s.scrapeRunMu.Lock()
	defer s.scrapeRunMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if metrics != nil {
		metrics.task.Update(TaskUpdate{Stage: "scrape", Message: "正在补全作品资料"})
	}
	if job.EntityKind == model.MetadataKindSeries {
		return s.hydrateCatalogSeries(ctx, job, tmdbID, metrics)
	}
	return s.hydrateCatalogRoot(ctx, job, tmdbID, metrics)
}

func (s *ScraperService) hydrateCatalogSeries(ctx context.Context, job *model.CatalogHydrationJob, tmdbID int, metrics *catalogArtworkMetrics) error {
	if job.Stage != model.CatalogJobStageSeasons {
		if err := s.hydrateCatalogSeriesRoot(ctx, job, tmdbID, metrics); err != nil {
			return err
		}
	}
	series, err := s.catalogJobMetadata(ctx, job)
	if err != nil {
		return err
	}
	for {
		if err := s.yieldCatalogToMedia(ctx, metrics); err != nil {
			return err
		}
		season, err := s.repo.Metadata.FindIncompleteCatalogChild(ctx, series.ID, model.MetadataKindSeason)
		if err != nil {
			return err
		}
		if season == nil {
			break
		}
		if metrics != nil {
			metrics.task.Update(TaskUpdate{Stage: "seasons", Message: fmt.Sprintf("正在补全第 %d 季", season.SeasonNum)})
		}
		if err := s.hydrateCatalogSeason(ctx, series, season, tmdbID, metrics); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	if err := s.repo.Metadata.MarkCatalogCheckpoint(ctx, series.ID, "catalog_hydrated_at", now); err != nil {
		return err
	}
	return s.repo.Metadata.CompleteCatalogJob(ctx, job.ID, series.ID, now)
}

func (s *ScraperService) hydrateCatalogSeriesRoot(ctx context.Context, job *model.CatalogHydrationJob, tmdbID int, metrics *catalogArtworkMetrics) error {
	if err := s.hydrateCatalogRootData(ctx, job, tmdbID, metrics); err != nil {
		return err
	}
	item, err := s.repo.Metadata.FindByIdentifier(ctx, job.Provider, job.EntityKind, job.ExternalID)
	if err != nil {
		return err
	}
	if item == nil {
		return errors.New("catalog job metadata is missing")
	}
	if err := s.repo.Metadata.AdvanceRunningCatalogJob(ctx, job.ID, model.CatalogJobStageSeasons, item.ID); err != nil {
		return err
	}
	job.Stage = model.CatalogJobStageSeasons
	job.MetadataID = &item.ID
	return nil
}

func (s *ScraperService) hydrateCatalogRoot(ctx context.Context, job *model.CatalogHydrationJob, tmdbID int, metrics *catalogArtworkMetrics) error {
	if err := s.hydrateCatalogRootData(ctx, job, tmdbID, metrics); err != nil {
		return err
	}
	item, err := s.repo.Metadata.FindByIdentifier(ctx, job.Provider, job.EntityKind, job.ExternalID)
	if err != nil {
		return err
	}
	if item == nil {
		return errors.New("catalog job metadata is missing")
	}
	if job.EntityKind == model.MetadataKindMovie {
		return s.completeCatalogLeafJob(ctx, job, item)
	}
	return s.repo.Metadata.RequeueCatalogJob(ctx, job.ID, model.CatalogJobStageSeasons, &item.ID)
}

func (s *ScraperService) hydrateCatalogRootData(ctx context.Context, job *model.CatalogHydrationJob, tmdbID int, metrics *catalogArtworkMetrics) error {
	if s.tmdb == nil {
		return errors.New("tmdb catalog provider is unavailable")
	}
	item, err := s.repo.Metadata.FindByIdentifier(ctx, "tmdb", job.EntityKind, job.ExternalID)
	if err != nil {
		return err
	}
	if item != nil && item.CatalogMetadataHydratedAt != nil && item.CatalogArtworkHydratedAt != nil {
		return nil
	}

	var match *Match
	if job.EntityKind == model.MetadataKindSeries {
		match, err = s.tmdb.GetTVMatch(ctx, tmdbID)
	} else {
		match, err = s.tmdb.GetMovieMatch(ctx, tmdbID)
	}
	if err != nil {
		return err
	}
	if match == nil || strings.TrimSpace(match.Title) == "" {
		return fmt.Errorf("tmdb %s %d returned no catalog details", job.EntityKind, tmdbID)
	}
	match.Source = "tmdb"
	if item == nil || item.CatalogMetadataHydratedAt == nil {
		identifiers := metadataIdentifiersFromMatch(match, job.EntityKind)
		preferredID := ""
		if item != nil {
			preferredID = item.ID
		}
		item, err = s.repo.Metadata.UpsertCanonicalWithMerge(ctx, metadataItemFromMatch(match, job.EntityKind, "tmdb"), identifiers, preferredID, true)
		if err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	if item.CatalogMetadataHydratedAt == nil {
		if err := s.repo.Metadata.UpsertProviderSnapshot(ctx, item.ID, "tmdb", match.RawJSON, now); err != nil {
			return err
		}
		if err := s.persistCredits(ctx, item.ID, match.LoadedCreditTypes, match.Credits); err != nil {
			return err
		}
		if job.EntityKind == model.MetadataKindSeries {
			for _, season := range match.Seasons {
				if _, err := s.upsertCatalogSeasonShell(ctx, item, season); err != nil {
					return err
				}
			}
		}
		if err := s.repo.Metadata.MarkCatalogCheckpoint(ctx, item.ID, "catalog_metadata_hydrated_at", now); err != nil {
			return err
		}
	}
	if item.CatalogArtworkHydratedAt == nil {
		if err := s.persistCatalogArtworkWithMetrics(ctx, item.ID, map[string]string{model.ArtworkTypePoster: match.CatalogPosterURL, model.ArtworkTypeBackdrop: match.CatalogBackdropURL}, metrics); err != nil {
			return err
		}
		if err := s.repo.Metadata.MarkCatalogCheckpoint(ctx, item.ID, "catalog_artwork_hydrated_at", now); err != nil {
			return err
		}
	}
	return nil
}

func (s *ScraperService) completeCatalogLeafJob(ctx context.Context, job *model.CatalogHydrationJob, item *model.MetadataItem) error {
	now := time.Now().UTC()
	if item.CatalogHydratedAt == nil {
		if err := s.repo.Metadata.MarkCatalogCheckpoint(ctx, item.ID, "catalog_hydrated_at", now); err != nil {
			return err
		}
	}
	return s.repo.Metadata.CompleteCatalogJob(ctx, job.ID, item.ID, now)
}

func (s *ScraperService) catalogJobMetadata(ctx context.Context, job *model.CatalogHydrationJob) (*model.MetadataItem, error) {
	if job.MetadataID != nil {
		if item, err := s.repo.Metadata.FindByID(ctx, *job.MetadataID); err != nil || item != nil {
			return item, err
		}
	}
	item, err := s.repo.Metadata.FindByIdentifier(ctx, job.Provider, job.EntityKind, job.ExternalID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errors.New("catalog job metadata is missing")
	}
	return item, nil
}

func (s *ScraperService) hydrateCatalogSeason(ctx context.Context, series, season *model.MetadataItem, tmdbID int, metrics *catalogArtworkMetrics) error {
	ctx = withTMDbSeasonBatch(ctx)
	var details *TMDbSeasonDetails
	var err error
	if season.CatalogMetadataHydratedAt == nil || season.CatalogArtworkHydratedAt == nil {
		details, err = s.tmdb.GetTVSeasonDetails(ctx, tmdbID, season.SeasonNum)
		if err != nil {
			return err
		}
		if details == nil {
			return fmt.Errorf("tmdb season %d returned no details", season.SeasonNum)
		}
	}
	now := time.Now().UTC()
	if season.CatalogMetadataHydratedAt == nil {
		season = catalogSeasonItem(series.ID, details)
		season, err = s.repo.Metadata.UpsertSeasonWithIdentifiers(ctx, season, catalogIdentifiers(model.MetadataKindSeason, details.ID, details.ExternalIDs))
		if err != nil {
			return err
		}
		if err := s.repo.Metadata.UpsertProviderSnapshot(ctx, season.ID, "tmdb", details.RawJSON, now); err != nil {
			return err
		}
		if err := s.persistCredits(ctx, season.ID, details.LoadedCreditTypes, details.Credits); err != nil {
			return err
		}
		for _, episode := range details.Episodes {
			if _, err := s.upsertCatalogEpisodeShell(ctx, season, episode); err != nil {
				return err
			}
		}
		if err := s.repo.Metadata.MarkCatalogCheckpoint(ctx, season.ID, "catalog_metadata_hydrated_at", now); err != nil {
			return err
		}
	}
	if season.CatalogArtworkHydratedAt == nil {
		if err := s.persistCatalogArtworkWithMetrics(ctx, season.ID, map[string]string{model.ArtworkTypePoster: details.PosterURL}, metrics); err != nil {
			return err
		}
		if err := s.repo.Metadata.MarkCatalogCheckpoint(ctx, season.ID, "catalog_artwork_hydrated_at", now); err != nil {
			return err
		}
	}
	for {
		if err := s.yieldCatalogToMedia(ctx, metrics); err != nil {
			return err
		}
		episode, err := s.repo.Metadata.FindIncompleteCatalogChild(ctx, season.ID, model.MetadataKindEpisode)
		if err != nil {
			return err
		}
		if episode == nil {
			break
		}
		if metrics != nil {
			metrics.task.Update(TaskUpdate{Stage: "episodes", Message: fmt.Sprintf("正在补全第 %d 季第 %d 集", season.SeasonNum, episode.EpisodeNum)})
		}
		if err := s.hydrateCatalogEpisode(ctx, season, episode, tmdbID, metrics); err != nil {
			return err
		}
	}
	return s.repo.Metadata.MarkCatalogCheckpoint(ctx, season.ID, "catalog_hydrated_at", time.Now().UTC())
}

func (s *ScraperService) hydrateCatalogEpisode(ctx context.Context, season, episode *model.MetadataItem, tmdbID int, metrics *catalogArtworkMetrics) error {
	if episode.CatalogMetadataHydratedAt != nil && episode.CatalogArtworkHydratedAt != nil {
		return s.repo.Metadata.MarkCatalogCheckpoint(ctx, episode.ID, "catalog_hydrated_at", time.Now().UTC())
	}
	details, err := s.tmdb.GetTVEpisodeDetails(ctx, tmdbID, season.SeasonNum, episode.EpisodeNum)
	if err != nil {
		return err
	}
	if details == nil {
		return fmt.Errorf("tmdb episode S%02dE%02d returned no details", season.SeasonNum, episode.EpisodeNum)
	}
	now := time.Now().UTC()
	if episode.CatalogMetadataHydratedAt == nil {
		title := preferredTMDbEntityTitle(details.Name, nil, model.MetadataKindEpisode, episode.EpisodeNum)
		item := &model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: episode.EpisodeNum, Title: title, Overview: strings.TrimSpace(details.Overview), Rating: details.Rating, RuntimeSec: details.Runtime * 60, ReleaseDate: details.AirDate, Year: details.AirYear, Source: "tmdb"}
		ids := catalogIdentifiers(model.MetadataKindEpisode, firstPositive(details.ID, catalogTMDbID(ctx, s, episode)), details.ExternalIDs)
		episode, err = s.repo.Metadata.UpsertEpisodeWithIdentifiers(ctx, item, ids)
		if err != nil {
			return err
		}
		if err := s.repo.Metadata.UpsertProviderSnapshot(ctx, episode.ID, "tmdb", details.RawJSON, now); err != nil {
			return err
		}
		if err := s.persistCredits(ctx, episode.ID, details.LoadedCreditTypes, details.Credits); err != nil {
			return err
		}
		if err := s.repo.Metadata.MarkCatalogCheckpoint(ctx, episode.ID, "catalog_metadata_hydrated_at", now); err != nil {
			return err
		}
	}
	if episode.CatalogArtworkHydratedAt == nil {
		if err := s.persistCatalogArtworkWithMetrics(ctx, episode.ID, map[string]string{model.ArtworkTypeStill: details.CatalogStillURL}, metrics); err != nil {
			return err
		}
		if err := s.repo.Metadata.MarkCatalogCheckpoint(ctx, episode.ID, "catalog_artwork_hydrated_at", now); err != nil {
			return err
		}
	}
	return s.repo.Metadata.MarkCatalogCheckpoint(ctx, episode.ID, "catalog_hydrated_at", time.Now().UTC())
}

func (s *ScraperService) upsertCatalogSeasonShell(ctx context.Context, series *model.MetadataItem, summary TMDbSeasonSummary) (*model.MetadataItem, error) {
	title := strings.TrimSpace(summary.Name)
	if title == "" {
		title = seasonName(summary.SeasonNumber)
	}
	item := &model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: summary.SeasonNumber, Title: title, ReleaseDate: normalizeReleaseDate(summary.AirDate), Source: "tmdb"}
	existing, err := s.repo.Metadata.FindSeason(ctx, series.ID, summary.SeasonNumber)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.Source != "tmdb" || existing.CatalogMetadataHydratedAt != nil {
			return existing, nil
		}
		preserveMissingLocalEpisodeDetails(item, existing)
	}
	return s.repo.Metadata.UpsertSeasonWithIdentifiers(ctx, item, catalogIdentifiers(model.MetadataKindSeason, summary.ID, TMDbExternalIDs{}))
}

func (s *ScraperService) upsertCatalogEpisodeShell(ctx context.Context, season *model.MetadataItem, summary TMDbEpisodeSummary) (*model.MetadataItem, error) {
	title := preferredTMDbEntityTitle(summary.Name, nil, model.MetadataKindEpisode, summary.EpisodeNumber)
	item := &model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: summary.EpisodeNumber, Title: title, Overview: summary.Overview, ReleaseDate: summary.AirDate, Rating: summary.Rating, RuntimeSec: summary.Runtime * 60, Source: "tmdb"}
	if season.ParentID != nil {
		existing, err := s.repo.Metadata.FindEpisode(ctx, *season.ParentID, season.SeasonNum, summary.EpisodeNumber)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.Source != "tmdb" || existing.CatalogMetadataHydratedAt != nil {
				return existing, nil
			}
			preserveMissingLocalEpisodeDetails(item, existing)
		}
	}
	return s.repo.Metadata.UpsertEpisodeWithIdentifiers(ctx, item, catalogIdentifiers(model.MetadataKindEpisode, summary.ID, TMDbExternalIDs{}))
}

func catalogSeasonItem(seriesID string, details *TMDbSeasonDetails) *model.MetadataItem {
	title := strings.TrimSpace(details.Name)
	if title == "" {
		title = seasonName(details.SeasonNumber)
	}
	year := 0
	if len(details.AirDate) >= 4 {
		year, _ = strconv.Atoi(details.AirDate[:4])
	}
	return &model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &seriesID, SeasonNum: details.SeasonNumber, Title: title, Overview: details.Overview, Rating: details.Rating, ReleaseDate: details.AirDate, Year: year, Source: "tmdb"}
}

func catalogIdentifiers(kind string, tmdbID int, external TMDbExternalIDs) []model.MetadataIdentifier {
	ids := make([]model.MetadataIdentifier, 0, 3)
	if tmdbID > 0 {
		ids = append(ids, model.MetadataIdentifier{Provider: "tmdb", EntityKind: kind, ExternalID: strconv.Itoa(tmdbID)})
	}
	if external.TVDBID > 0 {
		ids = append(ids, model.MetadataIdentifier{Provider: "thetvdb", EntityKind: kind, ExternalID: strconv.Itoa(external.TVDBID)})
	}
	if value := strings.TrimSpace(external.IMDbID); value != "" {
		ids = append(ids, model.MetadataIdentifier{Provider: "imdb", EntityKind: kind, ExternalID: value})
	}
	return ids
}

func catalogTMDbID(ctx context.Context, s *ScraperService, item *model.MetadataItem) int {
	if s == nil || item == nil {
		return 0
	}
	ids, err := s.repo.Metadata.ListIdentifiers(ctx, item.ID)
	if err != nil {
		return 0
	}
	for _, id := range ids {
		if id.Provider == "tmdb" && id.EntityKind == item.Kind {
			value, _ := strconv.Atoi(id.ExternalID)
			return value
		}
	}
	return 0
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (s *ScraperService) persistCatalogArtwork(ctx context.Context, metadataID string, sources map[string]string) error {
	return s.persistCatalogArtworkWithMetrics(ctx, metadataID, sources, nil)
}

type catalogArtworkMetrics struct {
	task          *TaskHandle
	Saved         int64
	Existing      int64
	SourceMissing int64
}

func (s *ScraperService) persistCatalogArtworkWithMetrics(ctx context.Context, metadataID string, sources map[string]string, metrics *catalogArtworkMetrics) error {
	for _, artworkType := range []string{model.ArtworkTypePoster, model.ArtworkTypeBackdrop, model.ArtworkTypeStill} {
		source, ok := sources[artworkType]
		if !ok || strings.TrimSpace(source) == "" {
			if ok && metrics != nil {
				metrics.SourceMissing++
			}
			continue
		}
		if s.artwork == nil {
			return errors.New("catalog artwork store is unavailable")
		}
		_, existing, err := s.artwork.importCatalogRemote(ctx, metadataID, artworkType, "tmdb", source)
		if err != nil {
			return err
		}
		if metrics != nil {
			if existing {
				metrics.Existing++
			} else {
				metrics.Saved++
			}
		}
	}
	return nil
}

func catalogRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 7 {
		attempt = 7
	}
	return time.Duration(1<<(attempt-1)) * time.Minute
}

func sanitizeCatalogError(err error) error {
	return sanitizeTaskLogError(err)
}

func sanitizeTaskLogError(err error) error {
	if err == nil {
		return nil
	}
	message := catalogURLPattern.ReplaceAllString(err.Error(), "[redacted-url]")
	return errors.New(message)
}

func isTMDbCatalogItem(item ExternalMediaResult) bool {
	return strings.EqualFold(strings.TrimSpace(item.Source), "tmdb") && item.TMDbID > 0
}

func catalogEntityKind(mediaType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "tv", "series", "show", "anime", "variety":
		return model.MetadataKindSeries, true
	case "movie", "film":
		return model.MetadataKindMovie, true
	}
	return "", false
}
