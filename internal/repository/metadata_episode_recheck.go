package repository

import (
	"context"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// TMDbEpisodeMetadataRecheckCandidate 是具备 Series TMDb 标识的集信息复查候选。
type TMDbEpisodeMetadataRecheckCandidate struct {
	MetadataID   string `gorm:"column:metadata_id"`
	Title        string
	Overview     string
	ReleaseDate  string
	SeriesTitle  string `gorm:"column:series_title"`
	SeasonNum    int
	EpisodeNum   int
	SeriesTMDbID string `gorm:"column:series_tm_db_id"`
	StillMissing bool   `gorm:"column:still_missing"`
}

// ListTMDbEpisodeMetadataRecheckAfter 按 ID 返回有媒体且缺少集信息的冷却到期候选。
func (r *MetadataRepository) ListTMDbEpisodeMetadataRecheckAfter(ctx context.Context, afterID string, checkedBefore time.Time, limit int) ([]TMDbEpisodeMetadataRecheckCandidate, error) {
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, 1000)
	q := r.db.WithContext(ctx).Table("metadata_items AS episode").
		Joins("JOIN metadata_items AS season ON season.id = episode.parent_id AND season.kind = ?", model.MetadataKindSeason).
		Joins("JOIN metadata_items AS series ON series.id = season.parent_id AND series.kind = ?", model.MetadataKindSeries).
		Where("episode.kind = ?", model.MetadataKindEpisode).
		Where("EXISTS (SELECT 1 FROM media m WHERE m.metadata_id = episode.id)").
		Where("episode.tmdb_episode_checked_at IS NULL OR episode.tmdb_episode_checked_at < ?", checkedBefore).
		Where(`(btrim(episode.title) = '' OR btrim(episode.overview) = '' OR btrim(episode.release_date) = ''
OR btrim(episode.title) ~* '^(episode|ep[.]?)[[:space:]._-]*0*[0-9]+$'
OR btrim(episode.title) ~ '^第[[:space:]]*[0-9]+[[:space:]]*(集|话|話)$'
OR NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = episode.id AND ma.artwork_type = 'still'))`).
		Where(`(SELECT count(*) FROM metadata_identifiers mid
WHERE mid.metadata_id = series.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'series' AND btrim(mid.external_id) ~ '^[0-9]+$') = 1`)
	if afterID = strings.TrimSpace(afterID); afterID != "" {
		q = q.Where("episode.id > ?", afterID)
	}
	var rows []TMDbEpisodeMetadataRecheckCandidate
	err := q.Select(`episode.id AS metadata_id, episode.title, episode.overview, episode.release_date,
series.title AS series_title, season.season_num, episode.episode_num,
(SELECT btrim(mid.external_id) FROM metadata_identifiers mid
 WHERE mid.metadata_id = series.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'series' AND btrim(mid.external_id) ~ '^[0-9]+$'
 LIMIT 1) AS series_tm_db_id,
NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id
 WHERE ma.metadata_id = episode.id AND ma.artwork_type = 'still') AS still_missing`).
		Order("episode.id ASC").Limit(limit).Scan(&rows).Error
	return rows, err
}
