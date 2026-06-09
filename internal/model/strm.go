// Package model — STRM 文件记录数据模型。
package model

// STRMRecord STRM 文件记录。
// 外部存储以"文件"形式入库，URL 指向实际资源。
type STRMRecord struct {
	Base
	Title      string `gorm:"size:512;not null;index" json:"title"`
	URL        string `gorm:"size:2048;not null" json:"url"`       // STRM 文件指向的 URL
	FilePath   string `gorm:"size:1024;not null" json:"file_path"` // 本地 STRM 文件路径
	Protocol   string `gorm:"size:32;not null" json:"protocol"`    // webdav / alist / s3 / http / https
	FileSize   int64  `json:"file_size"`
	MediaID    string `gorm:"size:36;index" json:"media_id"` // 关联媒体 ID
	MediaType  string `gorm:"size:16" json:"media_type"`     // movie / series
	SeasonNum  int    `json:"season_num"`
	EpisodeNum int    `json:"episode_num"`
}

// AllowedSTRMProtocols 协议白名单。
var AllowedSTRMProtocols = []string{
	"webdav", "davs",
	"alist", "alists",
	"openlist", "openlists",
	"s3",
	"http", "https",
}

// IsAllowedProtocol 检查协议是否在白名单中。
func IsAllowedProtocol(protocol string) bool {
	for _, p := range AllowedSTRMProtocols {
		if p == protocol {
			return true
		}
	}
	return false
}
