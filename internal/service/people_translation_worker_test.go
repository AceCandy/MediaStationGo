package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestScheduledPeopleTranslationUsesContextAndCache(t *testing.T) {
	var calls atomic.Int32
	requests := make(chan []AITranslationEntry, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		var entries []AITranslationEntry
		if err := json.Unmarshal([]byte(payload.Input), &entries); err != nil {
			t.Errorf("decode translation input: %v", err)
			return
		}
		select {
		case requests <- entries:
		default:
		}
		translations := make(map[string]string, len(entries))
		for _, entry := range entries {
			if entry.Kind == "person_name" {
				translations[entry.Key] = "梁朝伟"
			} else {
				translations[entry.Key] = "陈永仁"
			}
		}
		raw, _ := json.Marshal(translations)
		_ = json.NewEncoder(w).Encode(map[string]any{"output": []any{map[string]any{
			"type": "message", "content": []any{map[string]any{"type": "output_text", "text": string(raw)}},
		}}})
	}))
	t.Cleanup(server.Close)

	db := newServiceTestDB(t, &model.Person{}, &model.PersonIdentifier{}, &model.MetadataCredit{}, &model.TranslationCache{}, &model.Setting{})
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), peopleAITranslateSettingKey, "true"); err != nil {
		t.Fatal(err)
	}
	metadata := model.MetadataItem{Kind: model.MetadataKindMovie, Title: "无间道", OriginalName: "Infernal Affairs", Year: 2002, Source: "tmdb"}
	if err := db.Create(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AI: config.AIConfig{Enabled: true, APIKey: "test-key", APIBase: server.URL + "/v1", Model: "test-model"}}
	scraper := NewScraperService(cfg, zap.NewNop(), repos, nil, nil, nil, nil, nil).SetAI(NewAIService(cfg, zap.NewNop(), nil))
	scraper.SetTaskTracker(NewTaskTrackerService(zap.NewNop(), nil))

	if err := scraper.persistCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, []PersonCredit{{
		Provider: "tmdb", ExternalID: "140", Name: "Tony Leung Chiu-wai", Type: model.CreditTypeActor, OriginalRole: "Chan Wing-yan",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := scraper.translatePendingPeopleScheduled(t.Context()); err != nil {
		t.Fatal(err)
	}

	var entries []AITranslationEntry
	select {
	case entries = <-requests:
	case <-time.After(time.Second):
		t.Fatal("translation request was not sent")
	}
	assertPeopleTranslationContext(t, entries)
	waitForPeopleTranslation(t, db, "梁朝伟", "陈永仁")

	var cacheCount int64
	if err := db.Model(&model.TranslationCache{}).Count(&cacheCount).Error; err != nil || cacheCount != 2 {
		t.Fatalf("translation cache count = %d, err=%v", cacheCount, err)
	}
	if err := db.Model(&model.Person{}).Where("original_name = ?", "Tony Leung Chiu-wai").Update("name", "Tony Leung Chiu-wai").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MetadataCredit{}).Where("metadata_id = ?", metadata.ID).Update("role", "Chan Wing-yan").Error; err != nil {
		t.Fatal(err)
	}
	if err := scraper.translatePendingPeopleScheduled(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitForPeopleTranslation(t, db, "梁朝伟", "陈永仁")
	if got := calls.Load(); got != 1 {
		t.Fatalf("AI request count = %d, want 1 after cache hit", got)
	}
	snapshot := scraper.tasks.Snapshot()
	if len(snapshot.Recent) == 0 || snapshot.Recent[0].Name != "人物翻译" || snapshot.Recent[0].Trigger != TaskTriggerScheduled {
		t.Fatalf("task snapshot = %+v", snapshot)
	}
}

func TestScheduledPeopleTranslationLimitsPassTo1000(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		var entries []AITranslationEntry
		if err := json.Unmarshal([]byte(payload.Input), &entries); err != nil {
			t.Errorf("decode translation input: %v", err)
			return
		}
		received.Add(int32(len(entries)))
		translations := make(map[string]string, len(entries))
		for _, entry := range entries {
			translations[entry.Key] = "译名"
		}
		raw, _ := json.Marshal(translations)
		_ = json.NewEncoder(w).Encode(map[string]any{"output": []any{map[string]any{
			"type": "message", "content": []any{map[string]any{"type": "output_text", "text": string(raw)}},
		}}})
	}))
	t.Cleanup(server.Close)

	db := newServiceTestDB(t, &model.Person{}, &model.TranslationCache{}, &model.Setting{})
	people := make([]model.Person, peopleTranslationPassLimit+1)
	for i := range people {
		name := fmt.Sprintf("Person %04d", i)
		people[i] = model.Person{Name: name, OriginalName: name}
	}
	if err := db.CreateInBatches(&people, 100).Error; err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), peopleAITranslateSettingKey, "true"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AI: config.AIConfig{Enabled: true, APIKey: "test-key", APIBase: server.URL + "/v1", Model: "test-model"}}
	scraper := NewScraperService(cfg, zap.NewNop(), repos, nil, nil, nil, nil, nil).SetAI(NewAIService(cfg, zap.NewNop(), nil))
	scraper.SetTaskTracker(NewTaskTrackerService(zap.NewNop(), nil))

	if err := scraper.translatePendingPeopleScheduled(t.Context()); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := db.Model(&model.Person{}).Where("name = original_name").Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := scraper.tasks.Snapshot()
	if received.Load() != peopleTranslationPassLimit || pending != 1 || len(snapshot.Recent) != 1 || snapshot.Recent[0].Metrics["total"] != peopleTranslationPassLimit {
		t.Fatalf("received=%d pending=%d snapshot=%+v", received.Load(), pending, snapshot)
	}

	if err := scraper.translatePendingPeopleScheduled(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Person{}).Where("name = original_name").Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if received.Load() != peopleTranslationPassLimit+1 || pending != 0 {
		t.Fatalf("received=%d pending=%d", received.Load(), pending)
	}
}

