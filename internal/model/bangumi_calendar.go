package model

import "time"

// BangumiCalendarSnapshot 保存公共周历；刷新失败不删除最近成功的数据。
type BangumiCalendarSnapshot struct {
	Key       string    `gorm:"primaryKey;size:32"`
	Payload   string    `gorm:"type:jsonb;not null"`
	FetchedAt time.Time `gorm:"not null"`
}
