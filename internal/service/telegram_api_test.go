package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTelegramMethodURLUsesCustomAPIBase(t *testing.T) {
	got, err := telegramMethodURL(map[string]string{
		"api_base_url": "https://tg.example.com/",
	}, "123456:ABC-def", "sendMessage")
	if err != nil {
		t.Fatalf("telegramMethodURL returned error: %v", err)
	}
	want := "https://tg.example.com/bot123456:ABC-def/sendMessage"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeTelegramErrorRedactsBotToken(t *testing.T) {
	err := sanitizeTelegramError(errors.New(`Post "https://api.telegram.org/bot123456:SECRET/sendMessage": context deadline exceeded`))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "SECRET") || strings.Contains(msg, "123456:") {
		t.Fatalf("telegram token leaked in error: %s", msg)
	}
	if !strings.Contains(msg, "timeout") {
		t.Fatalf("expected timeout hint, got: %s", msg)
	}
}

func TestValidateTelegramChannelDoesNotRequireLegacyChatID(t *testing.T) {
	err := validateChannel(ChannelInput{
		Name: "Telegram",
		Type: "telegram",
		Config: map[string]any{
			"bot_token":      "123456:ABC-def",
			"admin_user_ids": "10001",
		},
	})
	if err != nil {
		t.Fatalf("validateChannel returned error: %v", err)
	}
}

func TestTelegramTargetChatIDsFallsBackToAdmins(t *testing.T) {
	got := telegramTargetChatIDs(map[string]string{
		"admin_user_ids": "10001, 10002",
	})
	if len(got) != 2 || got[0] != "10001" || got[1] != "10002" {
		t.Fatalf("got %#v, want admin user ids", got)
	}
}

func TestNormalizeTelegramChannelMigratesLegacyChatID(t *testing.T) {
	input := ChannelInput{
		Name: "Telegram",
		Type: "telegram",
		Config: map[string]any{
			"chat_id": "-10001",
		},
	}
	normalizeChannelInput(&input)
	if got := str(input.Config["group_chat_id"]); got != "-10001" {
		t.Fatalf("group_chat_id = %q, want -10001", got)
	}
}

func TestNormalizeTelegramChannelMigratesLegacyPrivateChatIDToAdmin(t *testing.T) {
	cfg := map[string]string{"chat_id": "5812333517"}
	normalizeTelegramConfig(cfg)
	if got := cfg["admin_user_ids"]; got != "5812333517" {
		t.Fatalf("admin_user_ids = %q, want legacy chat_id", got)
	}
}

func TestTelegramTargetChatIDsUsesLegacyPrivateChatID(t *testing.T) {
	got := telegramTargetChatIDs(map[string]string{
		"chat_id": "5812333517",
	})
	if len(got) != 1 || got[0] != "5812333517" {
		t.Fatalf("got %#v, want legacy private chat target", got)
	}
}

func TestTelegramProxyCandidatesDefaultLocalFallbacks(t *testing.T) {
	got := telegramProxyCandidates(map[string]string{})
	joined := strings.Join(got, ",")
	for _, want := range []string{"host.docker.internal:20171", "socks5://172.17.0.1:20170", "127.0.0.1:10808", "172.17.0.1:7890"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("default proxy candidates %q missing %q", joined, want)
		}
	}
}

func TestTelegramHTTPClientsPreferConfiguredProxy(t *testing.T) {
	clients := telegramHTTPClients(time.Second, map[string]string{
		"proxy_url": "http://proxy.example:7890",
	})
	if len(clients) == 0 {
		t.Fatal("expected telegram clients")
	}
	if got := telegramClientProxyString(t, clients[0]); got != "http://proxy.example:7890" {
		t.Fatalf("first client proxy = %q, want configured proxy", got)
	}
}

func telegramClientProxyString(t *testing.T, client *http.Client) string {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		return ""
	}
	req, err := http.NewRequest(http.MethodGet, defaultTelegramAPIBaseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil {
		return ""
	}
	return proxyURL.String()
}

func TestTelegramReplyAutoDeletesSentMessage(t *testing.T) {
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			requests <- "sendMessage"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":777}}`))
		case strings.HasSuffix(r.URL.Path, "/deleteMessage"):
			requests <- "deleteMessage"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, _ := json.Marshal(map[string]string{
		"bot_token":           "123456:ABC-def",
		"api_base_url":        server.URL,
		"auto_delete_seconds": "0",
	})
	_, bot := newBotTestService(t)
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: string(cfg)}
	if err := bot.reply(context.Background(), channel, 42, telegramCommandReply{Text: "hello"}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitForTelegramMethod(t, requests, "sendMessage")
	waitForTelegramMethod(t, requests, "deleteMessage")
}

func waitForTelegramMethod(t *testing.T, requests <-chan string, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case got := <-requests:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for telegram %s", want)
		}
	}
}

func TestTelegramCommandFiltering(t *testing.T) {
	if telegramIsCommandText("今天看什么") {
		t.Fatal("plain chat message should not be treated as command")
	}
	if !telegramIsCommandText("/start user pass") {
		t.Fatal("/start should be treated as command")
	}
	if got := telegramCommandName("/hideadult@MediaStationGoBot on"); got != "/hideadult" {
		t.Fatalf("telegramCommandName = %q, want /hideadult", got)
	}
	if telegramSupportedCommand("/签到") {
		t.Fatal("unrelated group bot command should not be handled")
	}
	for _, cmd := range []string{"/signin", "/redeem", "/gencode", "/users", "/renew_user", "/delete_user", "/cleanup_rule"} {
		if !telegramSupportedCommand(cmd) {
			t.Fatalf("%s should be supported so group slash commands get feedback", cmd)
		}
	}
}
