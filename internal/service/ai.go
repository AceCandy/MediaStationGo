// Package service — AI integration (OpenAI-compatible chat completions).
//
// AIService is a thin wrapper around any OpenAI-compatible REST endpoint
// (OpenAI, DeepSeek, Qwen, Ollama, …). Today we expose two operations:
//
//   - SmartSearch:    interpret a free-form Chinese / English query and
//     return a normalised JSON intent the React UI can
//     translate into filter params.
//   - Recommend:      given a list of recently-watched titles, generate
//     a short list of "you might like…" recommendations.
//
// The service is disabled (every method returns nil) when ai.enabled is
// false or ai.api_key is empty.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// AIService talks to OpenAI-compatible chat-completions and Responses endpoints.
type AIService struct {
	cfg       *config.Config
	log       *zap.Logger
	client    *http.Client
	apiConfig *APIConfigService
}

// NewAIService is the constructor.
func NewAIService(cfg *config.Config, log *zap.Logger, apiConfig *APIConfigService) *AIService {
	timeout := time.Duration(cfg.AI.Timeout) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultAITimeoutSeconds) * time.Second
	}
	return &AIService{
		cfg:       cfg,
		log:       log,
		apiConfig: apiConfig,
		client:    NewExternalHTTPClient(timeout),
	}
}

// Enabled reports whether the AI integration is configured.
func (a *AIService) Enabled() bool {
	return a.cfg.AI.Enabled && strings.TrimSpace(a.cfg.AI.APIKey) != ""
}

// EnabledFor reports whether the AI integration is configured for a request.
func (a *AIService) EnabledFor(ctx context.Context) bool {
	return a.resolveRuntimeConfig(ctx).Enabled
}

// AIStatus is returned to the UI for connection-state display.
type AIStatus struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Status resolves live database-backed AI config for the UI.
func (a *AIService) Status(ctx context.Context) AIStatus {
	cfg := a.resolveRuntimeConfig(ctx)
	return AIStatus{Enabled: cfg.Enabled, Provider: cfg.Provider, Model: cfg.Model}
}

// SearchIntent is the structured output the smart search endpoint returns.
type SearchIntent struct {
	Query    string `json:"query"`
	Year     int    `json:"year,omitempty"`
	Genre    string `json:"genre,omitempty"`
	Type     string `json:"type,omitempty"` // movie / tv / anime / music
	Sort     string `json:"sort,omitempty"` // recent / rating / random
	Language string `json:"language,omitempty"`
}

type AITranslationEntry struct {
	Key     string                `json:"key"`
	Kind    string                `json:"kind"`
	Text    string                `json:"text"`
	Context *AITranslationContext `json:"context,omitempty"`
}

// AITranslationContext 提供人物或角色所属作品的消歧线索。
type AITranslationContext struct {
	Title         string   `json:"title,omitempty"`
	OriginalTitle string   `json:"original_title,omitempty"`
	Year          int      `json:"year,omitempty"`
	MediaKind     string   `json:"media_kind,omitempty"`
	KnownFor      []string `json:"known_for,omitempty"`
}

// TranslatePeople returns only validated non-empty translations. It is best effort.
func (a *AIService) TranslatePeople(ctx context.Context, entries []AITranslationEntry) (map[string]string, error) {
	result := make(map[string]string)
	if a == nil || len(entries) == 0 || !a.EnabledFor(ctx) {
		return result, nil
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return result, err
	}
	system := "Translate the supplied person names and actor roles into accurate Simplified Chinese. Use each entry's work context only to disambiguate established person names and character roles. Return a JSON object mapping every exact key to one non-empty translated string. Preserve text that is already Chinese. JSON only."
	out, err := a.completeResponses(ctx, a.resolveRuntimeConfig(ctx), system, string(payload), nil)
	if err != nil {
		return result, err
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return result, err
	}
	valid := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		valid[entry.Key] = struct{}{}
	}
	for key, value := range raw {
		if _, ok := valid[key]; !ok {
			continue
		}
		text, ok := value.(string)
		if ok && strings.TrimSpace(text) != "" {
			result[key] = strings.TrimSpace(text)
		}
	}
	return result, nil
}

// SmartSearch turns a natural-language query into a structured intent.
// Returns a best-effort intent on parse failure (raw query passes through).
func (a *AIService) SmartSearch(ctx context.Context, raw string) (*SearchIntent, error) {
	runtime := a.resolveRuntimeConfig(ctx)
	if !runtime.Enabled {
		return &SearchIntent{Query: raw}, nil
	}
	const sys = "You are a media-library search assistant. Read the user's query and " +
		"output a JSON object with the keys: query (string), year (int, optional), " +
		"genre (string, optional), type (movie|tv|anime|music, optional), sort " +
		"(recent|rating|random, optional), language (zh|en, optional). Respond with " +
		"JSON only, no commentary."
	out, err := a.complete(ctx, runtime, sys, raw)
	if err != nil {
		return &SearchIntent{Query: raw}, err
	}
	var intent SearchIntent
	if err := json.Unmarshal([]byte(out), &intent); err != nil {
		// Fallback: tolerate non-JSON output by treating the raw text as
		// the cleaned query.
		intent.Query = strings.TrimSpace(out)
	}
	if intent.Query == "" {
		intent.Query = raw
	}
	return &intent, nil
}

