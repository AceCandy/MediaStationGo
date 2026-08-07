package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestPeopleTranslationWorkerUsesContextAndCache(t *testing.T) {
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
	ctx, cancel := context.WithCancel(t.Context())
	scraper.StartPeopleTranslationWorker(ctx)
	t.Cleanup(func() {
		cancel()
		scraper.WaitPeopleTranslationWorker()
	})

	if err := scraper.persistCredits(t.Context(), metadata.ID, []string{model.CreditTypeActor}, []PersonCredit{{
		Provider: "tmdb", ExternalID: "140", Name: "Tony Leung Chiu-wai", Type: model.CreditTypeActor, OriginalRole: "Chan Wing-yan",
	}}); err != nil {
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
	scraper.queuePeopleTranslation()
	waitForPeopleTranslation(t, db, "梁朝伟", "陈永仁")
	if got := calls.Load(); got != 1 {
		t.Fatalf("AI request count = %d, want 1 after cache hit", got)
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

func TestPeopleTranslationRetryDelay(t *testing.T) {
	want := []time.Duration{time.Minute, 3 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for failures, expected := range want {
		if got := peopleTranslationRetryDelay(failures); got != expected {
			t.Fatalf("retry delay after %d failures = %s, want %s", failures, got, expected)
		}
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
