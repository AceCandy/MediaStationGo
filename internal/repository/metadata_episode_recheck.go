package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// SaveTMDbMetadataRecheck 只保存季/集展示字段和检查时间，不影响整剧搜索文档。
func (r *MetadataRepository) SaveTMDbMetadataRecheck(ctx context.Context, item *model.MetadataItem) error {
	if err := validateMetadataItem(item); err != nil {
		return err
	}
	checkedColumn, checkedAt := "tmdb_episode_checked_at", item.TMDbEpisodeCheckedAt
	switch item.Kind {
	case model.MetadataKindSeason:
		checkedColumn, checkedAt = "tmdb_season_checked_at", item.TMDbSeasonCheckedAt
	case model.MetadataKindEpisode:
	default:
		return errors.New("TMDb metadata recheck requires season or episode")
	}
	if checkedAt == nil {
		return errors.New("TMDb metadata recheck requires checked time")
	}
	result := r.db.WithContext(ctx).Model(&model.MetadataItem{}).Where("id = ? AND kind = ?", item.ID, item.Kind).
		Updates(map[string]any{"title": item.Title, "overview": item.Overview, "release_date": item.ReleaseDate, "rating": item.Rating, "year": item.Year,
			checkedColumn: gorm.Expr("GREATEST("+checkedColumn+", ?)", *checkedAt)})
	if result.Error == nil && result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

// TMDbMetadataRecheckCandidate 是具备 Series TMDb 标识的季、集信息复查候选。
type TMDbMetadataRecheckCandidate struct {
	MetadataID      string `gorm:"column:metadata_id"`
	Kind            string
	Title           string
	Overview        string
	ReleaseDate     string
	SeriesTitle     string `gorm:"column:series_title"`
	SeasonNum       int
	EpisodeNum      int
	SeriesTMDbID    string `gorm:"column:series_tm_db_id"`
	ArtworkMissing  bool   `gorm:"column:artwork_missing"`
	TMDbID          string `gorm:"column:tmdb_id"`
	SnapshotMissing bool   `gorm:"column:snapshot_missing"`
}

// ListTMDbEpisodeMetadataRecheckAfter 按 ID 返回有媒体且缺少集信息的冷却到期候选。
func (r *MetadataRepository) ListTMDbEpisodeMetadataRecheckAfter(ctx context.Context, afterID string, checkedBefore time.Time, limit int) ([]TMDbMetadataRecheckCandidate, error) {
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
		Where(`(COALESCE(btrim(episode.overview), '') = '' OR COALESCE(btrim(episode.release_date), '') = ''
OR NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = episode.id AND ma.artwork_type = 'still'))`).
		Where(`(SELECT count(*) FROM metadata_identifiers mid
WHERE mid.metadata_id = series.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'series' AND btrim(mid.external_id) ~ '^[0-9]+$') = 1`)
	if afterID = strings.TrimSpace(afterID); afterID != "" {
		q = q.Where("episode.id > ?", afterID)
	}
	var rows []TMDbMetadataRecheckCandidate
	err := q.Select(`episode.id AS metadata_id, episode.kind, episode.title, episode.overview, episode.release_date,
series.title AS series_title, season.season_num, episode.episode_num,
(SELECT btrim(mid.external_id) FROM metadata_identifiers mid
 WHERE mid.metadata_id = series.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'series' AND btrim(mid.external_id) ~ '^[0-9]+$'
 LIMIT 1) AS series_tm_db_id,
NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id
 WHERE ma.metadata_id = episode.id AND ma.artwork_type = 'still') AS artwork_missing`).
		Order("episode.id ASC").Limit(limit).Scan(&rows).Error
	return rows, err
}

// ListTMDbSeasonMetadataRecheckAfter 独立筛选有媒体的季，不依赖其子集是否缺少信息。
func (r *MetadataRepository) ListTMDbSeasonMetadataRecheckAfter(ctx context.Context, afterID string, checkedBefore time.Time, limit int) ([]TMDbMetadataRecheckCandidate, error) {
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, 1000)
	q := r.db.WithContext(ctx).Table("metadata_items AS season").
		Joins("JOIN metadata_items AS series ON series.id = season.parent_id AND series.kind = ?", model.MetadataKindSeries).
		Where("season.kind = ?", model.MetadataKindSeason).
		Where(`EXISTS (SELECT 1 FROM media m WHERE m.metadata_id = season.id)
OR EXISTS (SELECT 1 FROM metadata_items episode JOIN media m ON m.metadata_id = episode.id
 WHERE episode.parent_id = season.id AND episode.kind = 'episode')`).
		Where("season.tmdb_season_checked_at IS NULL OR season.tmdb_season_checked_at < ?", checkedBefore).
		Where(`(COALESCE(btrim(season.overview), '') = '' OR COALESCE(btrim(season.release_date), '') = ''
OR NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id WHERE ma.metadata_id = season.id AND ma.artwork_type = 'poster')
OR NOT EXISTS (SELECT 1 FROM metadata_identifiers mid WHERE mid.metadata_id = season.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'season')
OR NOT EXISTS (SELECT 1 FROM metadata_provider_snapshots snapshot WHERE snapshot.metadata_id = season.id AND snapshot.provider = 'tmdb'))`).
		Where(`(SELECT count(*) FROM metadata_identifiers mid
 WHERE mid.metadata_id = series.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'series' AND btrim(mid.external_id) ~ '^[0-9]+$') = 1`).
		Where(`(SELECT count(*) FROM metadata_identifiers mid
 WHERE mid.metadata_id = season.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'season') <= 1`)
	if afterID = strings.TrimSpace(afterID); afterID != "" {
		q = q.Where("season.id > ?", afterID)
	}
	var rows []TMDbMetadataRecheckCandidate
	err := q.Select(`season.id AS metadata_id, season.kind, season.title, season.overview, season.release_date,
series.title AS series_title, season.season_num,
(SELECT btrim(mid.external_id) FROM metadata_identifiers mid
 WHERE mid.metadata_id = series.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'series' AND btrim(mid.external_id) ~ '^[0-9]+$'
 LIMIT 1) AS series_tm_db_id,
(SELECT btrim(mid.external_id) FROM metadata_identifiers mid
 WHERE mid.metadata_id = season.id AND mid.provider = 'tmdb' AND mid.entity_kind = 'season' LIMIT 1) AS tmdb_id,
NOT EXISTS (SELECT 1 FROM metadata_provider_snapshots snapshot
 WHERE snapshot.metadata_id = season.id AND snapshot.provider = 'tmdb') AS snapshot_missing,
NOT EXISTS (SELECT 1 FROM metadata_artworks ma JOIN artwork_assets aa ON aa.id = ma.asset_id
 WHERE ma.metadata_id = season.id AND ma.artwork_type = 'poster') AS artwork_missing`).
		Order("season.id ASC").Limit(limit).Scan(&rows).Error
	return rows, err
}
