package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"slices"
	"strings"
	"time"
)

func (r *HuangGuoAIRepository) RecordProgress(ctx context.Context, userID, sessionID string, media model.MediaView, position, duration int64, completed bool) error {
	if userID == "" || media.ID == "" || media.CatalogSource != model.TaskSystemHuangGuoAI || !huangguoai.ValidID(media.LookupCatalogID) || position < 0 || duration <= 0 || position > duration {
		return errors.New("黄果 AI进度参数无效")
	}
	now := time.Now()
	state := model.HuangGuoAIUserState{UserID: userID, SourceID: media.LookupCatalogID, EpisodeNumber: max(1, media.EpisodeNum), MediaID: media.ID, PositionMs: position, DurationMs: duration, Completed: completed, WatchedAt: &now}
	state.ResumePositionMs = ResumePosition(position, completed)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}}, DoUpdates: progressUpdates("huangguoai_user_states")}).Create(&state).Error; err != nil {
			return err
		}
		if strings.TrimSpace(sessionID) == "" {
			return nil
		}
		event := model.HuangGuoAIPlaybackEvent{UserID: userID, SessionID: strings.TrimSpace(sessionID), SourceID: state.SourceID, EpisodeNumber: state.EpisodeNumber, MediaID: media.ID, LibraryID: media.LibraryID, PlayedAt: now}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
	})
}

func (r *HuangGuoAIRepository) MarkPlayed(ctx context.Context, userID string, media model.MediaView, played bool) error {
	return r.MarkPlayedBatch(ctx, userID, []model.MediaView{media}, played)
}

// MarkPlayedBatch 原子更新已按源作品/集号去重的可见分集，不生成或删除播放事件。
func (r *HuangGuoAIRepository) MarkPlayedBatch(ctx context.Context, userID string, media []model.MediaView, played bool) error {
	if len(media) == 0 {
		return nil
	}
	states := make([]model.HuangGuoAIUserState, 0, len(media))
	now := time.Now()
	seen := map[string]bool{}
	for _, view := range media {
		if userID == "" || view.ID == "" || view.CatalogSource != model.TaskSystemHuangGuoAI || !huangguoai.ValidID(view.LookupCatalogID) {
			return errors.New("黄果 AI已看参数无效")
		}
		key := view.LookupCatalogID + ":" + fmt.Sprint(max(1, view.EpisodeNum))
		if seen[key] {
			continue
		}
		seen[key] = true
		states = append(states, model.HuangGuoAIUserState{UserID: userID, SourceID: view.LookupCatalogID, EpisodeNumber: max(1, view.EpisodeNum), MediaID: view.ID,
			Completed: true, WatchedAt: &now, DurationMs: view.ProbeDurationMS, PositionMs: view.ProbeDurationMS})
	}
	if played {
		return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}}, DoUpdates: clause.AssignmentColumns([]string{"media_id", "position_ms", "duration_ms", "resume_position_ms", "completed", "watched_at", "updated_at"})}).CreateInBatches(&states, 500).Error
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for batch := range slices.Chunk(states, 500) {
			keys := make([][]any, 0, len(batch))
			for _, state := range batch {
				keys = append(keys, []any{state.SourceID, state.EpisodeNumber})
			}
			if err := tx.Where("user_id = ? AND (source_id,episode_number) IN ?", userID, keys).Delete(&model.HuangGuoAIUserState{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *HuangGuoAIRepository) UserState(ctx context.Context, userID, id string, episode int, filter MediaQueryFilter) (model.HuangGuoAIUserState, error) {
	state := model.HuangGuoAIUserState{UserID: userID, SourceID: id, EpisodeNumber: episode}
	err := r.db.WithContext(ctx).Table("(?) state", PlaybackStates(ctx, r.db, "huangguoai", userID, filter)).Where("source_id=? AND episode_number=?", id, episode).Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return state, err
}
func (r *HuangGuoAIRepository) Favorite(ctx context.Context, userID, id string, value *bool) (bool, error) {
	if userID == "" || !huangguoai.ValidID(id) {
		return false, errors.New("收藏身份无效")
	}
	state := model.HuangGuoAIFavorite{UserID: userID, SourceID: id}
	if value == nil {
		err := r.db.WithContext(ctx).Where("user_id=? AND source_id=?", userID, id).Take(&state).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		}
		return state.Favorite, err
	}
	state.Favorite = *value
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "source_id"}}, DoUpdates: clause.AssignmentColumns([]string{"favorite", "updated_at"})}).Create(&state).Error
	return state.Favorite, err
}
func (r *HuangGuoAIRepository) MarkPreviousEpisodes(ctx context.Context, userID, id string, episode int, filter MediaQueryFilter) error {
	if episode <= 1 {
		return nil
	}
	var states []model.HuangGuoAIUserState
	q := (&MediaViewRepository{db: r.db}).huangGuoAIFileScope(ctx, filter).
		Joins("LEFT JOIN media_probe_metadata probe ON probe.media_id=m.id").
		Joins("LEFT JOIN huangguoai_user_states state ON state.source_id=w.source_id AND state.episode_number=ep.number AND state.user_id=?", userID).
		Where("w.source_id=? AND w.kind='series' AND ep.number<? AND NOT COALESCE(state.completed,FALSE)", id, episode).
		Select("DISTINCT ON (ep.id) ep.number AS episode_number,m.id AS media_id,COALESCE(NULLIF(state.duration_ms,0),probe.duration_ms,0) AS duration_ms").
		Order("ep.id,m.created_at DESC,m.id DESC")
	if err := q.Scan(&states).Error; err != nil || len(states) == 0 {
		return err
	}
	now := time.Now()
	for i := range states {
		states[i].UserID, states[i].SourceID = userID, id
		states[i].PositionMs, states[i].Completed, states[i].WatchedAt = states[i].DurationMs, true, &now
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}},
		DoUpdates: clause.AssignmentColumns([]string{"media_id", "position_ms", "duration_ms", "resume_position_ms", "completed", "watched_at", "updated_at"}),
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "NOT huangguoai_user_states.completed"}}},
	}).CreateInBatches(&states, 200).Error
}

