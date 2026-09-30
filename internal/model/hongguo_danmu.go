package model

import "time"

// HongGuoDanmu 按源作品和集号积累弹幕，不随来源资料清理，不关联视频 ID。
type HongGuoDanmu struct {
	SourceID        string `gorm:"primaryKey;size:32"`
	EpisodeNumber   int    `gorm:"primaryKey;check:chk_hongguo_danmu_episode,episode_number > 0"`
	CommentID       string `gorm:"primaryKey;size:64"`
	OffsetMS        int64  `gorm:"not null;check:chk_hongguo_danmu_offset,offset_ms >= 0"`
	Content         string `gorm:"type:text;not null"`
	SourceCreatedAt int64  `gorm:"not null"`
	CreatedAt       time.Time
}

func (HongGuoDanmu) TableName() string { return "hongguo_danmus" }
