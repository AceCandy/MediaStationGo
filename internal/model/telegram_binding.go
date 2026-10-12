package model

// TelegramBinding links a Telegram account to a local MediaStationGo user.
// The binding is password-verified when /start is used, then reused for
// low-risk self-service actions such as toggling adult-library visibility.
type TelegramBinding struct {
	Base
	TelegramUserID int64  `gorm:"uniqueIndex;not null" json:"telegram_user_id"`
	TelegramName   string `gorm:"size:128" json:"telegram_name,omitempty"`
	ChatID         int64  `gorm:"index" json:"chat_id"`
	UserID         string `gorm:"index;size:36;not null;uniqueIndex:idx_telegram_bindings_user_id_active,where:deleted_at IS NULL" json:"user_id"`
}
