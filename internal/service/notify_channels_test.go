package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func TestChannelSubscribesCanDisableAllEvents(t *testing.T) {
	channel := model.NotifyChannel{Events: `["` + NotifyEventNone + `"]`}

	if channelSubscribes(channel, EventDownloadComplete) {
		t.Fatal("explicit none sentinel should disable event pushes")
	}
}

func TestChannelSubscribesKeepsLegacyEmptyAsAllEvents(t *testing.T) {
	for _, raw := range []string{"", "[]"} {
		channel := model.NotifyChannel{Events: raw}
		if !channelSubscribes(channel, EventDownloadComplete) {
			t.Fatalf("legacy events %q should still subscribe to all events", raw)
		}
	}
}

func TestChannelSubscribesSupportsExplicitAllAndSpecificEvents(t *testing.T) {
	all := model.NotifyChannel{Events: `["` + NotifyEventAll + `"]`}
	if !channelSubscribes(all, EventScrapeFailed) {
		t.Fatal("explicit all sentinel should subscribe to every event")
	}

	specific := model.NotifyChannel{Events: `["` + EventDownloadComplete + `"]`}
	if !channelSubscribes(specific, EventDownloadComplete) {
		t.Fatal("specific event should be subscribed")
	}
	if channelSubscribes(specific, EventScrapeFailed) {
		t.Fatal("unlisted event should not be subscribed")
	}
}

func TestTelegramDispatchUsesPhotoAndFormattedCaption(t *testing.T) {
	var gotPath string
	var gotForm map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = map[string]string{}
		for key := range r.Form {
			gotForm[key] = r.Form.Get(key)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	svc := NewNotifyChannelService(zap.NewNop(), nil)
	channel := model.NotifyChannel{
		Type: "telegram",
		Config: `{
			"bot_token":"123456:ABC",
			"group_chat_id":"-10001",
			"api_base_url":"` + server.URL + `"
		}`,
	}
	err := svc.dispatchOneEvent(t.Context(), channel, NotifyEvent{
		Type:    EventDownloadComplete,
		Title:   "MediaStationGo 下载完成",
		Message: "任务：示例影片\n保存路径：/downloads/movie\nHash：abcdef",
		Data: map[string]interface{}{
			"poster_url": server.URL + "/poster.jpg",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/bot123456:ABC/sendPhoto" {
		t.Fatalf("path = %q, want sendPhoto", gotPath)
	}
	if gotForm["chat_id"] != "-10001" || gotForm["photo"] == "" {
		t.Fatalf("telegram form = %#v", gotForm)
	}
	caption := gotForm["caption"]
	for _, want := range []string{"<b>MediaStationGo</b>", "<b>下载完成</b>", "<b>任务</b>: 示例影片", "<code>/downloads/movie</code>", "<code>abcdef</code>"} {
		if !strings.Contains(caption, want) {
			t.Fatalf("caption missing %q: %s", want, caption)
		}
	}
}
