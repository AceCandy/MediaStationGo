package model

import "time"

// HongGuoUserState 属于用户数据，不随资料停用/卸载级联删除。
// 源作品 ID 与源集号构成稳定身份；0 仅用于作品收藏，电影进度仍归源第 1 集。
type HongGuoUserState struct {
	UserID        string     `gorm:"primaryKey;size:36" json:"user_id"`
	SourceID      string     `gorm:"primaryKey;size:32" json:"source_id"`
	EpisodeNumber int        `gorm:"primaryKey;check:chk_hongguo_state_episode,episode_number >= 0" json:"episode_number"`
	Favorite      bool       `gorm:"not null;default:false" json:"favorite"`
	MediaID       string     `gorm:"size:128" json:"media_id"`
	PositionMs    int64      `gorm:"not null;default:0" json:"position_ms"`
	DurationMs    int64      `gorm:"not null;default:0" json:"duration_ms"`
	Completed     bool       `gorm:"not null;default:false" json:"completed"`
	WatchedAt     *time.Time `gorm:"index" json:"watched_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (HongGuoUserState) TableName() string { return "hongguo_user_states" }

// HongGuoPlaybackEvent 保存独立播放会话事实，文件删除和手动取消已看不删除事件。
type HongGuoPlaybackEvent struct {
	PermanentBase
	UserID        string    `gorm:"size:36;not null;uniqueIndex:uidx_hongguo_playback_event,priority:1;index" json:"user_id"`
	SessionID     string    `gorm:"size:128;not null;uniqueIndex:uidx_hongguo_playback_event,priority:2" json:"session_id"`
	SourceID      string    `gorm:"size:32;not null;uniqueIndex:uidx_hongguo_playback_event,priority:3" json:"source_id"`
	EpisodeNumber int       `gorm:"not null;uniqueIndex:uidx_hongguo_playback_event,priority:4" json:"episode_number"`
	MediaID       string    `gorm:"size:128;not null" json:"media_id"`
	LibraryID     string    `gorm:"size:36;not null;index" json:"library_id"`
	PlayedAt      time.Time `gorm:"not null;index" json:"played_at"`
}

func (HongGuoPlaybackEvent) TableName() string { return "hongguo_playback_events" }
