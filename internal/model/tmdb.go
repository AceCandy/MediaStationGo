package model

import "time"

// TMDbSubject 是独立于本地媒体的来源身份；季和集保留已验证的父链。
type TMDbSubject struct {
	PermanentBase
	// 演职员补全独立退避，不影响正文和目录维护。
	NextPeopleAt *time.Time `gorm:"index" json:"-"`
	// 补缺与目录检查分别计时，完整基础资料不参加周期补缺。
	NextMaintenanceAt *time.Time   `gorm:"index" json:"-"`
	NextInventoryAt   *time.Time   `gorm:"index" json:"-"`
	Kind              string       `gorm:"size:16;not null;uniqueIndex:uidx_tmdb_subject_identity,priority:1;index" json:"kind"`
	TMDbID            int          `gorm:"column:tmdb_id;not null;check:tmdb_id > 0;uniqueIndex:uidx_tmdb_subject_identity,priority:2" json:"tmdb_id"`
	ParentID          *string      `gorm:"size:36;index;uniqueIndex:uidx_tmdb_subject_coordinate,priority:1,where:parent_id IS NOT NULL" json:"parent_id,omitempty"`
	Parent            *TMDbSubject `gorm:"foreignKey:ParentID;constraint:OnDelete:RESTRICT" json:"-"`
	Number            *int         `gorm:"uniqueIndex:uidx_tmdb_subject_coordinate,priority:2,where:parent_id IS NOT NULL" json:"number,omitempty"`
	Title             string       `gorm:"type:text" json:"title"`
	OriginalName      string       `gorm:"type:text" json:"original_name"`
	Overview          string       `gorm:"type:text" json:"overview"`
	ReleaseDate       string       `gorm:"size:32" json:"release_date"`
	Rating            *float64     `json:"rating"`
	VoteCount         int          `json:"vote_count"`
	Status            string       `gorm:"size:64" json:"status"`
	Quality           int          `gorm:"not null;default:0" json:"quality"`
	Fields            string       `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	FetchedAt         *time.Time   `json:"fetched_at,omitempty"`
}

func (TMDbSubject) TableName() string { return "tmdb_subjects" }

// TMDbPersonProfile 保存来源字段，不覆盖公共人物的人工资料。
type TMDbPersonProfile struct {
	PermanentBase
	// 人物详情补全独立退避，来源空简介也按有效响应结算。
	NextPeopleAt    *time.Time `gorm:"index" json:"-"`
	TMDbID          int        `gorm:"column:tmdb_id;not null;uniqueIndex;check:tmdb_id > 0" json:"tmdb_id"`
	PersonID        string     `gorm:"size:36;not null;uniqueIndex" json:"person_id"`
	Person          Person     `gorm:"foreignKey:PersonID;constraint:OnDelete:RESTRICT" json:"-"`
	Name            string     `gorm:"type:text" json:"name"`
	OriginalName    string     `gorm:"type:text" json:"original_name"`
	Biography       string     `gorm:"type:text" json:"biography"`
	Department      string     `gorm:"type:text" json:"department"`
	Fields          string     `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	Quality         int        `gorm:"not null;default:1" json:"quality"`
	FetchedAt       *time.Time `json:"fetched_at,omitempty"`
	ProfilePath     string     `gorm:"type:text" json:"-"`
	ImageKey        string     `gorm:"type:text" json:"-"`
	ImageSourcePath string     `gorm:"type:text" json:"-"`
	ImageStatus     string     `gorm:"size:16" json:"image_status"`
	ImageCheckedAt  *time.Time `gorm:"index" json:"-"`
	ImageRetryAt    *time.Time `json:"-"`
}

func (TMDbPersonProfile) TableName() string { return "tmdb_person_profiles" }

// TMDbPayload 每个来源组件与语言保留最新有效响应，时间来自实际获取。
type TMDbPayload struct {
	PermanentBase
	SubjectID       *string            `gorm:"size:36;uniqueIndex:uidx_tmdb_subject_payload,priority:1,where:subject_id IS NOT NULL;check:tmdb_payload_owner,(subject_id IS NULL) <> (person_profile_id IS NULL)" json:"subject_id,omitempty"`
	Subject         *TMDbSubject       `gorm:"foreignKey:SubjectID;constraint:OnDelete:RESTRICT" json:"-"`
	PersonProfileID *string            `gorm:"size:36;uniqueIndex:uidx_tmdb_person_payload,priority:1,where:person_profile_id IS NOT NULL" json:"person_profile_id,omitempty"`
	PersonProfile   *TMDbPersonProfile `gorm:"foreignKey:PersonProfileID;constraint:OnDelete:RESTRICT" json:"-"`
	Component       string             `gorm:"size:32;not null;uniqueIndex:uidx_tmdb_subject_payload,priority:2,where:subject_id IS NOT NULL;uniqueIndex:uidx_tmdb_person_payload,priority:2,where:person_profile_id IS NOT NULL" json:"component"`
	Language        string             `gorm:"size:16;not null;uniqueIndex:uidx_tmdb_subject_payload,priority:3,where:subject_id IS NOT NULL;uniqueIndex:uidx_tmdb_person_payload,priority:3,where:person_profile_id IS NOT NULL" json:"language"`
	Body            string             `gorm:"type:jsonb;not null" json:"-"`
	Origin          string             `gorm:"size:24;not null" json:"origin"`
	Quality         int                `json:"quality"`
	FetchedAt       time.Time          `gorm:"not null" json:"fetched_at"`
}