func TestSplitPeopleTranslationBatchesLimitsEntries(t *testing.T) {
	groups := make([]*pendingPeopleTranslation, 205)
	for i := range groups {
		groups[i] = &pendingPeopleTranslation{entry: AITranslationEntry{Key: fmt.Sprintf("translation:%d", i), Text: "name"}}
	}
	batches := splitPeopleTranslationBatches(groups, 100, 1_000_000)
	if len(batches) != 3 || len(batches[0]) != 100 || len(batches[1]) != 100 || len(batches[2]) != 5 {
		t.Fatalf("batch sizes = %d/%d/%d, batches=%d", len(batches[0]), len(batches[1]), len(batches[2]), len(batches))
	}
}

func TestPeopleTranslationEmptyPassDoesNotCreateTask(t *testing.T) {
	db := newServiceTestDB(t, &model.Person{}, &model.MetadataCredit{}, &model.TranslationCache{}, &model.Setting{})
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), peopleAITranslateSettingKey, "true"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AI: config.AIConfig{Enabled: true, APIKey: "test-key"}}
	scraper := NewScraperService(cfg, zap.NewNop(), repos, nil, nil, nil, nil, nil).SetAI(NewAIService(cfg, zap.NewNop(), nil))
	scraper.SetTaskTracker(NewTaskTrackerService(zap.NewNop(), nil))
	if err := scraper.translatePendingPeopleScheduled(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot := scraper.tasks.Snapshot()
	if len(snapshot.Active) != 0 || len(snapshot.Recent) != 0 {
		t.Fatalf("unexpected task snapshot = %+v", snapshot)
	}
}