// Recommend builds a short comma-separated list of titles given the user's
// history. The first call is intentionally best-effort: a future iteration
// may chain media DB lookups onto each suggestion.
func (a *AIService) Recommend(ctx context.Context, history []string, max int) ([]string, error) {
	runtime := a.resolveRuntimeConfig(ctx)
	if !runtime.Enabled || len(history) == 0 {
		return nil, nil
	}
	if max <= 0 || max > 20 {
		max = 8
	}
	sys := fmt.Sprintf("You are a film / TV recommendation assistant. Reply with %d "+
		"comma-separated titles only, no commentary, in the same language as the input.", max)
	usr := "I recently watched: " + strings.Join(history, "; ")
	out, err := a.complete(ctx, runtime, sys, usr)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out, ",")
	titles := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "\"'`")
		if p != "" {
			titles = append(titles, p)
		}
	}
	return titles, nil
}

// complete is the shared helper — POST /v1/chat/completions.
func (a *AIService) complete(ctx context.Context, runtime aiRuntimeConfig, system, user string) (string, error) {
	payload := map[string]any{
		"model":       runtime.Model,
		"temperature": 0.2,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	body, _ := json.Marshal(payload)
	endpoint := strings.TrimRight(runtime.APIBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runtime.APIKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ai %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	type choice struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	var out struct {
		Choices []choice `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("ai: empty completion")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// ChatTurn is one message in a multi-turn assistant transcript.
type ChatTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chat sends an entire transcript to the LLM. When the AI is disabled
// we return a deterministic offline reply so the assistant UI still
// has something to render.
func (a *AIService) Chat(ctx context.Context, history []ChatTurn) (string, error) {
	runtime := a.resolveRuntimeConfig(ctx)
	if !runtime.Enabled || len(history) == 0 {
		return offlineReply(history), nil
	}
	if runtime.WebSearchEnabled {
		return a.chatWithWebSearch(ctx, runtime, history)
	}
	// Build a chat/completions payload preserving the history order.
	msgs := make([]map[string]string, 0, len(history)+1)
	msgs = append(msgs, map[string]string{
		"role": "system",
		"content": "You are MediaStationGo's helpful media-library assistant. " +
			"Respond concisely in the user's language. " +
			"Never invent file paths or media that don't exist.",
	})
	for _, t := range history {
		msgs = append(msgs, map[string]string{"role": t.Role, "content": t.Content})
	}
	payload := map[string]any{
		"model":       runtime.Model,
		"temperature": 0.4,
		"messages":    msgs,
	}
	body, _ := json.Marshal(payload)
	endpoint := strings.TrimRight(runtime.APIBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runtime.APIKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ai %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	type choice struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	var out struct {
		Choices []choice `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("ai: empty completion")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// chatWithWebSearch uses the Responses API because hosted web search is not
// available through Chat Completions.
func (a *AIService) chatWithWebSearch(ctx context.Context, runtime aiRuntimeConfig, history []ChatTurn) (string, error) {
	return a.completeResponses(ctx, runtime, "You are MediaStationGo's helpful media-library assistant. "+
		"Respond concisely in the user's language. "+
		"Never invent file paths or media that don't exist.", history,
		[]map[string]string{{"type": "web_search"}})
}

// completeResponses sends one non-streaming Responses API request and returns
// the text from its message output. A nil tools slice keeps the request offline.
func (a *AIService) completeResponses(ctx context.Context, runtime aiRuntimeConfig, instructions string, input any, tools []map[string]string) (string, error) {
	payload := map[string]any{
		"model":        runtime.Model,
		"instructions": instructions,
		"input":        input,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	body, _ := json.Marshal(payload)
	endpoint := strings.TrimRight(runtime.APIBase, "/") + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runtime.APIKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ai responses %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	parts := make([]string, 0, 1)
	for _, item := range out.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, strings.TrimSpace(content.Text))
			}
		}
	}
	if len(parts) == 0 {
		return "", errors.New("ai responses: empty output")
	}
	return strings.Join(parts, "\n"), nil
}

// offlineReply returns a deterministic stand-in response so the UI's
// chat view stays functional when the AI provider is not configured.
func offlineReply(history []ChatTurn) string {
	if len(history) == 0 {
		return "Hi — AI provider is not configured. Set up OpenAI/DeepSeek in API Configs to chat with me."
	}
	last := history[len(history)-1].Content
	if len(last) > 80 {
		last = last[:80] + "…"
	}
	return "(offline) Heard: " + last + "\n请在 API 配置中接入 LLM 后重试。"
}
