package service

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

const autoMediaScrapeWorkerCount = 3

var catalogURLPattern = regexp.MustCompile(`https?://[^\s]+`)

// StartMediaScrapeWorkers 恢复并处理文件入库关联，来源补全由资料中心负责。
func (s *ScraperService) StartMediaScrapeWorkers(ctx context.Context) (startErr error) {
	if s == nil || s.repo == nil || s.repo.Metadata == nil {
		return errors.New("资料刮削服务不可用")
	}
	s.mediaScrapeOnce.Do(func() {
		if s.mediaScrapeWake == nil {
			s.mediaScrapeWake = make(chan struct{}, autoMediaScrapeWorkerCount)
		}
		if err := s.recoverRunningMediaScrapes(ctx); err != nil {
			startErr = err
			return
		}
		s.backgroundWG.Add(autoMediaScrapeWorkerCount)
		for range autoMediaScrapeWorkerCount {
			go s.runMediaScrapeWorker(ctx)
		}
	})
	return startErr
}
func (s *ScraperService) WaitBackgroundWorkers() {
	if s != nil {
		s.backgroundWG.Wait()
	}
}
func (s *ScraperService) WakeScrapeWorker() {
	if s != nil {
		s.wakeMediaScrapeWorkers()
	}
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

func (s *ScraperService) runMediaScrapeWorker(ctx context.Context) {
	defer s.backgroundWG.Done()
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
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.mediaScrapeWake:
		}
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

func (s *ScraperService) upsertCatalogEpisodeShell(ctx context.Context, season *model.MetadataItem, summary TMDbEpisodeSummary) (*model.MetadataItem, error) {
	title := preferredTMDbEntityTitle(summary.Name, nil, model.MetadataKindEpisode, summary.EpisodeNumber)
	item := &model.MetadataItem{Kind: model.MetadataKindEpisode, ParentID: &season.ID, EpisodeNum: summary.EpisodeNumber, Title: title, Overview: summary.Overview, ReleaseDate: summary.AirDate, Rating: summary.Rating, RuntimeSec: summary.Runtime * 60, Source: "tmdb"}
	if season.ParentID != nil {
		existing, err := s.repo.Metadata.FindEpisode(ctx, *season.ParentID, season.SeasonNum, summary.EpisodeNumber)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.Source != "tmdb" {
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

func sanitizeTaskLogError(err error) error {
	if err == nil {
		return nil
	}
	message := catalogURLPattern.ReplaceAllString(err.Error(), "[redacted-url]")
	return errors.New(message)
}
