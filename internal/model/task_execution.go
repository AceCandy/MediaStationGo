package model

import (
	"strings"
	"time"
)

const (
	TaskSystemCommon  = "common"
	TaskSystemCatalog = "catalog"
	TaskSystemHongGuo = "hongguo"
	TaskSystemNFO     = "nfo"
)

// CommonTaskKinds 是不归属特定资料库的公共文件与账号任务。
var CommonTaskKinds = []string{"organize", "probe", "scan", "watch", "cleanup"}

// TaskSystemForKind 同时服务新执行写入和历史空归属记录的兼容读取。
func TaskSystemForKind(kind string) string {
	if strings.HasPrefix(kind, "nfo_") {
		return TaskSystemNFO
	}
	if strings.HasPrefix(kind, "hongguo_") {
		return TaskSystemHongGuo
	}
	for _, common := range CommonTaskKinds {
		if common == kind {
			return TaskSystemCommon
		}
	}
	return TaskSystemCatalog
}

// TaskExecution 保存一次面向管理员可见的后台任务执行摘要。
// 详细过程写入独立任务日志，不在该表中存储。
type TaskExecution struct {
	Base
	System     string     `gorm:"size:16;not null;default:'';index" json:"system"`
	Kind       string     `gorm:"size:32;not null;index" json:"kind"`
	Trigger    string     `gorm:"size:16;not null;index" json:"trigger"`
	Name       string     `gorm:"size:255;not null" json:"name"`
	Status     string     `gorm:"size:16;not null;index" json:"status"`
	Stage      string     `gorm:"size:64" json:"stage,omitempty"`
	SourcePath string     `gorm:"size:2048" json:"source_path,omitempty"`
	DestPath   string     `gorm:"size:2048" json:"dest_path,omitempty"`
	Message    string     `gorm:"type:text" json:"message,omitempty"`
	Error      string     `gorm:"type:text" json:"error,omitempty"`
	Metrics    string     `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	StartedAt  time.Time  `gorm:"not null;index" json:"started_at"`
	FinishedAt *time.Time `gorm:"index" json:"finished_at,omitempty"`
}
