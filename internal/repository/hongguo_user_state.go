package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordProgress 的坐标来自已验证的媒体投影；业务层负责权限、门槛和完成度。
func (r *HongGuoRepository) RecordProgress(ctx context.Context, userID, sessionID string, media model.MediaView, position, duration int64, completed bool) error {
	if userID == "" || media.ID == "" || media.CatalogSource != model.TaskSystemHongGuo || !hongguo.ValidID(media.LookupCatalogID) || position < 0 || duration <= 0 || position > duration {
		return errors.New("红果进度参数无效")
	}
	now := time.Now()
	state := model.HongGuoUserState{UserID: userID, SourceID: media.LookupCatalogID, EpisodeNumber: max(1, media.EpisodeNum), MediaID: media.ID, PositionMs: position, DurationMs: duration, Completed: completed, WatchedAt: &now}
	state.ResumePositionMs = ResumePosition(position, completed)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}}, DoUpdates: progressUpdates("hongguo_user_states")}).Create(&state).Error; err != nil {
			return err
		}
		if strings.TrimSpace(sessionID) == "" {
			return nil
		}
		event := model.HongGuoPlaybackEvent{UserID: userID, SessionID: strings.TrimSpace(sessionID), SourceID: state.SourceID, EpisodeNumber: state.EpisodeNumber, MediaID: media.ID, LibraryID: media.LibraryID, PlayedAt: now}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
	})
}

func (r *HongGuoRepository) UserState(ctx context.Context, userID, sourceID string, episode int, filters ...MediaQueryFilter) (model.HongGuoUserState, error) {
	state := model.HongGuoUserState{UserID: userID, SourceID: sourceID, EpisodeNumber: episode}
	filter := MediaQueryFilter{IncludeNSFW: true}
	if len(filters) > 0 {
		filter = filters[0]
	}
	err := r.db.WithContext(ctx).Table("(?) AS state", PlaybackStates(ctx, r.db, "hongguo", userID, filter)).Where("source_id = ? AND episode_number = ?", sourceID, episode).Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return state, err
}

func (r *HongGuoRepository) SetFavorite(ctx context.Context, userID, sourceID string, favorite bool) error {
	if userID == "" || !hongguo.ValidID(sourceID) {
		return errors.New("红果收藏参数无效")
	}
	state := model.HongGuoUserState{UserID: userID, SourceID: sourceID, Favorite: favorite}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}}, DoUpdates: clause.AssignmentColumns([]string{"favorite", "updated_at"})}).Create(&state).Error
}

func (r *HongGuoRepository) MarkPlayed(ctx context.Context, userID string, media model.MediaView, played bool) error {
	if userID == "" || media.ID == "" || media.CatalogSource != model.TaskSystemHongGuo || !hongguo.ValidID(media.LookupCatalogID) {
		return errors.New("红果已看参数无效")
	}
	if !played {
		return r.db.WithContext(ctx).Where("user_id = ? AND source_id = ? AND episode_number = ?", userID, media.LookupCatalogID, max(1, media.EpisodeNum)).Delete(&model.HongGuoUserState{}).Error
	}
	now := time.Now()
	state := model.HongGuoUserState{UserID: userID, SourceID: media.LookupCatalogID, EpisodeNumber: max(1, media.EpisodeNum), MediaID: media.ID, Completed: played}
	if played {
		state.WatchedAt = &now
		state.DurationMs = media.ProbeDurationMS
		state.PositionMs = media.ProbeDurationMS
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "source_id"}, {Name: "episode_number"}}, DoUpdates: clause.AssignmentColumns([]string{"media_id", "position_ms", "duration_ms", "resume_position_ms", "completed", "watched_at", "updated_at"})}).Create(&state).Error
}

// HongGuoUserCard 按当前可见文件展示用户状态，不公开其他用户及来源原始地址。
type HongGuoUserCard struct {
	SourceID      string    `json:"source_id"`
	Title         string    `json:"title"`
	Kind          string    `json:"kind"`
	MediaID       string    `json:"media_id"`
	SeasonNumber  int       `json:"season_number"`
	EpisodeNumber int       `json:"episode_number"`
	PositionMs    int64     `json:"position_ms"`
	DurationMs    int64     `json:"duration_ms"`
	Completed     bool      `json:"completed"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (r *HongGuoRepository) UserCards(ctx context.Context, userID, tab string, page, size int, filter MediaQueryFilter) ([]HongGuoUserCard, int64, error) {
	if userID == "" || (tab != "favourites" && tab != "history" && tab != "continue") || page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, 0, errors.New("用户资料查询参数无效")
	}
	q := r.db.WithContext(ctx).Table("(?) AS s", PlaybackStates(ctx, r.db, "hongguo", userID, filter)).
		Joins("JOIN hongguo_works w ON w.source_id = s.source_id").
		Joins("JOIN hongguo_media_bindings b ON b.work_id = w.id").
		Joins("JOIN media m ON m.id = b.media_id AND m.catalog_source = 'hongguo'").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id").
		Joins(HongGuoAlbumJoin).
		Where("s.user_id = ?", userID)
	if tab == "favourites" {
		q = q.Where("s.episode_number = 0 AND s.favorite")
	} else {
		q = q.Where("s.episode_number > 0 AND s.episode_number = COALESCE(ep.number,1) AND s.watched_at IS NOT NULL")
		if tab == "continue" {
			q = q.Where("s.position_ms > 0")
		}
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	key := "CASE WHEN s.episode_number = 0 AND g.id IS NOT NULL THEN g.id ELSE s.source_id || ':' || s.episode_number END"
	if tab == "continue" {
		// 继续观看按展示剧集聚合；完整历史仍保留每个源分集。
		key = "CASE WHEN g.id IS NOT NULL THEN 'group:' || g.id ELSE 'work:' || s.source_id END"
	}
	q = q.Select("DISTINCT ON (" + key + ") s.source_id, COALESCE(g.title,w.title) AS title, w.kind, m.id AS media_id, CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END AS season_number, s.episode_number, s.position_ms, s.duration_ms, s.completed, s.updated_at").Order(key + ", s.updated_at DESC, CASE WHEN m.id = s.media_id THEN 0 ELSE 1 END, m.id")
	outer := r.db.WithContext(ctx).Table("(?) AS cards", q)
	var total int64
	if err := outer.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []HongGuoUserCard{}
	err := outer.Order("updated_at DESC, source_id, episode_number").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	return rows, total, err
}
