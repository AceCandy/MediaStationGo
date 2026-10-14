package model

import "time"

// MetadataIndexJob 是来源变化到搜索索引的持久待办，修订号防止处理期间的新变更被清除。
type MetadataIndexJob struct {
	MetadataID string    `gorm:"primaryKey;size:36"`
	Revision   int64     `gorm:"not null;default:1"`
	NextAt     time.Time `gorm:"not null;index"`
}
