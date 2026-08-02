package model

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
	Base
	Kind         string        `gorm:"size:16;not null;index;check:chk_metadata_identity,(kind = 'season' AND parent_id IS NOT NULL AND parent_id <> '' AND season_num >= 0 AND episode_num = 0) OR (kind = 'episode' AND parent_id IS NOT NULL AND parent_id <> '' AND season_num = 0 AND episode_num > 0) OR (kind IN ('movie','series') AND parent_id IS NULL AND season_num = 0 AND episode_num = 0)" json:"kind"`
	ParentID     *string       `gorm:"size:36;index;uniqueIndex:uidx_metadata_season,priority:1,where:kind = 'season' AND deleted_at IS NULL;uniqueIndex:uidx_metadata_episode,priority:1,where:kind = 'episode' AND deleted_at IS NULL" json:"parent_id,omitempty"`
	SeasonNum    int           `gorm:"uniqueIndex:uidx_metadata_season,priority:2,where:kind = 'season' AND deleted_at IS NULL" json:"season_num"`
	EpisodeNum   int           `gorm:"uniqueIndex:uidx_metadata_episode,priority:2,where:kind = 'episode' AND deleted_at IS NULL" json:"episode_num"`
	Title        string        `gorm:"size:255;not null" json:"title"`
	OriginalName string        `gorm:"size:255" json:"original_name,omitempty"`
	EpisodeTitle string        `gorm:"size:255" json:"episode_title,omitempty"`
	Overview     string        `gorm:"type:text" json:"overview,omitempty"`
	Rating       float32       `json:"rating"`
	Year         int           `json:"year"`
	ReleaseDate  string        `gorm:"size:10;index" json:"release_date,omitempty"`
	Languages    string        `gorm:"size:64" json:"languages,omitempty"`
	Countries    string        `gorm:"size:128" json:"countries,omitempty"`
	Genres       string        `gorm:"type:text" json:"genres,omitempty"`
	NSFW         bool          `gorm:"default:false" json:"nsfw"`
	Source       string        `gorm:"size:32;not null" json:"source"`
	Parent       *MetadataItem `gorm:"foreignKey:ParentID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
}

// MetadataIdentifier 保存 provider 外部标识；同一数字在不同 provider 或实体类型下互不冲突。
type MetadataIdentifier struct {
	Base
	MetadataID string `gorm:"size:36;not null;index" json:"metadata_id"`
	Provider   string `gorm:"size:32;not null;uniqueIndex:uidx_metadata_identifier,priority:1" json:"provider"`
	EntityKind string `gorm:"size:16;not null;uniqueIndex:uidx_metadata_identifier,priority:2" json:"entity_kind"`
	ExternalID string `gorm:"size:128;not null;uniqueIndex:uidx_metadata_identifier,priority:3" json:"external_id"`
}

// ArtworkAsset 描述 DataDir 中按内容哈希保存的一份权威原图。
type ArtworkAsset struct {
	Base
	SHA256     string `gorm:"size:64;not null;uniqueIndex" json:"sha256"`
	StorageKey string `gorm:"size:255;not null;uniqueIndex" json:"storage_key"`
	MimeType   string `gorm:"size:64;not null" json:"mime_type"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SizeBytes  int64  `json:"size_bytes"`
}

// MetadataArtwork 将共享元数据关联到当前选中的一张权威图片。
type MetadataArtwork struct {
	Base
	MetadataID     string `gorm:"size:36;not null;index;uniqueIndex:uidx_metadata_artwork,priority:1" json:"metadata_id"`
	AssetID        string `gorm:"size:36;not null;index" json:"asset_id"`
	ArtworkType    string `gorm:"size:16;not null;uniqueIndex:uidx_metadata_artwork,priority:2" json:"artwork_type"`
	SourceProvider string `gorm:"size:32" json:"source_provider,omitempty"`
	SourceURL      string `gorm:"size:2048" json:"source_url,omitempty"`
}
