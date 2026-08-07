package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestAIStatusUsesDatabaseOpenAIConfig(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	repo := &repository.Container{DB: db}
	crypto := NewCryptoService("test-secret", zap.NewNop())
	apiConfig := NewAPIConfigService(zap.NewNop(), repo, crypto)
	key := "sk-test"
	baseURL := "https://example.test/v1"
	enabled := true
	if _, err := apiConfig.Update(context.Background(), "openai", APIConfigPatch{
		APIKey:  &key,
		BaseURL: &baseURL,
		Enabled: &enabled,
	}); err != nil {
		t.Fatal(err)
	}

	ai := NewAIService(&config.Config{
		AI: config.AIConfig{
			Enabled: false,
			Model:   "gpt-4o-mini",
		},
	}, zap.NewNop(), apiConfig)

	status := ai.Status(context.Background())
	if !status.Enabled {
		t.Fatalf("AI status disabled, want enabled from database config")
	}
	if status.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", status.Provider)
	}
}

func TestNewAIServiceDefaultsTo120SecondTimeout(t *testing.T) {
	ai := NewAIService(&config.Config{}, zap.NewNop(), nil)
	if ai.client.Timeout != time.Duration(config.DefaultAITimeoutSeconds)*time.Second {
		t.Fatalf("AI client timeout = %s, want 120s", ai.client.Timeout)
	}
}

func TestAIStatusHonorsDisabledDatabaseOpenAIConfig(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	repo := &repository.Container{DB: db}
	apiConfig := NewAPIConfigService(zap.NewNop(), repo, NewCryptoService("test-secret", zap.NewNop()))
	key := "sk-test"
	enabled := false
	if _, err := apiConfig.Update(context.Background(), "openai", APIConfigPatch{
		APIKey:  &key,
		Enabled: &enabled,
	}); err != nil {
		t.Fatal(err)
	}

	ai := NewAIService(&config.Config{
		AI: config.AIConfig{
			Enabled: true,
			APIKey:  "sk-file",
			Model:   "gpt-4o-mini",
		},
	}, zap.NewNop(), apiConfig)

	if ai.Status(context.Background()).Enabled {
		t.Fatalf("AI status enabled, want disabled when database config is explicitly disabled")
	}
}

func TestAPIConfigUpdateResolvesAIOptions(t *testing.T) {
	db := newServiceTestDB(t, &model.APIConfig{})
	apiConfig := NewAPIConfigService(zap.NewNop(), &repository.Container{DB: db}, NewCryptoService("test-secret", zap.NewNop()))
	modelName := "  gpt-5.6  "
	webSearchEnabled := true

	view, err := apiConfig.Update(t.Context(), "openai", APIConfigPatch{
		Model:            &modelName,
		WebSearchEnabled: &webSearchEnabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Model != "gpt-5.6" || !view.WebSearchEnabled {
		t.Fatalf("public options = model %q, web search %v", view.Model, view.WebSearchEnabled)
	}

	resolved, err := apiConfig.Resolve(t.Context(), "openai")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Model != "gpt-5.6" || !resolved.WebSearchEnabled {
		t.Fatalf("resolved options = model %q, web search %v", resolved.Model, resolved.WebSearchEnabled)
	}
}

func TestAIChatUsesResponsesWebSearch(t *testing.T) {
	ai := newConfiguredAITestService(t, true, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want /v1/responses", r.URL.Path)
		}
		var payload struct {
			Model string     `json:"model"`
			Input []ChatTurn `json:"input"`
			Tools []struct {
				Type string `json:"type"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload.Model != "gpt-5.6" || len(payload.Input) != 1 || len(payload.Tools) != 1 || payload.Tools[0].Type != "web_search" {
			t.Errorf("unexpected responses payload: %+v", payload)
		}
		_, _ = w.Write([]byte(`{"output":[{"type":"web_search_call"},{"type":"message","content":[{"type":"output_text","text":"联网结果"}]}]}`))
	})

	out, err := ai.Chat(t.Context(), []ChatTurn{{Role: "user", Content: "今天有什么电影新闻？"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "联网结果" {
		t.Fatalf("chat output = %q", out)
	}
}

func TestAIChatWithoutWebSearchUsesChatCompletions(t *testing.T) {
	ai := newConfiguredAITestService(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"普通回答"}}]}`))
	})

	out, err := ai.Chat(t.Context(), []ChatTurn{{Role: "user", Content: "你好"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "普通回答" {
		t.Fatalf("chat output = %q", out)
	}
}

func TestAITranslatePeopleUsesResponsesWithoutWebSearch(t *testing.T) {
	ai := newConfiguredAITestService(t, true, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want /v1/responses", r.URL.Path)
		}
		var payload struct {
			Tools []map[string]string `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(payload.Tools) != 0 {
			t.Errorf("translation tools = %+v, want none", payload.Tools)
		}
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"{\"person-1\":\"周润发\"}"}]}]}`))
	})

	out, err := ai.TranslatePeople(t.Context(), []AITranslationEntry{{Key: "person-1", Text: "Chow Yun-fat"}})
	if err != nil {
		t.Fatal(err)
	}
	if out["person-1"] != "周润发" {
		t.Fatalf("translation = %q", out["person-1"])
	}
}

func newConfiguredAITestService(t *testing.T, webSearchEnabled bool, handler http.HandlerFunc) *AIService {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	db := newServiceTestDB(t, &model.APIConfig{})
	apiConfig := NewAPIConfigService(zap.NewNop(), &repository.Container{DB: db}, NewCryptoService("test-secret", zap.NewNop()))
	key := "sk-test"
	baseURL := server.URL + "/v1"
	modelName := "gpt-5.6"
	enabled := true
	if _, err := apiConfig.Update(t.Context(), "openai", APIConfigPatch{
		APIKey:           &key,
		BaseURL:          &baseURL,
		Model:            &modelName,
		Enabled:          &enabled,
		WebSearchEnabled: &webSearchEnabled,
	}); err != nil {
		t.Fatal(err)
	}
	return NewAIService(&config.Config{}, zap.NewNop(), apiConfig)
}
