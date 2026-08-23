package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestGroupScrapeCandidateRowsUsesStableIdentity(t *testing.T) {
	rows := []model.Media{
		{Base: model.Base{ID: "series-1-a"}, SeriesID: "series-1"},
		{Base: model.Base{ID: "series-1-b"}, SeriesID: "series-1"},
		{Base: model.Base{ID: "metadata-1-a"}, MetadataID: "metadata-1"},
		{Base: model.Base{ID: "metadata-1-b"}, MetadataID: "metadata-1"},
		{Base: model.Base{ID: "media-1"}},
	}
	groups, err := groupScrapeCandidateRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 || len(groups[0].MediaIDs) != 2 || len(groups[1].MediaIDs) != 2 || len(groups[2].MediaIDs) != 1 {
		t.Fatalf("groups = %#v, want series/metadata/media groups with sizes 2/2/1", groups)
	}
}

func TestClaimNextPendingMediaGroupClaimsWholeSeries(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	library := model.Library{Name: "TV", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: library.ID, SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e01.mkv", SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "pending"},
		{LibraryID: library.ID, SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e02.mkv", SeasonNum: 1, EpisodeNum: 2, ScrapeStatus: "pending"},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	group, err := scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if group == nil || len(group.MediaIDs) != 2 {
		t.Fatalf("claimed group = %#v, want complete two-episode series", group)
	}
	var running int64
	if err := repos.DB.Model(&model.Media{}).Where("series_hint = ? AND scrape_status = ?", "series-1", "running").Count(&running).Error; err != nil {
		t.Fatal(err)
	}
	if running != 2 {
		t.Fatalf("running rows = %d, want 2", running)
	}

	late := model.Media{LibraryID: library.ID, SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e03.mkv", SeasonNum: 1, EpisodeNum: 3, ScrapeStatus: "pending"}
	if err := repos.DB.Create(&late).Error; err != nil {
		t.Fatal(err)
	}
	group, err = scraper.claimNextPendingMediaGroup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if group != nil {
		t.Fatalf("claimed late episode while series is running: %#v", group)
	}
}

func TestClaimNextPendingMediaGroupConcurrentSingleClaim(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	library := model.Library{Name: "TV", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: library.ID, SeriesID: "series-concurrent", Title: "Show", Path: "/media/tv/show-s01e01.mkv", SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "pending"},
		{LibraryID: library.ID, SeriesID: "series-concurrent", Title: "Show", Path: "/media/tv/show-s01e02.mkv", SeasonNum: 1, EpisodeNum: 2, ScrapeStatus: "pending"},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	type claimResult struct {
		group *scrapeCandidateGroup
		err   error
	}
	start := make(chan struct{})
	results := make(chan claimResult, autoMediaScrapeWorkerCount)
	for range autoMediaScrapeWorkerCount {
		go func() {
			<-start
			group, err := scraper.claimNextPendingMediaGroup(t.Context())
			results <- claimResult{group: group, err: err}
		}()
	}
	close(start)

	claimed := 0
	for range autoMediaScrapeWorkerCount {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.group != nil {
			claimed++
			if len(result.group.MediaIDs) != len(rows) {
				t.Fatalf("claimed %d rows, want %d", len(result.group.MediaIDs), len(rows))
			}
		}
	}
	if claimed != 1 {
		t.Fatalf("successful claims = %d, want 1", claimed)
	}
}

func TestRecoverRunningMediaScrapes(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: library.ID, Title: "Movie", Path: "/media/movies/movie.mkv", ScrapeStatus: "running"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.recoverRunningMediaScrapes(t.Context()); err != nil {
		t.Fatal(err)
	}
	var stored model.Media
	if err := repos.DB.First(&stored, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ScrapeStatus != "pending" {
		t.Fatalf("scrape status = %q, want pending", stored.ScrapeStatus)
	}
}

func TestResetMediaScrapeResetsCompleteSeries(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()

	library := model.Library{Name: "TV", Path: "/media/tv", Type: "tv", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{LibraryID: library.ID, MetadataID: "episode-1", SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e01.mkv", SeasonNum: 1, EpisodeNum: 1, ScrapeStatus: "matched"},
		{LibraryID: library.ID, MetadataID: "episode-2", SeriesID: "series-1", Title: "Show", Path: "/media/tv/show-s01e02.mkv", SeasonNum: 1, EpisodeNum: 2, ScrapeStatus: "matched"},
	}
	metadata := []model.MetadataItem{
		{Base: model.Base{ID: "episode-1"}, Kind: model.MetadataKindMovie, Title: "Episode 1", Source: "test"},
		{Base: model.Base{ID: "episode-2"}, Kind: model.MetadataKindMovie, Title: "Episode 2", Source: "test"},
	}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := scraper.ResetMediaScrape(t.Context(), rows[0].ID); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := repos.DB.Model(&model.Media{}).Where("series_hint = ? AND scrape_status = ?", "series-1", "pending").Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending != 2 {
		t.Fatalf("pending rows = %d, want complete two-episode series", pending)
	}
}

func TestResetLibraryScrapeQueuesOnlyUnfinishedRowsUnlessMatchedRequested(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	library := model.Library{Name: "Movies", Path: "/media/movies", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&library).Error; err != nil {
		t.Fatal(err)
	}
	rows := make([]model.Media, 7)
	for i := range rows {
		rows[i] = model.Media{LibraryID: library.ID, Title: "Movie", Path: "/media/movies/movie-" + string(rune('a'+i)) + ".mkv", ScrapeStatus: "pending", ScrapeError: "old", ScrapeTrigger: TaskTriggerEvent}
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	statuses := []any{nil, "", "pending", "error", "no_match", "matched", "running"}
	for i, status := range statuses {
		if err := repos.DB.Model(&model.Media{}).Where("id = ?", rows[i].ID).Update("scrape_status", status).Error; err != nil {
			t.Fatal(err)
		}
	}

	queued, err := scraper.ResetLibraryScrape(t.Context(), library.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 5 {
		t.Fatalf("queued = %d, want 5 unfinished rows", queued)
	}
	var stored []model.Media
	if err := repos.DB.Order("path").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	for i := range stored {
		if i < 5 {
			if stored[i].ScrapeStatus != "pending" || stored[i].ScrapeTrigger != TaskTriggerManual || stored[i].ScrapeError != "" {
				t.Fatalf("unfinished row %d = %#v", i, stored[i])
			}
			continue
		}
		if stored[i].ScrapeStatus != statuses[i] || stored[i].ScrapeTrigger != TaskTriggerEvent || stored[i].ScrapeError != "old" {
			t.Fatalf("completed/running row %d changed: %#v", i, stored[i])
		}
	}

	queued, err = scraper.ResetLibraryScrape(t.Context(), library.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 6 {
		t.Fatalf("includeMatched queued = %d, want all except running", queued)
	}
	if err := repos.DB.First(&stored[5], "id = ?", rows[5].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored[5].ScrapeStatus != "pending" || stored[5].ScrapeTrigger != TaskTriggerManual {
		t.Fatalf("matched row was not explicitly requeued: %#v", stored[5])
	}
}
