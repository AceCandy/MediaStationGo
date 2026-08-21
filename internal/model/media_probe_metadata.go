package model

import "time"

// MediaProbeMetadata 保存单个可播放媒体的完整 ffprobe 文档。
// MediaID 同时作为主键和外键，确保每个 Media 最多只有一份探测记录。
type MediaProbeMetadata struct {
	MediaID        string    `gorm:"primaryKey;type:varchar(36);not null" json:"media_id"`
	Media          *Media    `gorm:"foreignKey:MediaID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	ProbeJSON      string    `gorm:"type:text;not null" json:"-"`
	SchemaVersion  int       `gorm:"not null" json:"schema_version"`
	SummaryVersion int       `gorm:"not null;default:0" json:"summary_version"`
	DurationMS     int64     `gorm:"not null;default:0" json:"duration_ms"`
	SizeBytes      int64     `gorm:"not null;default:0" json:"size_bytes"`
	Container      string    `gorm:"size:128;not null;default:''" json:"container,omitempty"`
	BitRate        int64     `gorm:"not null;default:0" json:"bit_rate"`
	Width          int       `gorm:"not null;default:0" json:"width"`
	Height         int       `gorm:"not null;default:0" json:"height"`
	VideoCodec     string    `gorm:"size:32;not null;default:''" json:"video_codec,omitempty"`
	AudioCodec     string    `gorm:"size:32;not null;default:''" json:"audio_codec,omitempty"`
	ProbedAt       time.Time `gorm:"not null" json:"probed_at"`
}

func (MediaProbeMetadata) TableName() string {
	return "media_probe_metadata"
}
