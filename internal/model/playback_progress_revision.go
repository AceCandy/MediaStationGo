package model

// PlaybackProgressRevision 保留用户逻辑条目的进度版本；删除历史后仍保留，防止旧同步复活进度。
type PlaybackProgressRevision struct {
	UserID        string `gorm:"primaryKey;size:36"`
	Source        string `gorm:"primaryKey;size:16"`
	ItemID        string `gorm:"primaryKey;size:128"`
	EpisodeNumber int    `gorm:"primaryKey"`
	Revision      int64  `gorm:"not null;default:0"`
}
