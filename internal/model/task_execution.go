package model

import (
	"strings"
	"time"
)

const (
	TaskSystemCommon     = "common"
	TaskSystemMedia      = "media"
	TaskSystemTMDb       = "tmdb"
	TaskSystemDouban     = "douban"
	TaskSystemHongGuo    = "hongguo"
	TaskSystemHuangGuoAI = "huangguoai"
)

// CommonTaskKinds 是不依赖具体媒体项的公共任务。
var CommonTaskKinds = []string{"cleanup"}

// TaskSystemForKind 决定新任务执行记录的归属体系。
func TaskSystemForKind(kind string) string {
	if strings.HasPrefix(kind, "huangguoai_") {
		return TaskSystemHuangGuoAI
	}
	if strings.HasPrefix(kind, "hongguo_") {
		return TaskSystemHongGuo
	}
	if strings.HasPrefix(kind, "tmdb_data_") {
		return TaskSystemTMDb
	}
	if strings.HasPrefix(kind, "douban_data_") {
		return TaskSystemDouban
	}
	for _, common := range CommonTaskKinds {
		if common == kind {
			return TaskSystemCommon
		}
	}
	return TaskSystemMedia
}

// TaskSystemForExecution 区分共用任务类型下的资料中心人物翻译，保留历史标识。
func TaskSystemForExecution(kind, name string) string {
	if kind == "people" && name == "人物翻译" {
		return TaskSystemTMDb
	}
	return TaskSystemForKind(kind)
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
