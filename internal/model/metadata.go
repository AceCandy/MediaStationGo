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

	CatalogJobStatusPending   = "pending"
	CatalogJobStatusRunning   = "running"
	CatalogJobStatusRetry     = "retry"
	CatalogJobStatusCompleted = "completed"
	CatalogJobStatusFailed    = "failed"

	CatalogJobStageRoot    = "root"
	CatalogJobStageSeasons = "seasons"
)

// MetadataItem 保存可由多个媒体文件共享的作品、剧集或单集元数据。
type MetadataItem struct {
	PermanentBase
	Kind         string        `gorm:"size:16;not null;index;check:chk_metadata_identity_season_zero,(kind = 'season' AND parent_id IS NOT NULL AND parent_id <> '' AND season_num >= 0 AND episode_num = 0) OR (kind = 'episode' AND parent_id IS NOT NULL AND parent_id <> '' AND season_num = 0 AND episode_num > 0) OR (kind IN ('movie','series') AND parent_id IS NULL AND season_num = 0 AND episode_num = 0)" json:"kind"`
	ParentID     *string       `gorm:"size:36;index;uniqueIndex:uidx_metadata_season,priority:1,where:kind = 'season';uniqueIndex:uidx_metadata_episode,priority:1,where:kind = 'episode'" json:"parent_id,omitempty"`
	SeasonNum    int           `gorm:"uniqueIndex:uidx_metadata_season,priority:2,where:kind = 'season'" json:"season_num"`
	EpisodeNum   int           `gorm:"uniqueIndex:uidx_metadata_episode,priority:2,where:kind = 'episode'" json:"episode_num"`
	Title        string        `gorm:"size:255;not null" json:"title"`
	OriginalName string        `gorm:"size:255" json:"original_name,omitempty"`
	Overview     string        `gorm:"type:text" json:"overview,omitempty"`
	Rating       float32       `json:"rating"`
	RuntimeSec   int           `json:"runtime_sec"`
	Year         int           `json:"year"`
	ReleaseDate  string        `gorm:"size:10;index" json:"release_date,omitempty"`
	Languages    string        `gorm:"size:64" json:"languages,omitempty"`
	Countries    string        `gorm:"size:128" json:"countries,omitempty"`
	Genres       string        `gorm:"type:text" json:"genres,omitempty"`
	NSFW         bool          `gorm:"default:false" json:"nsfw"`
	Source       string        `gorm:"size:32;not null" json:"source"`
	Parent       *MetadataItem `gorm:"foreignKey:ParentID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`

	// CatalogMetadataHydratedAt 表示本实体的 provider 数据、标识和人物已完成入库。
	CatalogMetadataHydratedAt *time.Time `gorm:"index" json:"catalog_metadata_hydrated_at,omitempty"`
	// PeopleHydratedAt 表示人物关系已从来源同步，包括来源明确返回空结果。
	PeopleHydratedAt *time.Time `gorm:"index" json:"people_hydrated_at,omitempty"`
	// CatalogArtworkHydratedAt 表示本实体要求的图片均已入库或 provider 明确未提供。
	CatalogArtworkHydratedAt *time.Time `gorm:"index" json:"catalog_artwork_hydrated_at,omitempty"`
	// CatalogHydratedAt 表示本实体及其全部目录子项均已完成入库。
	CatalogHydratedAt *time.Time `gorm:"index" json:"catalog_hydrated_at,omitempty"`
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
}

