package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestManualPeopleBackfillRecordsEmptyRun(t *testing.T) {
	scraper, _, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	tasks := NewTaskTrackerService(nil, nil)
	tasks.ConfigurePersistence(nil, t.TempDir())
	scraper.SetTaskTracker(tasks)

	if err := scraper.runPeopleBackfillPass(t.Context(), TaskTriggerEvent); err != nil {
		t.Fatal(err)
	}
	if snapshot := tasks.Snapshot(); len(snapshot.Recent) != 0 {
		t.Fatalf("event snapshot = %+v, want no empty execution", snapshot)
	}
	if err := scraper.runPeopleBackfillPass(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}

	snapshot := tasks.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Status != TaskStatusCompleted || snapshot.Recent[0].Metrics["total"] != 0 {
		t.Fatalf("manual snapshot = %+v", snapshot)
	}
	log, err := tasks.ReadDefinitionLog(TaskDefinitionPeopleBackfill, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.Content, "人物信息补齐已启动") || !strings.Contains(log.Content, "人物信息补齐执行完成，无待补齐人物") {
		t.Fatalf("task log = %q", log.Content)
	}
}

func TestManualPeopleBackfillRecordsCandidateQueryFailure(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	tasks := NewTaskTrackerService(nil, nil)
	tasks.ConfigurePersistence(nil, t.TempDir())
	scraper.SetTaskTracker(tasks)
	sqlDB, err := repos.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := scraper.runPeopleBackfillPass(t.Context(), TaskTriggerManual); err == nil {
		t.Fatal("manual run succeeded after candidate query failed")
	}
	snapshot := tasks.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Status != TaskStatusFailed {
		t.Fatalf("manual snapshot = %+v, want failed execution", snapshot)
	}
	log, err := tasks.ReadDefinitionLog(TaskDefinitionPeopleBackfill, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.Content, "人物信息补齐失败") {
		t.Fatalf("task log = %q", log.Content)
	}
}

func TestBackfillPeopleFillsGlobalMetadataWithoutCredits(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	scraper.SetTaskTracker(NewTaskTrackerService(nil, nil))
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Show", Source: "tmdb"}
	if err := repos.DB.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: series.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := scraper.runPeopleBackfillPass(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	snapshot := scraper.tasks.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Name != "人物信息补齐" || snapshot.Recent[0].Metrics["completed"] != 1 {
		t.Fatalf("task snapshot = %+v", snapshot)
	}
	credits, err := repos.Person.ListCreditsWithPeople(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	hasActor := false
	for _, credit := range credits {
		hasActor = hasActor || credit.Type == model.CreditTypeActor && credit.Person.Name == "Test Actor"
	}
	if len(credits) != 2 || !hasActor {
		t.Fatalf("credits = %+v", credits)
	}
	var hydrated model.MetadataItem
	if err := repos.DB.First(&hydrated, "id = ?", series.ID).Error; err != nil || hydrated.PeopleHydratedAt == nil {
		t.Fatalf("people hydrated at = %v, err=%v", hydrated.PeopleHydratedAt, err)
	}
	second, err := scraper.BackfillLibraryPeople(t.Context(), "another-library", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 0 {
		t.Fatalf("second result = %+v, want no candidates", second)
	}
}

func TestBackfillPeopleDoesNotRetrySuccessfulEmptyCredits(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Empty Credits", Source: "tmdb"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "12345"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := scraper.persistCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor, model.CreditTypeDirector, model.CreditTypeWriter}, nil); err != nil {
		t.Fatal(err)
	}

	candidates, err := scraper.pendingPeopleBackfillCandidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none", candidates)
	}
}

func TestPendingPeopleBackfillCandidatesRequireTMDbSourceAndMatchingKind(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	items := []model.MetadataItem{
		{Kind: model.MetadataKindMovie, Title: "Douban Movie", Source: "douban"},
		{Kind: model.MetadataKindMovie, Title: "Wrong Kind", Source: "tmdb"},
	}
	if err := repos.DB.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{MetadataID: items[0].ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "101"},
		{MetadataID: items[1].ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "102"},
	}
	if err := repos.DB.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}

	candidates, err := scraper.pendingPeopleBackfillCandidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %+v, want none", candidates)
	}
	var count int64
	if err := repos.DB.Model(&model.MetadataIdentifier{}).Where("metadata_id IN ?", []string{items[0].ID, items[1].ID}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("identifier count = %d, err=%v", count, err)
	}
}

