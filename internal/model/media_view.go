package model

import "strconv"

// MediaView 是面向列表、详情、搜索、播放器和 Emby 的统一只读投影。
// Media 提供文件事实，外层字段覆盖同名的旧展示字段并来自共享元数据。
type MediaView struct {
	Media
	CatalogItemID  string  `gorm:"column:view_catalog_item_id" json:"catalog_item_id,omitempty"`
	SeriesID       string  `gorm:"column:view_series_id" json:"series_id,omitempty"`
	SeriesTitle    string  `gorm:"column:view_series_title" json:"series_title,omitempty"`
	SeasonID       string  `gorm:"column:view_season_id" json:"season_id,omitempty"`
	Title          string  `gorm:"column:view_title" json:"title"`
	OriginalName   string  `gorm:"column:view_original_name" json:"original_name,omitempty"`
	PosterURL      string  `gorm:"-" json:"poster_url,omitempty"`
	BackdropURL    string  `gorm:"-" json:"backdrop_url,omitempty"`
	Overview       string  `gorm:"column:view_overview" json:"overview,omitempty"`
	Rating         float32 `gorm:"column:view_rating" json:"rating"`
	Year           int     `gorm:"column:view_year" json:"year"`
	ReleaseDate    string  `gorm:"column:view_release_date" json:"release_date,omitempty"`
	SeasonNum      int     `gorm:"column:view_season_num" json:"season_num"`
	EpisodeNum     int     `gorm:"column:view_episode_num" json:"episode_num"`
	TMDbID         int     `gorm:"-" json:"tmdb_id"`
	BangumiID      int     `gorm:"-" json:"bangumi_id"`
	DoubanID       string  `gorm:"column:view_douban_id" json:"douban_id,omitempty"`
	TheTVDBID      string  `gorm:"column:view_thetvdb_id" json:"thetvdb_id,omitempty"`
	Languages      string  `gorm:"column:view_languages" json:"languages,omitempty"`
	Countries      string  `gorm:"column:view_countries" json:"countries,omitempty"`
	Genres         string  `gorm:"column:view_genres" json:"genres,omitempty"`
	NSFW           bool    `gorm:"column:view_nsfw" json:"nsfw"`
	MetadataKind   string  `gorm:"column:view_metadata_kind" json:"metadata_kind,omitempty"`
	MetadataSource string  `gorm:"column:view_metadata_source" json:"metadata_source,omitempty"`
	TMDbSnapshot   bool    `gorm:"-" json:"tmdb_snapshot,omitempty"`
	DoubanSnapshot bool    `gorm:"-" json:"douban_snapshot,omitempty"`
	TMDbStatus     string  `gorm:"-" json:"tmdb_status,omitempty"`
	DoubanStatus   string  `gorm:"-" json:"douban_status,omitempty"`
	SeriesTMDbID   int     `gorm:"-" json:"series_tmdb_id,omitempty"`

	PosterAssetID     string `gorm:"column:view_poster_asset_id" json:"-"`
	BackdropAssetID   string `gorm:"column:view_backdrop_asset_id" json:"-"`
	TMDbExternalID    string `gorm:"column:view_tmdb_external_id" json:"-"`
	BangumiExternalID string `gorm:"column:view_bangumi_external_id" json:"-"`

	ProbeDurationMS int64  `gorm:"column:view_probe_duration_ms" json:"-"`
	ProbeSizeBytes  int64  `gorm:"column:view_probe_size_bytes" json:"-"`
	ProbeContainer  string `gorm:"column:view_probe_container" json:"-"`
	ProbeWidth      int    `gorm:"column:view_probe_width" json:"-"`
	ProbeHeight     int    `gorm:"column:view_probe_height" json:"-"`
	ProbeVideoCodec string `gorm:"column:view_probe_video_codec" json:"-"`
	ProbeAudioCodec string `gorm:"column:view_probe_audio_codec" json:"-"`
}

func (v *MediaView) Normalize() {
	if v == nil {
		return
	}
	v.PosterURL = artworkViewURL(v.PosterAssetID)
	v.BackdropURL = artworkViewURL(v.BackdropAssetID)
	v.TMDbID, _ = strconv.Atoi(v.TMDbExternalID)
	v.BangumiID, _ = strconv.Atoi(v.BangumiExternalID)
	v.DurationSec = int(v.ProbeDurationMS / 1000)
	v.SizeBytes = v.ProbeSizeBytes
	v.Container = v.ProbeContainer
	v.Width = v.ProbeWidth
	v.Height = v.ProbeHeight
	v.VideoCodec = v.ProbeVideoCodec
	v.AudioCodec = v.ProbeAudioCodec
}

func artworkViewURL(assetID string) string {
	if assetID == "" {
		return ""
	}
	return "/api/artwork/" + assetID
}