// ArtworkAsset 描述 DataDir 中按内容哈希保存的一份权威原图。
type ArtworkAsset struct {
	PermanentBase
	SHA256     string `gorm:"size:64;not null;uniqueIndex" json:"sha256"`
	StorageKey string `gorm:"size:255;not null;uniqueIndex" json:"storage_key"`
	MimeType   string `gorm:"size:64;not null" json:"mime_type"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SizeBytes  int64  `json:"size_bytes"`
}

// MetadataArtwork 将共享元数据关联到当前选中的一张权威图片。
type MetadataArtwork struct {
	PermanentBase
	MetadataID     string `gorm:"size:36;not null;index;uniqueIndex:uidx_metadata_artwork,priority:1" json:"metadata_id"`
	AssetID        string `gorm:"size:36;not null;index" json:"asset_id"`
	ArtworkType    string `gorm:"size:16;not null;uniqueIndex:uidx_metadata_artwork,priority:2" json:"artwork_type"`
	SourceProvider string `gorm:"size:32" json:"source_provider,omitempty"`
	SourceURL      string `gorm:"size:2048" json:"source_url,omitempty"`
}

// MetadataArtworkRecheck 记录 TMDb 最近一次明确未提供某类图片的时间。
type MetadataArtworkRecheck struct {
	PermanentBase
	MetadataID    string       `gorm:"size:36;not null;index;uniqueIndex:uidx_metadata_artwork_recheck,priority:1" json:"metadata_id"`
	ArtworkType   string       `gorm:"size:16;not null;uniqueIndex:uidx_metadata_artwork_recheck,priority:2" json:"artwork_type"`
	LastNoImageAt time.Time    `gorm:"not null;index" json:"last_no_image_at"`
	Metadata      MetadataItem `gorm:"foreignKey:MetadataID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

// MetadataArtworkCandidate 保存已本地化但尚未成为当前选择的 provider 图片。
type MetadataArtworkCandidate struct {
	PermanentBase
	MetadataID     string `gorm:"size:36;not null;index;uniqueIndex:uidx_metadata_artwork_candidate,priority:1" json:"metadata_id"`
	AssetID        string `gorm:"size:36;not null;index" json:"asset_id"`
	ArtworkType    string `gorm:"size:16;not null;uniqueIndex:uidx_metadata_artwork_candidate,priority:2" json:"artwork_type"`
	SourceProvider string `gorm:"size:32;not null;uniqueIndex:uidx_metadata_artwork_candidate,priority:3" json:"source_provider"`
	SourceURL      string `gorm:"size:2048" json:"source_url,omitempty"`
	// RepairCheckedURL 记录已得到可接受终态的豆瓣官方大图 URL。
	RepairCheckedURL string       `gorm:"type:text;not null;default:''" json:"-"`
	Metadata         MetadataItem `gorm:"foreignKey:MetadataID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	Asset            ArtworkAsset `gorm:"foreignKey:AssetID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

// MetadataProviderSnapshot 保存 provider 原始实体响应及其质量状态，避免未投影字段丢失。
type MetadataProviderSnapshot struct {
	PermanentBase
	MetadataID string       `gorm:"size:36;not null;uniqueIndex:uidx_metadata_provider_snapshot,priority:1" json:"metadata_id"`
	Provider   string       `gorm:"size:32;not null;uniqueIndex:uidx_metadata_provider_snapshot,priority:2" json:"provider"`
	Payload    string       `gorm:"type:jsonb;not null" json:"payload"`
	Degraded   bool         `gorm:"not null;default:false" json:"degraded"`
	FetchedAt  time.Time    `gorm:"not null;index" json:"fetched_at"`
	Metadata   MetadataItem `gorm:"foreignKey:MetadataID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

// CatalogHydrationJob 是发现页目录抓取的持久化调度状态。
type CatalogHydrationJob struct {
	PermanentBase
	Provider      string        `gorm:"size:32;not null;uniqueIndex:uidx_catalog_hydration_job,priority:1" json:"provider"`
	EntityKind    string        `gorm:"size:16;not null;uniqueIndex:uidx_catalog_hydration_job,priority:2" json:"entity_kind"`
	ExternalID    string        `gorm:"size:128;not null;uniqueIndex:uidx_catalog_hydration_job,priority:3" json:"external_id"`
	MetadataID    *string       `gorm:"size:36;index" json:"metadata_id,omitempty"`
	Status        string        `gorm:"size:16;not null;index" json:"status"`
	Stage         string        `gorm:"size:16;not null;index" json:"stage"`
	Attempts      int           `gorm:"not null;default:0" json:"attempts"`
	NextAttemptAt *time.Time    `gorm:"index" json:"next_attempt_at,omitempty"`
	LastError     string        `gorm:"type:text" json:"last_error,omitempty"`
	StartedAt     *time.Time    `json:"started_at,omitempty"`
	CompletedAt   *time.Time    `json:"completed_at,omitempty"`
	Metadata      *MetadataItem `gorm:"foreignKey:MetadataID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"-"`
}
