package service

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestChannelSubscribesCanDisableAllEvents(t *testing.T) {
	channel := model.NotifyChannel{Events: `["` + NotifyEventNone + `"]`}

	if channelSubscribes(channel, EventScrapeFailed) {
		t.Fatal("explicit none sentinel should disable event pushes")
	}
}

func TestChannelSubscribesKeepsLegacyEmptyAsAllEvents(t *testing.T) {
	for _, raw := range []string{"", "[]"} {
		channel := model.NotifyChannel{Events: raw}
		if !channelSubscribes(channel, EventScrapeFailed) {
			t.Fatalf("legacy events %q should still subscribe to all events", raw)
		}
	}
}

func TestChannelSubscribesSupportsExplicitAllAndSpecificEvents(t *testing.T) {
	all := model.NotifyChannel{Events: `["` + NotifyEventAll + `"]`}
	if !channelSubscribes(all, EventScrapeFailed) {
		t.Fatal("explicit all sentinel should subscribe to every event")
	}

	specific := model.NotifyChannel{Events: `["` + EventScrapeFailed + `"]`}
	if !channelSubscribes(specific, EventScrapeFailed) {
		t.Fatal("specific event should be subscribed")
	}
	if channelSubscribes(specific, EventSystemAlert) {
		t.Fatal("unlisted event should not be subscribed")
	}
}

// TestTelegramMediaTemplateRendersEnrichedFields 验证补齐的媒体字段
// (年份/评分/类型/原名/语言)确实透传进 Telegram 富模板 caption。
// 这是「模型/刮削链路补齐字段」与「作者富模板」对接的端到端断言。
