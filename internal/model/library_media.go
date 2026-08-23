package model

const (
	LibraryTypeNFOMovie = "nfo_movie"
	LibraryTypeNFOTV    = "nfo_tv"
)

// Library 表示一个逻辑媒体库。Path 保留为兼容字段，指向第一条 LibraryRoot。
type Library struct {
	Base
	Name     string        `gorm:"size:128;not null" json:"name"`
	Path     string        `gorm:"size:1024;not null" json:"path"`
	Type     string        `gorm:"size:16;not null;default:movie" json:"type"` // movie / tv / anime / music / nfo_movie / nfo_tv
	CoverURL string        `gorm:"size:1024" json:"cover_url,omitempty"`
	Enabled  bool          `gorm:"default:true" json:"enabled"`
	Roots    []LibraryRoot `gorm:"foreignKey:LibraryID" json:"roots,omitempty"`
}

// LibraryRoot 是逻辑媒体库下的一条真实物理/挂载路径。
type LibraryRoot struct {
	Base
	LibraryID string `gorm:"index;size:36;not null" json:"library_id"`
	Name      string `gorm:"size:128" json:"name,omitempty"`
	Path      string `gorm:"size:1024;not null" json:"path"`
	Enabled   bool   `gorm:"default:true" json:"enabled"`
	SortOrder int    `gorm:"default:0" json:"sort_order"`
}

