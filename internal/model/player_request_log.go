package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PlayerRequestLog 保存播放器兼容 API 的脱敏请求信息。
// requested_at 同时是 PostgreSQL 月分区键，因此与 id 组成复合主键。
type PlayerRequestLog struct {
	ID          string              `gorm:"primaryKey;type:varchar(36)" json:"id"`
	RequestedAt time.Time           `gorm:"primaryKey;not null" json:"requested_at"`
	Method      string              `gorm:"size:16;not null" json:"method"`
	Route       string              `gorm:"type:text;not null" json:"route"`
	Status      int                 `gorm:"not null" json:"status"`
	DurationMS  int64               `gorm:"not null" json:"duration_ms"`
	IP          string              `gorm:"size:64;not null" json:"ip"`
	Body        string              `gorm:"type:text;not null;default:''" json:"body"`
	PathParams  map[string][]string `gorm:"serializer:json;type:jsonb;not null" json:"path_params"`
	Headers     map[string][]string `gorm:"serializer:json;type:jsonb;not null" json:"headers"`
	Query       map[string][]string `gorm:"serializer:json;type:jsonb;not null" json:"query"`
}

func (PlayerRequestLog) TableName() string { return "player_request_logs" }

func (p *PlayerRequestLog) BeforeCreate(_ *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	return nil
}
