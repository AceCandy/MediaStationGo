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
}

type PlaybackStatsBucket struct {
	Period string `json:"period"`
	Count  int64  `json:"count"`
}

type PlaybackStatsResult struct {
	Total   int64                 `json:"total"`
	Buckets []PlaybackStatsBucket `json:"buckets"`
}

func (r *PlaybackEventRepository) Stats(ctx context.Context, filter PlaybackStatsFilter) (*PlaybackStatsResult, error) {
	q := r.db.WithContext(ctx).
		Table("playback_events AS pe").
		Joins("LEFT JOIN metadata_items AS mi ON mi.id = pe.metadata_id AND mi.deleted_at IS NULL").
		Where("pe.deleted_at IS NULL AND pe.played_at >= ? AND pe.played_at < ?", filter.From, filter.To)
	if filter.UserID != "" {
		q = q.Where("pe.user_id = ?", filter.UserID)
	}
	if len(filter.LibraryIDs) > 0 {
		q = q.Where("pe.library_id IN ?", filter.LibraryIDs)
	}
	switch filter.MediaType {
	case "movie":
		q = q.Where("mi.kind = ?", "movie")
	case "tv":
		q = q.Where("mi.kind IN ?", []string{"series", "season", "episode"})
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	type row struct {
		Period time.Time
		Count  int64
	}
	var rows []row
	periodSQL := "DATE_TRUNC(?, pe.played_at AT TIME ZONE ?)"
	if err := q.Select(periodSQL+" AS period, COUNT(*) AS count", filter.Grain, filter.TimeZone).
		Group("period").
		Order("period ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	buckets := make([]PlaybackStatsBucket, 0, len(rows))
	for _, row := range rows {
		format := "2006-01-02"
		if filter.Grain == "month" {
			format = "2006-01"
		}
		buckets = append(buckets, PlaybackStatsBucket{Period: row.Period.Format(format), Count: row.Count})
	}
	return &PlaybackStatsResult{Total: total, Buckets: buckets}, nil
}
