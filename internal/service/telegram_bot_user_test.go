package service

import (
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTelegramStartClearsStaleUserBinding(t *testing.T) {
	repos, _, _, _ := newAuthTestServices(t)
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 20001,
		TelegramName:   "@viewer",
		ChatID:         20001,
		UserID:         "deleted-user",
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	bot := NewTelegramBotService(zap.NewNop(), repos, nil)

	reply := bot.cmdStart(t.Context(), &TelegramMessage{
		From: TelegramUser{ID: 20001, Username: "viewer", FirstName: "Viewer"},
		Chat: TelegramChat{ID: 20001, Type: "private"},
	}, nil)

	if !strings.Contains(reply.Text, "已不存在") {
		t.Fatalf("expected stale binding message, got %q", reply.Text)
	}
	var count int64
	if err := repos.DB.Model(&model.TelegramBinding{}).Where("telegram_user_id = ?", 20001).Count(&count).Error; err != nil {
		t.Fatalf("count binding: %v", err)
	}
	if count != 0 {
		t.Fatalf("stale binding should be removed, got %d", count)
	}
}
