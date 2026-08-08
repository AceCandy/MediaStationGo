package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// StartCatalogHydrationWorker starts the service-lifetime worker used by the
// discover page. The worker is intentionally independent from HTTP requests so
// a slow detail, credit, or image request cannot delay the feed response.
func (s *ScraperService) StartCatalogHydrationWorker(ctx context.Context) {
	if s == nil {
		return
	}
	s.catalogHydrationOnce.Do(func() {
		s.catalogHydrationMu.Lock()
		if s.catalogHydrationPending == nil {
			s.catalogHydrationPending = make(map[string]ExternalMediaResult)
		}
		if s.catalogHydrationWake == nil {
			s.catalogHydrationWake = make(chan struct{}, 1)
		}
		s.catalogHydrationMu.Unlock()
		s.catalogHydrationWG.Add(1)
		go s.runCatalogHydrationWorker(ctx)
	})
}

// WaitCatalogHydrationWorker joins the worker during service shutdown.
func (s *ScraperService) WaitCatalogHydrationWorker() {
	if s != nil {
		s.catalogHydrationWG.Wait()
	}
}

// QueueCatalogHydration coalesces TMDb discover rows for asynchronous import.
func (s *ScraperService) QueueCatalogHydration(items []ExternalMediaResult) {
	if s == nil || len(items) == 0 {
		return
	}
	s.catalogHydrationMu.Lock()
	if s.catalogHydrationPending == nil {
		s.catalogHydrationPending = make(map[string]ExternalMediaResult)
	}
	if s.catalogHydrationWake == nil {
		s.catalogHydrationWake = make(chan struct{}, 1)
	}
	for _, item := range items {
		if !isTMDbCatalogItem(item) {
			continue
		}
		key := catalogHydrationKey(item)
		if _, exists := s.catalogHydrationPending[key]; !exists {
			s.catalogHydrationPending[key] = item
		}
	}
	wake := s.catalogHydrationWake
	s.catalogHydrationMu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (s *ScraperService) runCatalogHydrationWorker(ctx context.Context) {
	defer s.catalogHydrationWG.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.catalogHydrationWake:
		}
		for {
			items := s.takeCatalogHydrationBatch()
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				// Failed items are retried when a later discover response observes them again.
				if err := s.hydrateCatalogItem(ctx, item); err != nil && ctx.Err() == nil && s.log != nil {
					s.log.Warn("discover catalog hydration failed",
						zap.Int("tmdb_id", item.TMDbID),
						zap.String("media_type", item.MediaType),
						zap.Error(err))
				}
			}
		}
	}
}

func (s *ScraperService) takeCatalogHydrationBatch() []ExternalMediaResult {
	s.catalogHydrationMu.Lock()
	defer s.catalogHydrationMu.Unlock()
	if len(s.catalogHydrationPending) == 0 {
		return nil
	}
	items := make([]ExternalMediaResult, 0, len(s.catalogHydrationPending))
	for _, item := range s.catalogHydrationPending {
		items = append(items, item)
	}
	s.catalogHydrationPending = make(map[string]ExternalMediaResult)
	return items
}

func (s *ScraperService) hydrateCatalogItem(ctx context.Context, item ExternalMediaResult) error {
	if !isTMDbCatalogItem(item) || s.repo == nil || s.repo.Metadata == nil || s.tmdb == nil {
		return nil
	}
	entityKind := catalogEntityKind(item.MediaType)
	externalID := strconv.Itoa(item.TMDbID)
	existing, err := s.repo.Metadata.FindByIdentifier(ctx, "tmdb", entityKind, externalID)
	if err != nil {
		return err
	}
	if existing != nil && existing.CatalogHydratedAt != nil {
		return nil
	}

	var match *Match
	if entityKind == model.MetadataKindSeries {
		match, err = s.tmdb.GetTVMatch(ctx, item.TMDbID)
	} else {
		match, err = s.tmdb.GetMovieMatch(ctx, item.TMDbID)
	}
	if err != nil {
		return err
	}
	if match == nil || strings.TrimSpace(match.Title) == "" {
		return fmt.Errorf("tmdb %d returned no catalog match", item.TMDbID)
	}
	match.Source = "tmdb"
	match.MediaType = item.MediaType
	fillCatalogMatchFallbacks(match, item)
	result, err := s.persistProviderMetadata(ctx, &model.Media{}, nil, match)
	if err != nil {
		return err
	}
	if result == nil || result.Target == nil {
		return errors.New("catalog metadata persistence returned no target")
	}
	return s.repo.Metadata.MarkCatalogHydrated(ctx, result.Target.ID, time.Now().UTC())
}

func fillCatalogMatchFallbacks(match *Match, item ExternalMediaResult) {
	if match == nil {
		return
	}
	if match.Title == "" {
		match.Title = item.Title
	}
	if match.Overview == "" {
		match.Overview = item.Overview
	}
	if match.PosterURL == "" {
		match.PosterURL = item.PosterURL
	}
	if match.BackdropURL == "" {
		match.BackdropURL = item.BackdropURL
	}
	if match.Year == 0 {
		match.Year = item.Year
	}
	if match.Rating == 0 {
		match.Rating = item.Rating
	}
	if match.TMDbID == 0 {
		match.TMDbID = item.TMDbID
	}
}

func isTMDbCatalogItem(item ExternalMediaResult) bool {
	return strings.EqualFold(strings.TrimSpace(item.Source), "tmdb") && item.TMDbID > 0
}

func catalogEntityKind(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "tv", "series", "show", "anime", "variety":
		return model.MetadataKindSeries
	default:
		return model.MetadataKindMovie
	}
}

func catalogHydrationKey(item ExternalMediaResult) string {
	return fmt.Sprintf("tmdb:%s:%d", catalogEntityKind(item.MediaType), item.TMDbID)
}
