package model

import "time"

// HongGuoDiscovery 保存分类摘要；没有对应正式作品且不在失败冷却中的记录即为待补齐项。
// 不作为可绑定媒体资料，避免列表集数和状态被误当作详情证据。
type HongGuoDiscovery struct {
	SourceID       string    `gorm:"primaryKey;size:32" json:"source_id"`
	SourceCategory string    `gorm:"size:32;not null;default:'';index" json:"source_category"`
	Title          string    `gorm:"type:text" json:"title"`
	Overview       string    `gorm:"type:text" json:"overview"`
	CoverURL       string    `gorm:"type:text" json:"-"`
	EpisodeCount   int       `json:"episode_count"`
	UpdateText     string    `gorm:"type:text" json:"update_text"`
	CreatedAt      time.Time `gorm:"index" json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (HongGuoDiscovery) TableName() string { return "hongguo_discoveries" }

// HongGuoRankEntry 保存官网榜单中的作品名次；作品可同时属于总榜和类型榜。
type HongGuoRankEntry struct {
	RankKey   string    `gorm:"primaryKey;size:32" json:"rank_key"`
	SourceID  string    `gorm:"primaryKey;size:32;index" json:"source_id"`
	Position  int       `gorm:"not null" json:"position"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (HongGuoRankEntry) TableName() string { return "hongguo_rank_entries" }

// HongGuoWork 是红果源作品，独立于现有 metadata_items；SourceID 不随人工聚合变化。
type HongGuoWork struct {
	PermanentBase
	SourceID           string     `gorm:"size:32;not null;uniqueIndex" json:"source_id"`
	SourceCategory     string     `gorm:"size:32;not null;default:'';index" json:"source_category"`
	Kind               string     `gorm:"size:16;not null;index;check:chk_hongguo_work_kind,kind IN ('movie','series')" json:"kind"`
	Title              string     `gorm:"type:text;not null" json:"title"`
	Overview           string     `gorm:"type:text" json:"overview"`
	Tags               string     `gorm:"type:text;not null;default:'[]'" json:"-"`
	EpisodeCount       int        `gorm:"not null;default:0" json:"episode_count"`
	TotalEpisodes      int        `gorm:"not null;default:0" json:"total_episodes"`
	AccessibleEpisodes int        `gorm:"not null;default:0" json:"accessible_episodes"`
	UpdateText         string     `gorm:"type:text" json:"update_text"`
	SourceStatus       string     `gorm:"type:text" json:"source_status"`
	Completed          bool       `gorm:"not null;default:false" json:"completed"`
	FirstVisibleAt     *time.Time `json:"first_visible_at,omitempty"`
	Rating             float32    `json:"rating"`
	RatingCount        int64      `json:"rating_count"`
	RefreshedAt        time.Time  `gorm:"not null;index" json:"refreshed_at"`
}

func (HongGuoWork) TableName() string { return "hongguo_works" }

// HongGuoEpisode 以源作品和集号保持身份，SourceVideoID 只作内部溯源，不用于播放。
type HongGuoEpisode struct {
	PermanentBase
	WorkID        string      `gorm:"size:36;not null;uniqueIndex:uidx_hongguo_episode,priority:1" json:"work_id"`
	Number        int         `gorm:"not null;uniqueIndex:uidx_hongguo_episode,priority:2;check:chk_hongguo_episode_number,number > 0" json:"number"`
	SourceVideoID string      `gorm:"size:32" json:"-"`
	Work          HongGuoWork `gorm:"foreignKey:WorkID;constraint:OnDelete:RESTRICT" json:"-"`
}

func (HongGuoEpisode) TableName() string { return "hongguo_episodes" }

// HongGuoPerson 按来源人物 ID 去重，不与现有人物表按名字合并。
type HongGuoPerson struct {
	PermanentBase
	SourceID string `gorm:"size:32;not null;uniqueIndex" json:"source_id"`
	Name     string `gorm:"type:text;not null" json:"name"`
}

func (HongGuoPerson) TableName() string { return "hongguo_people" }

// HongGuoCredit 保存作品级人物关系；Subtitle 保留原文，不假定它一定是饰演角色。
type HongGuoCredit struct {
	PermanentBase
	WorkID    string        `gorm:"size:36;not null;uniqueIndex:uidx_hongguo_credit,priority:1" json:"work_id"`
	PersonID  string        `gorm:"size:36;not null;uniqueIndex:uidx_hongguo_credit,priority:2" json:"person_id"`
	Subtitle  string        `gorm:"type:text" json:"subtitle"`
	SortOrder int           `json:"sort_order"`
	Work      HongGuoWork   `gorm:"foreignKey:WorkID;constraint:OnDelete:CASCADE" json:"-"`
	Person    HongGuoPerson `gorm:"foreignKey:PersonID;constraint:OnDelete:RESTRICT" json:"person"`
}

func (HongGuoCredit) TableName() string { return "hongguo_credits" }

// HongGuoSnapshot 保存最近一次成功解析的资料白名单，不作为同步进度或公共响应。
type HongGuoSnapshot struct {
	WorkID    string      `gorm:"primaryKey;size:36" json:"-"`
	Payload   string      `gorm:"type:jsonb;not null" json:"-"`
	FetchedAt time.Time   `gorm:"not null" json:"-"`
	Work      HongGuoWork `gorm:"foreignKey:WorkID;constraint:OnDelete:CASCADE" json:"-"`
}

func (HongGuoSnapshot) TableName() string { return "hongguo_snapshots" }

// HongGuoArtwork 独立保存发现/作品海报或人物头像及其下载待办，签名地址不向客户端公开。
type HongGuoArtwork struct {
	PermanentBase
	SourceID      *string        `gorm:"size:32;uniqueIndex:uidx_hongguo_artwork_source" json:"-"`
	WorkID        *string        `gorm:"size:36;uniqueIndex" json:"work_id,omitempty"`
	PersonID      *string        `gorm:"size:36;uniqueIndex" json:"person_id,omitempty"`
	SourceURL     string         `gorm:"type:text;not null" json:"-"`
	LocalKey      string         `gorm:"type:text" json:"-"`
	NextAttemptAt *time.Time     `gorm:"index" json:"-"`
	Attempts      int            `gorm:"not null;default:0" json:"-"`
	Work          *HongGuoWork   `gorm:"foreignKey:WorkID;constraint:OnDelete:CASCADE" json:"-"`
	Person        *HongGuoPerson `gorm:"foreignKey:PersonID;constraint:OnDelete:CASCADE" json:"-"`
}

func (HongGuoArtwork) TableName() string { return "hongguo_artworks" }

// HongGuoSyncState 持久化各分类的下一页与增量扫描边界；执行日志不承载业务游标。
type HongGuoSyncState struct {
	Category  string    `gorm:"primaryKey;size:32" json:"category"`
	NextPage  int       `gorm:"not null;default:1" json:"next_page"`
	AfterID   string    `gorm:"size:36" json:"after_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (HongGuoSyncState) TableName() string { return "hongguo_sync_states" }

// HongGuoSyncFailure 保留尚未成功导入的源 ID，分类游标前进也不会丢失失败作品。
type HongGuoSyncFailure struct {
	SourceID  string    `gorm:"primaryKey;size:32" json:"source_id"`
	Attempts  int       `gorm:"not null;default:0" json:"attempts"`
	RetryAt   time.Time `gorm:"not null;index" json:"retry_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (HongGuoSyncFailure) TableName() string { return "hongguo_sync_failures" }

// HongGuoGroup 是人工整剧聚合，仅改变展示层级，不改变源作品及分集身份。
type HongGuoGroup struct {
	PermanentBase
	Title string `gorm:"type:text;not null" json:"title"`
}

func (HongGuoGroup) TableName() string { return "hongguo_groups" }

// HongGuoGroupMember 规定聚合中的季号，每个源作品最多属于一个聚合。
type HongGuoGroupMember struct {
	WorkID       string       `gorm:"primaryKey;size:36" json:"work_id"`
	GroupID      string       `gorm:"size:36;not null;uniqueIndex:uidx_hongguo_group_season,priority:1" json:"group_id"`
	SeasonNumber int          `gorm:"not null;uniqueIndex:uidx_hongguo_group_season,priority:2;check:chk_hongguo_group_season,season_number > 0" json:"season_number"`
	Group        HongGuoGroup `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"-"`
	Work         HongGuoWork  `gorm:"foreignKey:WorkID;constraint:OnDelete:RESTRICT" json:"-"`
}

func (HongGuoGroupMember) TableName() string { return "hongguo_group_members" }

// HongGuoMediaBinding 连接公共物理文件与红果资料，不向旧 metadata_items 写入替身记录。
type HongGuoMediaBinding struct {
	MediaID   string          `gorm:"primaryKey;size:36" json:"media_id"`
	WorkID    string          `gorm:"size:36;not null;index" json:"work_id"`
	EpisodeID *string         `gorm:"size:36;index" json:"episode_id,omitempty"`
	Media     Media           `gorm:"foreignKey:MediaID;constraint:OnDelete:CASCADE" json:"-"`
	Work      HongGuoWork     `gorm:"foreignKey:WorkID;constraint:OnDelete:RESTRICT" json:"-"`
	Episode   *HongGuoEpisode `gorm:"foreignKey:EpisodeID;constraint:OnDelete:RESTRICT" json:"-"`
}

func (HongGuoMediaBinding) TableName() string { return "hongguo_media_bindings" }

// HongGuoModels 集中声明来源拥有的表，不包含公共文件和用户状态的生命周期。
func HongGuoModels() []interface{} {
	return []interface{}{&HongGuoDiscovery{}, &HongGuoRankEntry{}, &HongGuoWork{}, &HongGuoEpisode{}, &HongGuoPerson{}, &HongGuoCredit{}, &HongGuoSnapshot{}, &HongGuoArtwork{}, &HongGuoSyncState{}, &HongGuoSyncFailure{}, &HongGuoGroup{}, &HongGuoGroupMember{}, &HongGuoMediaBinding{}}
}
