package model

import "time"

const CatalogSourceNFO = "nfo"

// NFOItem 是本地目录或文件定义的条目，不以外部平台标识去重。
// LocalKey 仅在媒体库内保持扫描幂等；不同目录的同名作品相互独立。
type NFOItem struct {
	PermanentBase
	LibraryID  string   `gorm:"size:36;not null;uniqueIndex:uidx_nfo_local_item,priority:1" json:"library_id"`
	LocalKey   string   `gorm:"type:text;not null;uniqueIndex:uidx_nfo_local_item,priority:2" json:"-"`
	Kind       string   `gorm:"size:16;not null;check:chk_nfo_item_kind,kind IN ('movie','series','season','episode')" json:"kind"`
	ParentID   *string  `gorm:"size:36;index" json:"parent_id,omitempty"`
	Parent     *NFOItem `gorm:"foreignKey:ParentID;constraint:OnDelete:RESTRICT" json:"-"`
	SeasonNum  int      `json:"season_num"`
	EpisodeNum int      `json:"episode_num"`
	NFOFields  `gorm:"embedded"`
}

// NFOFields 保存本地资料快照，图片只保存受控的本地资产引用。
// 外部 ID 和人物是附带资料，不关联普通资料表或触发联网任务。
type NFOFields struct {
	Title           string  `gorm:"type:text;not null" json:"title"`
	OriginalName    string  `gorm:"type:text" json:"original_name,omitempty"`
	Overview        string  `gorm:"type:text" json:"overview,omitempty"`
	Year            int     `json:"year"`
	ReleaseDate     string  `gorm:"size:10" json:"release_date,omitempty"`
	Rating          float32 `json:"rating"`
	Genres          string  `gorm:"type:text" json:"genres,omitempty"`
	Countries       string  `gorm:"type:text" json:"countries,omitempty"`
	Languages       string  `gorm:"type:text" json:"languages,omitempty"`
	NSFW            bool    `json:"nsfw"`
	PosterAssetID   string  `gorm:"size:36" json:"-"`
	BackdropAssetID string  `gorm:"size:36" json:"-"`
	ExternalIDs     string  `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	People          string  `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
}

// NFOMediaBinding 保存每个文件的独立资料及版本关系，不向普通元数据写入替身。
type NFOMediaBinding struct {
	MediaID     string  `gorm:"primaryKey;size:36" json:"media_id"`
	ItemID      string  `gorm:"size:36;not null;index" json:"item_id"`
	Media       Media   `gorm:"foreignKey:MediaID;constraint:OnDelete:CASCADE" json:"-"`
	Item        NFOItem `gorm:"foreignKey:ItemID;constraint:OnDelete:RESTRICT" json:"-"`
	VersionName string  `gorm:"type:text" json:"version_name,omitempty"`
	Fingerprint string  `gorm:"size:64;not null" json:"-"`
	NFOFields   `gorm:"embedded"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NFOUserState 按本地逻辑条目记录收藏与播放状态；删除文件不删除用户状态。
type NFOUserState struct {
	UserID     string     `gorm:"primaryKey;size:36" json:"user_id"`
	ItemID     string     `gorm:"primaryKey;size:36" json:"item_id"`
	MediaID    string     `gorm:"size:36" json:"media_id"`
	Favorite   bool       `json:"favorite"`
	PositionMs int64      `json:"position_ms"`
	DurationMs int64      `json:"duration_ms"`
	Completed  bool       `json:"completed"`
	WatchedAt  *time.Time `json:"watched_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// NFOPlaybackEvent 保留本地条目的播放事实，独立于普通作品事件。
type NFOPlaybackEvent struct {
	PermanentBase
	UserID    string    `gorm:"size:36;not null;uniqueIndex:uidx_nfo_playback_event,priority:1"`
	SessionID string    `gorm:"size:128;not null;uniqueIndex:uidx_nfo_playback_event,priority:2"`
	ItemID    string    `gorm:"size:36;not null;uniqueIndex:uidx_nfo_playback_event,priority:3"`
	MediaID   string    `gorm:"size:36;not null"`
	LibraryID string    `gorm:"size:36;not null;index"`
	PlayedAt  time.Time `gorm:"not null;index"`
}

func (NFOItem) TableName() string          { return "nfo_items" }
func (NFOMediaBinding) TableName() string  { return "nfo_media_bindings" }
func (NFOUserState) TableName() string     { return "nfo_user_states" }
func (NFOPlaybackEvent) TableName() string { return "nfo_playback_events" }