// Media 是单个可播放文件。展示元数据通过 MetadataID 关联共享表；扫描后、
// 刮削完成前允许暂时没有该关联。
// Title、Year、provider ID 和 SeriesID 仅是扫描/匹配提示，不是权威元数据。
type Media struct {
	Base
	LibraryID     string        `gorm:"index;size:36" json:"library_id"`
	LibraryRootID string        `gorm:"index;size:36" json:"library_root_id,omitempty"`
	MetadataID    string        `gorm:"index;size:36;default:null" json:"metadata_id"`
	Metadata      *MetadataItem `gorm:"foreignKey:MetadataID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	SeriesID      string        `gorm:"column:series_hint;index;size:128" json:"series_id,omitempty"`
	SeriesTitle   string        `gorm:"-" json:"series_title,omitempty"`
	Title         string        `gorm:"column:scan_title;size:255" json:"title"`
	OriginalName  string        `gorm:"-" json:"original_name,omitempty"`
	EpisodeTitle  string        `gorm:"-" json:"-"`
	Path          string        `gorm:"uniqueIndex;size:1024;not null" json:"path"`
	RelativePath  string        `gorm:"size:1024" json:"relative_path,omitempty"`
	// 以下技术字段仅承载 ffprobe 的扁平 API 投影，不映射到 media 主表。
	SizeBytes int64 `gorm:"-" json:"size_bytes"`
	// ScanFileSizeBytes 与 ScanFileMTimeNS 记录扫描路径自身的文件指纹。
	// 对 .strm，它们描述边车文本文件；目标媒体大小由 MediaProbeMetadata 保存。
	ScanFileSizeBytes int64   `gorm:"column:scan_file_size_bytes" json:"-"`
	ScanFileMTimeNS   int64   `gorm:"column:scan_file_mtime_ns" json:"-"`
	DurationSec       int     `gorm:"-" json:"duration_sec"`
	Width             int     `gorm:"-" json:"width"`
	Height            int     `gorm:"-" json:"height"`
	VideoCodec        string  `gorm:"-" json:"video_codec,omitempty"`
	AudioCodec        string  `gorm:"-" json:"audio_codec,omitempty"`
	Container         string  `gorm:"-" json:"container,omitempty"`
	PosterURL         string  `gorm:"-" json:"poster_url,omitempty"`
	BackdropURL       string  `gorm:"-" json:"backdrop_url,omitempty"`
	Overview          string  `gorm:"-" json:"overview,omitempty"`
	Rating            float32 `gorm:"-" json:"rating"`
	Year              int     `gorm:"column:scan_year" json:"year"`
	ReleaseDate       string  `gorm:"-" json:"release_date,omitempty"`
	SeasonNum         int     `json:"season_num"`
	EpisodeNum        int     `json:"episode_num"`
	ScrapeStatus      string  `gorm:"size:16;default:pending" json:"scrape_status"`
	ScrapeTrigger     string  `gorm:"size:16;default:event" json:"scrape_trigger,omitempty"`
	ScrapeError       string  `gorm:"size:1024" json:"scrape_error,omitempty"`
	LocalMetadataHint string  `gorm:"type:text" json:"-"`
	TMDbID            int     `gorm:"column:lookup_tmdb_id" json:"tmdb_id"`
	BangumiID         int     `gorm:"column:lookup_bangumi_id" json:"bangumi_id"`
	DoubanID          string  `gorm:"column:lookup_douban_id;size:32" json:"douban_id,omitempty"`
	TheTVDBID         string  `gorm:"column:lookup_thetvdb_id;size:64" json:"thetvdb_id,omitempty"`
	Languages         string  `gorm:"-" json:"languages,omitempty"`
	Countries         string  `gorm:"-" json:"countries,omitempty"`
	Genres            string  `gorm:"-" json:"genres,omitempty"`
	NSFW              bool    `gorm:"-" json:"nsfw"`

	// STRMURL is the indirection target for .strm files: when present the
	// stream handler redirects to it instead of opening the local file.
	// Only absolute HTTP/HTTPS targets are accepted by public entry points.
	STRMURL string `gorm:"size:2048" json:"strm_url,omitempty"`

	LibraryName string `gorm:"-" json:"library_name,omitempty"`
	LibraryPath string `gorm:"-" json:"library_path,omitempty"`

	DisplayLibraryID   string `gorm:"-" json:"display_library_id,omitempty"`
	DisplayLibraryName string `gorm:"-" json:"display_library_name,omitempty"`
	DisplayLibraryPath string `gorm:"-" json:"display_library_path,omitempty"`

	// FileHash is a sparse-sample MD5 used for duplicate detection.
	// Computed on-demand by the duplicate finder; format: "<hex>-<size>".
	FileHash string `gorm:"index;size:64" json:"file_hash,omitempty"`

	// FileID is a "device:inode" identity for the underlying file. Hardlinks
	// to the same data share a FileID, letting the scanner skip re-importing a
	// source file and its organized hardlink as two separate items.
	FileID string `gorm:"index;size:64" json:"file_id,omitempty"`

	// IsDuplicate flags this media as a duplicate of another media row.
	IsDuplicate bool   `gorm:"default:false" json:"is_duplicate"`
	DuplicateOf string `gorm:"size:128" json:"duplicate_of,omitempty"`

	// Tracks 仅在单媒体详情响应中附加，列表和搜索不会加载完整探测文档。
	Tracks []MediaTrack `gorm:"-" json:"tracks,omitempty"`
}

// MediaTrack 是面向详情页的安全轨道投影，不包含原始路径、URL 或任意探测标签。
type MediaTrack struct {
	Index             int     `json:"index"`
	Type              string  `json:"type"`
	Codec             string  `json:"codec,omitempty"`
	Profile           string  `json:"profile,omitempty"`
	Level             int     `json:"level,omitempty"`
	TimeBase          string  `json:"time_base,omitempty"`
	Language          string  `json:"language,omitempty"`
	DisplayLanguage   string  `json:"display_language,omitempty"`
	Title             string  `json:"title,omitempty"`
	DisplayTitle      string  `json:"display_title,omitempty"`
	BitRate           int64   `json:"bit_rate,omitempty"`
	IsDefault         bool    `json:"is_default"`
	IsForced          bool    `json:"is_forced"`
	IsHearingImpaired bool    `json:"is_hearing_impaired,omitempty"`
	IsVisualImpaired  bool    `json:"is_visual_impaired,omitempty"`
	Width             int     `json:"width,omitempty"`
	Height            int     `json:"height,omitempty"`
	AspectRatio       string  `json:"aspect_ratio,omitempty"`
	PixelFormat       string  `json:"pixel_format,omitempty"`
	BitDepth          int     `json:"bit_depth,omitempty"`
	ColorRange        string  `json:"color_range,omitempty"`
	ColorSpace        string  `json:"color_space,omitempty"`
	ColorTransfer     string  `json:"color_transfer,omitempty"`
	ColorPrimaries    string  `json:"color_primaries,omitempty"`
	VideoRange        string  `json:"video_range,omitempty"`
	AverageFrameRate  float64 `json:"average_frame_rate,omitempty"`
	RealFrameRate     float64 `json:"real_frame_rate,omitempty"`
	Channels          int     `json:"channels,omitempty"`
	SampleRate        int     `json:"sample_rate,omitempty"`
	ChannelLayout     string  `json:"channel_layout,omitempty"`
	SampleFormat      string  `json:"sample_format,omitempty"`
	BitsPerSample     int     `json:"bits_per_sample,omitempty"`
	IsTextSubtitle    bool    `json:"is_text_subtitle,omitempty"`
}
