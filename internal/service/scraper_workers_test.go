package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMediaScrapeWorkersStopOnCancel(t *testing.T) {
	scraper, _, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	ctx, cancel := context.WithCancel(t.Context())
	scraper.StartMediaScrapeWorkers(ctx)
	cancel()
	scraper.WaitBackgroundWorkers()
}

func TestSanitizeTaskLogErrorRemovesQuery(t *testing.T) {
	err := sanitizeTaskLogError(errors.New("download https://example.test/image.jpg?token=secret&x=1 failed"))
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "?") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("sanitized error leaked query: %v", err)
	}
}
