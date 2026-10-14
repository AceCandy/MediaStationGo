package model

import "time"

// DiscoverRankingPage 保存来源榜单分页快照；成功时间不受缓存读取和失败回退影响。
type DiscoverRankingPage struct {
	Key       string    `gorm:"primaryKey;size:64"`
	Page      int       `gorm:"primaryKey"`
	Payload   string    `gorm:"type:jsonb;not null"`
	FetchedAt time.Time `gorm:"not null"`
}