func (TMDbPayload) TableName() string { return "tmdb_payloads" }

// TMDbCredit 区分作品演职员与人物履历证据，保留多个角色和全部职务。
type TMDbCredit struct {
	PermanentBase
	SubjectID       string            `gorm:"size:36;not null;index;uniqueIndex:uidx_tmdb_credit,priority:1" json:"subject_id"`
	Subject         TMDbSubject       `gorm:"foreignKey:SubjectID;constraint:OnDelete:RESTRICT" json:"-"`
	PersonProfileID string            `gorm:"size:36;not null;index;uniqueIndex:uidx_tmdb_credit,priority:2" json:"person_profile_id"`
	PersonProfile   TMDbPersonProfile `gorm:"foreignKey:PersonProfileID;constraint:OnDelete:RESTRICT" json:"person"`
	Origin          string            `gorm:"size:16;not null;uniqueIndex:uidx_tmdb_credit,priority:3" json:"origin"`
	CreditKey       string            `gorm:"size:64;not null;uniqueIndex:uidx_tmdb_credit,priority:4" json:"-"`
	CreditID        string            `gorm:"type:text" json:"credit_id"`
	Category        string            `gorm:"size:8;not null" json:"category"`
	Character       string            `gorm:"type:text" json:"character"`
	Department      string            `gorm:"type:text" json:"department"`
	Job             string            `gorm:"type:text" json:"job"`
	SortOrder       int               `json:"order"`
}

func (TMDbCredit) TableName() string { return "tmdb_credits" }

// TMDbArtwork 保存来源图片路径及本地资产，普通媒体直接读取此关联。
type TMDbArtwork struct {
	PermanentBase
	SubjectID      string        `gorm:"size:36;not null;uniqueIndex:uidx_tmdb_artwork,priority:1" json:"subject_id"`
	Subject        TMDbSubject   `gorm:"foreignKey:SubjectID;constraint:OnDelete:RESTRICT" json:"-"`
	Type           string        `gorm:"size:16;not null;uniqueIndex:uidx_tmdb_artwork,priority:2" json:"type"`
	SourcePath     string        `gorm:"type:text" json:"-"`
	DownloadedPath string        `gorm:"type:text" json:"-"`
	AssetID        *string       `gorm:"size:36;index" json:"-"`
	Asset          *ArtworkAsset `gorm:"foreignKey:AssetID;constraint:OnDelete:RESTRICT" json:"-"`
	Status         string        `gorm:"size:16;not null" json:"status"`
	CheckedAt      *time.Time    `gorm:"index" json:"-"`
	RetryAt        *time.Time    `json:"-"`
}

func (TMDbArtwork) TableName() string { return "tmdb_artworks" }

// TMDbQueryCache 保存规范化查询的原始有序结果，不包含用户媒体归属。
type TMDbQueryCache struct {
	PermanentBase
	Key       string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Body      string    `gorm:"type:jsonb;not null" json:"-"`
	FetchedAt time.Time `json:"fetched_at"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`
}

func (TMDbQueryCache) TableName() string { return "tmdb_query_cache" }

// TMDbRefreshJob 使用无凭据的请求身份；首次响应前季集可能尚无来源 ID。
type TMDbRefreshJob struct {
	PermanentBase
	Key             string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Path            string     `gorm:"type:text;not null" json:"-"`
	Query           string     `gorm:"type:text;not null" json:"-"`
	ConfigKey       string     `gorm:"size:64;index" json:"-"`
	SubjectID       *string    `gorm:"size:36;index" json:"subject_id,omitempty"`
	PersonProfileID *string    `gorm:"size:36;index" json:"person_profile_id,omitempty"`
	PayloadID       *string    `gorm:"size:36;index" json:"-"`
	Scope           string     `gorm:"size:32" json:"scope"`
	Language        string     `gorm:"size:16" json:"language"`
	Status          string     `gorm:"size:16;index" json:"status"`
	ExplicitPending bool       `gorm:"not null;default:false;index" json:"-"`
	RequestedBy     string     `gorm:"size:36;index" json:"-"`
	Token           string     `gorm:"size:36" json:"-"`
	LeaseUntil      *time.Time `gorm:"index" json:"-"`
	LastSuccessAt   *time.Time `json:"last_success_at,omitempty"`
	NextRefreshAt   *time.Time `gorm:"index" json:"next_refresh_at,omitempty"`
	NextAttemptAt   *time.Time `gorm:"index" json:"next_retry_at,omitempty"`
	CooldownUntil   *time.Time `gorm:"index" json:"-"`
	Attempts        int        `json:"attempts"`
	LastError       string     `gorm:"type:text" json:"error,omitempty"`
}

func (TMDbRefreshJob) TableName() string { return "tmdb_refresh_jobs" }
