package model

import "time"

// HongGuoDownloadWork 固定作品路径；从未开始的整剧可随目录规则迁移，开始后不再变更。
type HongGuoDownloadWork struct {
	SourceID  string    `gorm:"primaryKey;size:32" json:"source_id"`
	Title     string    `gorm:"type:text;not null" json:"title"`
	Root      string    `gorm:"type:text;not null" json:"root"`
	Directory string    `gorm:"type:text;not null" json:"directory"`
	CreatedAt time.Time `json:"created_at"`
}

// HongGuoDownload 是分集业务队列；地址和媒体密钥不持久化，也不从执行日志恢复。
type HongGuoDownload struct {
	PermanentBase
	SourceID     string     `gorm:"size:32;not null;uniqueIndex:uidx_hg_download_episode,priority:1" json:"source_id"`
	Episode      int        `gorm:"not null;uniqueIndex:uidx_hg_download_episode,priority:2" json:"episode"`
	VideoID      string     `gorm:"size:32" json:"-"`
	Title        string     `gorm:"type:text" json:"title"`
	Root         string     `gorm:"type:text" json:"-"`
	RelativePath string     `gorm:"type:text" json:"relative_path"`
	Status       string     `gorm:"size:24;not null;index:idx_hg_download_due,priority:1" json:"status"`
	Bytes        int64      `json:"bytes"`
	TotalBytes   int64      `json:"total_bytes"`
	Attempts     int        `json:"attempts"`
	Error        string     `gorm:"type:text" json:"error"`
	LeaseToken   string     `gorm:"size:36" json:"-"`
	LeaseUntil   *time.Time `gorm:"index:idx_hg_download_due,priority:2" json:"-"`
	SHA256       string     `gorm:"size:64" json:"-"`
	VerifiedSize int64      `json:"-"`
	StagingPath  string     `gorm:"type:text" json:"-"`
	// RawSize 标记已同步磁盘、等待校验的原始文件；密钥恢复时重新向原来源读取。
	RawSize     int64   `gorm:"not null;default:0" json:"-"`
	Source      string  `gorm:"size:16" json:"source"`
	SourceTries int     `gorm:"not null;default:0" json:"-"`
	Encrypted   bool    `json:"-"`
	Duration    float64 `json:"-"`
	// 仅记录安全的媒体属性和每个来源最近一次失败，不含播放地址及解密材料。
	Quality      int               `json:"quality"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	Codec        string            `gorm:"size:32" json:"codec"`
	SourceErrors map[string]string `gorm:"serializer:json;type:text" json:"source_errors"`
}
