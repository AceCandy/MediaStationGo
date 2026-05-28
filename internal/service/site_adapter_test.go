package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMTeamAuthenticateRequiresAPIKey(t *testing.T) {
	adapter := NewMTeamAdapter()
	err := adapter.Authenticate(context.Background(), SiteConfig{
		URL:      "https://api.m-team.cc",
		AuthType: "api_key",
	})
	if err == nil || !strings.Contains(err.Error(), "API Access Token") {
		t.Fatalf("Authenticate error = %v, want API Access Token hint", err)
	}
}

func TestMTeamAuthenticateUsesOpenAPIKeyHeader(t *testing.T) {
	var gotPath string
	var gotKey string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"0","message":"SUCCESS","data":{"total":"0","data":[]}}`))
	}))
	defer server.Close()

	adapter := NewMTeamAdapter()
	err := adapter.Authenticate(context.Background(), SiteConfig{
		URL:      server.URL,
		AuthType: "api_key",
		APIKey:   "token-123",
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Authenticate returned error: %v", err)
	}
	if gotPath != "/api/torrent/search" {
		t.Fatalf("path = %q, want /api/torrent/search", gotPath)
	}
	if gotKey != "token-123" {
		t.Fatalf("x-api-key = %q, want token-123", gotKey)
	}
	if gotPayload["mode"] != "all" || gotPayload["keyword"] != nil {
		t.Fatalf("payload = %#v, want mode all without keyword probe", gotPayload)
	}
}

func TestMTeamAuthenticateReportsAPIMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"message":"key無效","data":null}`))
	}))
	defer server.Close()

	adapter := NewMTeamAdapter()
	err := adapter.Authenticate(context.Background(), SiteConfig{
		URL:      server.URL,
		AuthType: "api_key",
		APIKey:   "bad-token",
		Timeout:  5 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "key無效") {
		t.Fatalf("Authenticate error = %v, want key invalid message", err)
	}
}
