package model

import "time"

const (
	MetadataKindMovie   = "movie"
	MetadataKindSeries  = "series"
	MetadataKindSeason  = "season"
	MetadataKindEpisode = "episode"

	ArtworkTypePoster   = "poster"
	ArtworkTypeBackdrop = "backdrop"
	ArtworkTypeStill    = "still"
)

// MetadataItem 保存可由多个媒体文件共享的作品、剧集或单集元数据。
type MetadataItem struct {
	PermanentBase
	// LatestMediaAddedAt 是自身及后代现存文件的最新入库时间，由数据库维护。
	LatestMediaAddedAt *time.Time `gorm:"->" json:"latest_media_added_at,omitempty"`
	// LibraryIDs 是自身及后代现存文件的库集合；NULL 表示历史数据尚未初始化。
	LibraryIDs *string `gorm:"type:jsonb;->" json:"-"`

	Kind       string  `gorm:"size:16;not null;index;check:chk_metadata_identity_season_zero,(kind = 'season' AND parent_id IS NOT NULL AND parent_id <> '' AND season_num >= 0 AND episode_num = 0) OR (kind = 'episode' AND parent_id IS NOT NULL AND parent_id <> '' AND season_num = 0 AND episode_num > 0) OR (kind IN ('movie','series') AND parent_id IS NULL AND season_num = 0 AND episode_num = 0)" json:"kind"`
	ParentID   *string `gorm:"size:36;index;uniqueIndex:uidx_metadata_season,priority:1,where:kind = 'season';uniqueIndex:uidx_metadata_episode,priority:1,where:kind = 'episode'" json:"parent_id,omitempty"`
	SeasonNum  int     `gorm:"uniqueIndex:uidx_metadata_season,priority:2,where:kind = 'season'" json:"season_num"`
	EpisodeNum int     `gorm:"uniqueIndex:uidx_metadata_episode,priority:2,where:kind = 'episode'" json:"episode_num"`
	// 展示字段仅由 metadata_presentations 读取，不在身份表存储。
	Title        string        `gorm:"->;-:migration" json:"title"`
	OriginalName string        `gorm:"->;-:migration" json:"original_name,omitempty"`
	Overview     string        `gorm:"->;-:migration" json:"overview,omitempty"`
	Rating       float32       `gorm:"->;-:migration" json:"rating"`
	RuntimeSec   int           `gorm:"->;-:migration" json:"runtime_sec"`
	Year         int           `gorm:"->;-:migration" json:"year"`
	ReleaseDate  string        `gorm:"->;-:migration" json:"release_date,omitempty"`
	Languages    string        `gorm:"->;-:migration" json:"languages,omitempty"`
	Countries    string        `gorm:"->;-:migration" json:"countries,omitempty"`
	Genres       string        `gorm:"->;-:migration" json:"genres,omitempty"`
	Source       string        `gorm:"size:32;not null" json:"source"`
	Parent       *MetadataItem `gorm:"foreignKey:ParentID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`

	// TMDbEpisodeCheckedAt 表示集信息最近一次由 TMDb 完整复查成功的时间。
	TMDbEpisodeCheckedAt *time.Time `gorm:"column:tmdb_episode_checked_at;index" json:"tmdb_episode_checked_at,omitempty"`
	// TMDbSeasonCheckedAt 表示季信息最近一次由 TMDb 完整复查成功的时间。
	TMDbSeasonCheckedAt *time.Time `gorm:"column:tmdb_season_checked_at;index" json:"tmdb_season_checked_at,omitempty"`
}

// MetadataIdentifier 保存 provider 外部标识；同一数字在不同 provider 或实体类型下互不冲突。
type MetadataIdentifier struct {
	PermanentBase
	MetadataID string `gorm:"size:36;not null;index;index:idx_metadata_identifier_provider_kind_metadata,priority:3" json:"metadata_id"`
	Provider   string `gorm:"size:32;not null;uniqueIndex:uidx_metadata_identifier,priority:1;index:idx_metadata_identifier_provider_kind_metadata,priority:1" json:"provider"`
	EntityKind string `gorm:"size:16;not null;uniqueIndex:uidx_metadata_identifier,priority:2;index:idx_metadata_identifier_provider_kind_metadata,priority:2" json:"entity_kind"`
	ExternalID string `gorm:"size:128;not null;uniqueIndex:uidx_metadata_identifier,priority:3" json:"external_id"`
	// DoubanEntityKind 仅记录人工确认的豆瓣实际类型；为空时仍按本地类型补齐。
	DoubanEntityKind string `gorm:"size:16;not null;default:''" json:"-"`
}

// ArtworkAsset 描述 DataDir 中按内容哈希保存的一份权威原图。
type ArtworkAsset struct {
	PermanentBase
	SHA256     string `gorm:"size:64;not null" json:"sha256"`
	StorageKey string `gorm:"size:255;not null;uniqueIndex" json:"storage_key"`
	MimeType   string `gorm:"size:64;not null" json:"mime_type"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SizeBytes  int64  `json:"size_bytes"`
}

// MetadataArtwork 是当前来源图片的只读关联，不对应持久表。
type MetadataArtwork struct {
	PermanentBase
	MetadataID     string `json:"metadata_id"`
	AssetID        string `json:"asset_id"`
	ArtworkType    string `json:"artwork_type"`
	SourceProvider string `json:"source_provider,omitempty"`
	SourceURL      string `json:"source_url,omitempty"`
}

func (MetadataArtwork) TableName() string { return "metadata_artwork_presentations" }

// ProviderData 是资料中心按本地ID关联的读取结果，不对应独立数据表。
type ProviderData struct {
	ID         string       `json:"id"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	MetadataID string       `json:"metadata_id"`
	Provider   string       `json:"provider"`
	Payload    string       `json:"payload"`
	Degraded   bool         `json:"degraded"`
	FetchedAt  time.Time    `json:"fetched_at"`
	Metadata   MetadataItem `gorm:"foreignKey:MetadataID;references:ID" json:"-"`
}
