package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type PlaybackEventRepository struct{ db *gorm.DB }

func (r *PlaybackEventRepository) Insert(ctx context.Context, event *model.PlaybackEvent) error {
	if event == nil || strings.TrimSpace(event.UserID) == "" || strings.TrimSpace(event.SessionID) == "" || strings.TrimSpace(event.MetadataID) == "" {
		return errors.New("user, session and metadata are required")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "session_id"}, {Name: "metadata_id"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "deleted_at"}, Value: nil},
		}},
		DoNothing: true,
	}).Create(event).Error
}

type PlaybackStatsFilter struct {
	Grain      string
	From       time.Time
	To         time.Time
	UserID     string
	MediaType  string
	LibraryIDs []string
	TimeZone   string
	Page       int
	PageSize   int
	RankGrain  string
	RankPeriod string
	RankFrom   time.Time
	RankTo     time.Time
}

type PlaybackStatsBucket struct {
	Period string `json:"period"`
	Count  int64  `json:"count"`
}

type PlaybackStatsResult struct {
	Total   int64                    `json:"total"`
	Buckets []PlaybackStatsBucket    `json:"buckets"`
	Details PlaybackStatsDetailsPage `json:"details"`
	Ranking PlaybackStatsRanking     `json:"ranking"`
}

// PlaybackStatsDetail 是一次真实播放事件的管理员展示投影。
type PlaybackStatsDetail struct {
	System         string    `json:"system"`
	SourceID       string    `json:"source_id,omitempty"`
	ID             string    `json:"id"`
	PlayedAt       time.Time `json:"played_at"`
	UserID         string    `json:"user_id"`
	UserName       string    `json:"user_name"`
	LibraryID      string    `json:"library_id"`
	LibraryName    string    `json:"library_name"`
	MediaID        string    `json:"media_id"`
	MetadataID     string    `json:"metadata_id"`
	Title          string    `json:"title"`
	SeriesTitle    string    `json:"series_title,omitempty"`
	SeasonNum      int       `json:"season_num,omitempty"`
	EpisodeNum     int       `json:"episode_num,omitempty"`
	PosterURL      string    `json:"poster_url,omitempty"`
	MediaAvailable bool      `json:"media_available"`
}

