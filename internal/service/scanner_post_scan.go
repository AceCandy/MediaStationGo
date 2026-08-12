package service

import (
	"context"
)

func (s *ScannerService) invalidateMediaCache(ctx context.Context) {
	if s != nil && s.cache != nil {
		s.cache.DeletePrefix(ctx, "media:")
		s.cache.DeletePrefix(ctx, "stats:")
	}
}

func (s *ScannerService) startAutoScrape(ctx context.Context, libraryID string) {
	if s != nil && s.scraper != nil {
		s.scraper.WakeScrapeWorker()
	}
}
