package model

import "time"

// TMDbRecheckJob 保存季集复查的到期状态；执行历史不是恢复检查点。
type TMDbRecheckJob struct {
	MetadataID       string     `gorm:"primaryKey;size:36" json:"metadata_id"`
	Status           string     `gorm:"size:16;not null;default:pending;index:idx_tmdb_recheck_status_due,priority:1" json:"status"`
	DueAt            *time.Time `gorm:"index:idx_tmdb_recheck_status_due,priority:2;index:idx_tmdb_recheck_due,where:due_at IS NOT NULL" json:"due_at"`
	Attempts         int        `gorm:"not null;default:0" json:"attempts"`
	LastError        string     `gorm:"type:text;not null;default:''" json:"last_error"`
	LeaseToken       string     `gorm:"size:36;not null;default:''" json:"-"`
	LeaseUntil       *time.Time `json:"-"`
	NotFoundIdentity string     `gorm:"type:text;not null;default:''" json:"-"`
}

// TMDbRecheckChange 由业务事务登记，游标分批展开整剧/季的后代。
type TMDbRecheckChange struct {
	MetadataID string `gorm:"primaryKey;size:36;index:idx_tmdb_recheck_changes_pending_id,where:pending"`
	Revision   int64  `gorm:"not null;default:1"`
	Pending    bool   `gorm:"not null;default:true"`
	Expand     bool   `gorm:"not null;default:false"`
	Cursor     string `gorm:"size:36;not null;default:''"`
}

// TMDbRecheckScan 保存初始化和低频文件核对进度。
type TMDbRecheckScan struct {
	ID     int       `gorm:"primaryKey;autoIncrement:false"`
	Cursor string    `gorm:"size:36;not null;default:''"`
	NextAt time.Time `gorm:"not null"`
}

// TMDbRecheckAssetChange 将共享图片资产的影响展开限制在后台批次内。
type TMDbRecheckAssetChange struct {
	AssetID string `gorm:"primaryKey;size:36"`
	Cursor  string `gorm:"size:36;not null;default:''"`
}