type HuangGuoAIUserCard struct {
	SourceID      string    `json:"source_id"`
	Title         string    `json:"title"`
	Kind          string    `json:"kind"`
	MediaID       string    `json:"media_id"`
	EpisodeNumber int       `json:"episode_number"`
	PositionMs    int64     `json:"position_ms"`
	DurationMs    int64     `json:"duration_ms"`
	Completed     bool      `json:"completed"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (r *HuangGuoAIRepository) UserCards(ctx context.Context, userID, tab string, page, size int, filter MediaQueryFilter) ([]HuangGuoAIUserCard, int64, error) {
	if userID == "" || page < 1 || page > 1000000 || size < 1 || size > 100 || (tab != "favourites" && tab != "history" && tab != "continue") {
		return nil, 0, errors.New("用户记录查询无效")
	}
	db := r.db.WithContext(ctx)
	var q *gorm.DB
	key := "s.source_id || ':' || s.episode_number"
	fields := "s.episode_number,s.position_ms,s.duration_ms,s.completed"
	order := "s.updated_at DESC,CASE WHEN m.id=s.media_id THEN 0 ELSE 1 END,m.id"
	if tab == "favourites" {
		q = db.Table("huangguoai_favorites s").Joins("JOIN huangguoai_works w ON w.source_id=s.source_id").Where("s.favorite")
		key = "s.source_id"
		fields = "0 AS episode_number,0 AS position_ms,0 AS duration_ms,FALSE AS completed"
		order = "ep.number,m.id"
	} else {
		q = db.Table("(?) s", PlaybackStates(ctx, r.db, "huangguoai", userID, filter)).Joins("JOIN huangguoai_works w ON w.source_id=s.source_id").Where("s.watched_at IS NOT NULL")
		if tab == "continue" {
			key = "s.source_id"
			q = q.Where("s.position_ms>0")
		}
	}
	q = q.Joins("JOIN huangguoai_media_bindings b ON b.work_id=w.id").Joins("JOIN media m ON m.id=b.media_id AND m.catalog_source='huangguoai'").Joins("JOIN huangguoai_episodes ep ON ep.id=b.episode_id AND ep.work_id=w.id").Where("s.user_id=? AND w.projection_error='' AND (w.kind='series' OR ep.number=1)", userID)
	if tab != "favourites" {
		q = q.Where("ep.number=s.episode_number")
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id=ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id<>ALL(?)", &filter.HiddenLibraryIDs)
	}
	q = q.Select("DISTINCT ON (" + key + ") w.source_id,w.title,w.kind,m.id AS media_id," + fields + ",s.updated_at").Order(key + "," + order)
	outer := db.Table("(?) cards", q)
	var total int64
	if err := outer.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []HuangGuoAIUserCard{}
	err := outer.Order("updated_at DESC,source_id,episode_number").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	return rows, total, err
}