func TestBackfillPeopleInvalidatesNotFoundTMDbAndContinues(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/404/credits":
			w.WriteHeader(http.StatusNotFound)
		case "/tv/12345/credits":
			_ = json.NewEncoder(w).Encode(map[string]any{"cast": []map[string]any{{"id": 99, "name": "Test Actor", "character": "Hero"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	scraper.cfg.Secrets.TMDbAPIProxy = upstream.URL
	scraper.SetTaskTracker(NewTaskTrackerService(nil, nil))

	invalid := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Invalid", Source: "tmdb"}
	valid := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Valid", Source: "tmdb"}
	if err := repos.DB.Create(&[]model.MetadataItem{invalid, valid}).Error; err != nil {
		t.Fatal(err)
	}
	identifiers := []model.MetadataIdentifier{
		{MetadataID: invalid.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "404"},
		{MetadataID: valid.ID, Provider: "tmdb", EntityKind: model.MetadataKindSeries, ExternalID: "12345"},
	}
	if err := repos.DB.Create(&identifiers).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{MetadataID: invalid.ID, Title: "Invalid", Path: "/media/invalid.mkv", TMDbID: 404, ScrapeStatus: "matched", ScrapeTrigger: TaskTriggerManual, ScrapeError: "old"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.runPeopleBackfillPass(t.Context(), TaskTriggerManual); err != nil {
		t.Fatal(err)
	}
	snapshot := scraper.tasks.Snapshot()
	if len(snapshot.Recent) != 1 || snapshot.Recent[0].Metrics["failed"] != 1 || snapshot.Recent[0].Metrics["completed"] != 1 {
		t.Fatalf("task snapshot = %+v", snapshot)
	}
	var activeIdentifiers int64
	if err := repos.DB.Model(&model.MetadataIdentifier{}).Where("metadata_id = ? AND provider = ?", invalid.ID, "tmdb").Count(&activeIdentifiers).Error; err != nil || activeIdentifiers != 0 {
		t.Fatalf("active identifiers = %d, err=%v", activeIdentifiers, err)
	}
	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.TMDbID != 0 || got.ScrapeStatus != "pending" || got.ScrapeTrigger != TaskTriggerEvent || got.ScrapeError != "" {
		t.Fatalf("media after invalidation = %+v", got)
	}
	candidates, err := scraper.pendingPeopleBackfillCandidates(t.Context())
	if err != nil || len(candidates) != 0 {
		t.Fatalf("remaining candidates = %+v, err=%v", candidates, err)
	}
	if len(scraper.catalogHydrationWake) != 1 {
		t.Fatalf("scrape wake count = %d", len(scraper.catalogHydrationWake))
	}
}

func TestBackfillPeopleKeepsTMDbIdentifierOnServerError(t *testing.T) {
	scraper, repos, closeUpstream := newTestScraper(t)
	defer closeUpstream()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	scraper.cfg.Secrets.TMDbAPIProxy = upstream.URL

	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "Temporary Failure", Source: "tmdb"}
	if err := repos.DB.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.MetadataIdentifier{MetadataID: metadata.ID, Provider: "tmdb", EntityKind: model.MetadataKindMovie, ExternalID: "500"}).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{MetadataID: metadata.ID, Title: metadata.Title, Path: "/media/temporary.mkv", TMDbID: 500, ScrapeStatus: "matched"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	result, err := scraper.BackfillLibraryPeople(t.Context(), "", nil)
	if err != nil || result.Failed != 1 {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
	var identifierCount int64
	if err := repos.DB.Model(&model.MetadataIdentifier{}).Where("metadata_id = ?", metadata.ID).Count(&identifierCount).Error; err != nil || identifierCount != 1 {
		t.Fatalf("identifier count = %d, err=%v", identifierCount, err)
	}
	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.TMDbID != 500 || got.ScrapeStatus != "matched" {
		t.Fatalf("media after temporary error = %+v", got)
	}
}