func TestPendingRoleTranslationsShareSeasonContext(t *testing.T) {
	db := newServiceTestDB(t, &model.MetadataItem{}, &model.Person{}, &model.MetadataCredit{})
	repos := repository.New(db)
	series := model.MetadataItem{Kind: model.MetadataKindSeries, Title: "Series", Source: "tmdb"}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	season1 := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 1, Title: "Season 1", Source: "tmdb"}
	season2 := model.MetadataItem{Kind: model.MetadataKindSeason, ParentID: &series.ID, SeasonNum: 2, Title: "Season 2", Source: "tmdb"}
	if err := db.Create(&season1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&season2).Error; err != nil {
		t.Fatal(err)
	}
	episodes := []model.MetadataItem{
		{Kind: model.MetadataKindEpisode, ParentID: &season1.ID, EpisodeNum: 1, Title: "Episode 1", Source: "tmdb"},
		{Kind: model.MetadataKindEpisode, ParentID: &season1.ID, EpisodeNum: 2, Title: "Episode 2", Source: "tmdb"},
		{Kind: model.MetadataKindEpisode, ParentID: &season2.ID, EpisodeNum: 1, Title: "Episode 1", Source: "tmdb"},
	}
	if err := db.Create(&episodes).Error; err != nil {
		t.Fatal(err)
	}
	person := model.Person{Name: "演员", OriginalName: "Actor", NormalizedName: "actor", Source: "tmdb"}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	for _, episode := range episodes {
		credit := model.MetadataCredit{MetadataID: episode.ID, PersonID: person.ID, Type: model.CreditTypeActor, OriginalRole: "Same Role", Role: "Same Role"}
		if err := db.Create(&credit).Error; err != nil {
			t.Fatal(err)
		}
	}

	scraper := NewScraperService(&config.Config{}, zap.NewNop(), repos, nil, nil, nil, nil, nil)
	groups, err := scraper.pendingPeopleTranslationGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("role translation groups = %d, want 2", len(groups))
	}
	targetCounts := make(map[string]int, len(groups))
	for _, group := range groups {
		targetCounts[group.lookup.ContextKey] = len(group.targets)
	}
	if targetCounts[season1.ID] != 2 || targetCounts[season2.ID] != 1 {
		t.Fatalf("role translation target counts = %v", targetCounts)
	}
}

func TestTranslatePeopleWindowReturnsResultDetails(t *testing.T) {
	db := newServiceTestDB(t, &model.Person{}, &model.TranslationCache{})
	repos := repository.New(db)
	person := model.Person{Name: "Tony Leung Chiu-wai", OriginalName: "Tony Leung Chiu-wai"}
	if err := db.Create(&person).Error; err != nil {
		t.Fatal(err)
	}
	lookup := newTranslationCacheLookup("person_name", person.ID, person.OriginalName)
	if err := db.Create(&model.TranslationCache{Kind: lookup.Kind, ContextKey: lookup.ContextKey, SourceText: lookup.SourceText, TargetLanguage: lookup.TargetLanguage, PromptVersion: lookup.PromptVersion, TranslatedText: "梁朝伟"}).Error; err != nil {
		t.Fatal(err)
	}
	group := &pendingPeopleTranslation{lookup: lookup, targets: []repository.TranslationTarget{{Kind: "person_name", ID: person.ID, OriginalText: person.OriginalName}}}
	scraper := NewScraperService(&config.Config{}, zap.NewNop(), repos, nil, nil, nil, nil, nil)
	applied, details, err := scraper.translatePeopleWindow(t.Context(), []*pendingPeopleTranslation{group})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || !slices.Contains(details, "人物翻译 [缓存]: Tony Leung Chiu-wai -> 梁朝伟") {
		t.Fatalf("applied=%d details=%v", applied, details)
	}
}

func assertPeopleTranslationContext(t *testing.T, entries []AITranslationEntry) {
	t.Helper()
	if len(entries) != 2 {
		t.Fatalf("translation entries = %d, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.Context == nil {
			t.Fatalf("entry %q has no context", entry.Kind)
		}
		switch entry.Kind {
		case "person_name":
			if len(entry.Context.KnownFor) != 1 || entry.Context.KnownFor[0] != "无间道 (2002)" {
				t.Fatalf("person context = %+v", entry.Context)
			}
		case "role":
			if entry.Context.Title != "无间道" || entry.Context.OriginalTitle != "Infernal Affairs" || entry.Context.Year != 2002 || entry.Context.MediaKind != model.MetadataKindMovie {
				t.Fatalf("role context = %+v", entry.Context)
			}
		default:
			t.Fatalf("unexpected translation kind %q", entry.Kind)
		}
	}
}

func waitForPeopleTranslation(t *testing.T, db *gorm.DB, personName, role string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var person model.Person
		var credit model.MetadataCredit
		personErr := db.Model(&model.Person{}).Where("name = ?", personName).First(&person).Error
		creditErr := db.Model(&model.MetadataCredit{}).Where("role = ?", role).First(&credit).Error
		if personErr == nil && creditErr == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	var people []model.Person
	var credits []model.MetadataCredit
	var caches []model.TranslationCache
	_ = db.Find(&people).Error
	_ = db.Find(&credits).Error
	_ = db.Find(&caches).Error
	t.Fatalf("translations did not reach person=%q role=%q; people=%+v credits=%+v caches=%+v", personName, role, people, credits, caches)
}