// PlaybackStatsDetailsPage 是播放事件的稳定分页结果。
type PlaybackStatsDetailsPage struct {
	Items    []PlaybackStatsDetail `json:"items"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Total    int64                 `json:"total"`
}

// PlaybackStatsRankItem 是电影作品或电视剧季度的聚合排行项。
type PlaybackStatsRankItem struct {
	System      string `json:"system"`
	GroupID     string `json:"group_id"`
	Title       string `json:"title"`
	SeriesTitle string `json:"series_title,omitempty"`
	SeasonNum   int    `json:"season_num,omitempty"`
	PosterURL   string `json:"poster_url,omitempty"`
	Count       int64  `json:"count"`
}

// PlaybackStatsRanking 是指定日或周的 Top 10。
type PlaybackStatsRanking struct {
	Grain  string                  `json:"grain"`
	Period string                  `json:"period"`
	Items  []PlaybackStatsRankItem `json:"items"`
}

func (r *PlaybackEventRepository) Stats(ctx context.Context, filter PlaybackStatsFilter) (*PlaybackStatsResult, error) {
	return queryPlaybackStats(r.db.WithContext(ctx), filter, r.playbackStatsQueries(ctx, filter))
}

func (r *PlaybackEventRepository) playbackStatsQueries(ctx context.Context, filter PlaybackStatsFilter) playbackStatsQueries {
	detailQuery := playbackStatsDisplayQuery(r.playbackStatsQuery(ctx, filter)).
		Select(`'catalog' AS system, '' AS source_id, pe.id, pe.played_at, pe.user_id,
			COALESCE(NULLIF(u.nickname, ''), u.username, '已删除账户') AS user_name,
			pe.library_id, COALESCE(l.name, '已删除媒体库') AS library_name,
			pe.media_id, pe.metadata_id, COALESCE(NULLIF(mi.title, ''), '媒体已不可用') AS title,
			COALESCE(series_metadata.title, '') AS series_title,
			COALESCE(season_metadata.season_num, 0) AS season_num,
			COALESCE(mi.episode_num, 0) AS episode_num,
			CASE WHEN COALESCE(item_poster_asset.id, season_poster_asset.id, series_poster_asset.id, '') = '' THEN ''
				ELSE '/api/artwork/' || COALESCE(item_poster_asset.id, season_poster_asset.id, series_poster_asset.id) END AS poster_url,
			(m.id IS NOT NULL) AS media_available`)

	rankSource := playbackStatsDisplayQuery(r.playbackStatsQuery(ctx, filter)).
		Where("pe.played_at >= ? AND pe.played_at < ?", filter.RankFrom, filter.RankTo).
		Select(`'catalog' AS system, CASE
				WHEN mi.kind = 'episode' THEN COALESCE(season_metadata.id, mi.id)
				ELSE mi.id
			END AS group_id,
			CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(series_metadata.title, season_metadata.title, mi.title)
				ELSE mi.title END AS title,
			CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(series_metadata.title, '') ELSE '' END AS series_title,
			CASE WHEN mi.kind IN ('episode', 'season') THEN COALESCE(season_metadata.season_num, mi.season_num, 0) ELSE 0 END AS season_num,
			CASE WHEN COALESCE(season_poster_asset.id, series_poster_asset.id, item_poster_asset.id, '') = '' THEN ''
				ELSE '/api/artwork/' || COALESCE(season_poster_asset.id, series_poster_asset.id, item_poster_asset.id) END AS poster_url`)
	return playbackStatsQueries{
		events:  r.playbackStatsQuery(ctx, filter).Select("pe.played_at"),
		details: detailQuery, ranking: rankSource,
	}
}

func (r *PlaybackEventRepository) playbackStatsQuery(ctx context.Context, filter PlaybackStatsFilter) *gorm.DB {
	q := r.db.WithContext(ctx).
		Table("playback_events AS pe").
		Joins("LEFT JOIN metadata_items AS mi ON mi.id = pe.metadata_id").
		Where("pe.deleted_at IS NULL AND pe.played_at >= ? AND pe.played_at < ?", filter.From, filter.To)
	if filter.UserID != "" {
		q = q.Where("pe.user_id = ?", filter.UserID)
	}
	if len(filter.LibraryIDs) > 0 {
		q = q.Where("pe.library_id = ANY(?)", &filter.LibraryIDs)
	}
	switch filter.MediaType {
	case "movie":
		q = q.Where("mi.kind = ?", "movie")
	case "tv":
		q = q.Where("mi.kind IN ?", []string{"series", "season", "episode"})
	}
	return q
}

func playbackStatsDisplayQuery(q *gorm.DB) *gorm.DB {
	return q.
		Joins("LEFT JOIN users AS u ON u.id = pe.user_id AND u.deleted_at IS NULL").
		Joins("LEFT JOIN libraries AS l ON l.id = pe.library_id AND l.deleted_at IS NULL").
		Joins("LEFT JOIN media AS m ON m.id = pe.media_id").
		Joins("LEFT JOIN metadata_items AS season_metadata ON season_metadata.id = CASE WHEN mi.kind = 'episode' THEN mi.parent_id WHEN mi.kind = 'season' THEN mi.id ELSE NULL END AND season_metadata.kind = 'season'").
		Joins("LEFT JOIN metadata_items AS series_metadata ON series_metadata.id = CASE WHEN mi.kind = 'episode' THEN season_metadata.parent_id WHEN mi.kind = 'season' THEN mi.parent_id WHEN mi.kind = 'series' THEN mi.id ELSE NULL END AND series_metadata.kind = 'series'").
		Joins("LEFT JOIN metadata_artworks AS item_poster ON item_poster.metadata_id = mi.id AND item_poster.artwork_type = 'poster'").
		Joins("LEFT JOIN artwork_assets AS item_poster_asset ON item_poster_asset.id = item_poster.asset_id").
		Joins("LEFT JOIN metadata_artworks AS season_poster ON season_poster.metadata_id = season_metadata.id AND season_poster.artwork_type = 'poster'").
		Joins("LEFT JOIN artwork_assets AS season_poster_asset ON season_poster_asset.id = season_poster.asset_id").
		Joins("LEFT JOIN metadata_artworks AS series_poster ON series_poster.metadata_id = series_metadata.id AND series_poster.artwork_type = 'poster'").
		Joins("LEFT JOIN artwork_assets AS series_poster_asset ON series_poster_asset.id = series_poster.asset_id")
}
