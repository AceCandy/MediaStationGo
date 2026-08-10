// Package model — STRM 文件记录数据模型。
package model

// STRMRecord 记录本地 STRM 文件及其 HTTP/HTTPS 播放地址。
type STRMRecord struct {
	Base
	Title      string `gorm:"size:512;not null;index" json:"title"`
	URL        string `gorm:"size:2048;not null" json:"url"`       // STRM 文件指向的 URL
	FilePath   string `gorm:"size:1024;not null" json:"file_path"` // 本地 STRM 文件路径
	Protocol   string `gorm:"size:32;not null" json:"protocol"`    // http / https
	FileSize   int64  `json:"file_size"`
	MediaID    string `gorm:"size:128;index" json:"media_id"` // 关联媒体 ID
	MediaType  string `gorm:"size:16" json:"media_type"`      // movie / series
	SeasonNum  int    `json:"season_num"`
	EpisodeNum int    `json:"episode_num"`
}
