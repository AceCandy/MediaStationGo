package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *NFORepository) UserState(ctx context.Context, userID, itemID string, filters ...MediaQueryFilter) (model.NFOUserState, error) {
	state := model.NFOUserState{UserID: userID, ItemID: strings.TrimPrefix(itemID, "nfo-")}
	filter := MediaQueryFilter{IncludeNSFW: true}
	if len(filters) > 0 {
		filter = filters[0]
	}
	err := r.db.WithContext(ctx).Table("(?) AS state", PlaybackStates(ctx, r.db, "nfo", userID, filter)).Where("item_id = ?", state.ItemID).Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return state, err
}

// SetFavorite 只修改收藏位，不能覆盖同条目的播放进度。
func (r *NFORepository) SetFavorite(ctx context.Context, userID, itemID, mediaID string, favorite bool) error {
	if userID == "" {
		return errors.New("user id is required")
	}
	itemID = strings.TrimPrefix(itemID, "nfo-")
	var item model.NFOItem
	if err := r.db.WithContext(ctx).First(&item, "id = ?", itemID).Error; err != nil {
		return err
	}
	if item.Kind != model.MetadataKindMovie && item.Kind != model.MetadataKindSeries {
		return ErrFavoriteUnsupportedType
	}
	state := model.NFOUserState{UserID: userID, ItemID: itemID, MediaID: mediaID, Favorite: favorite}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"favorite", "updated_at"}),
	}).Create(&state).Error
}

// RecordProgress 的文件必须已通过可见性校验；状态和会话事件一起提交。
func (r *NFORepository) RecordProgress(ctx context.Context, userID, sessionID string, media model.MediaView, position, duration int64, completed bool) error {
	if userID == "" || media.ID == "" || media.CatalogSource != model.CatalogSourceNFO || !strings.HasPrefix(media.CatalogItemID, "nfo-") || position < 0 || duration <= 0 || position > duration {
		return errors.New("本地媒体进度参数无效")
	}
	now := time.Now()
	state := model.NFOUserState{UserID: userID, ItemID: strings.TrimPrefix(media.CatalogItemID, "nfo-"), MediaID: media.ID, PositionMs: position, DurationMs: duration, Completed: completed, WatchedAt: &now}
	state.ResumePositionMs = ResumePosition(position, completed)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "item_id"}}, DoUpdates: progressUpdates("nfo_user_states")}).Create(&state).Error; err != nil {
			return err
		}
		if strings.TrimSpace(sessionID) == "" {
			return nil
		}
		event := model.NFOPlaybackEvent{UserID: userID, SessionID: strings.TrimSpace(sessionID), ItemID: state.ItemID, MediaID: media.ID, LibraryID: media.LibraryID, PlayedAt: now}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
	})
}

func saveNFOProgress(tx *gorm.DB, state *model.NFOUserState) error {
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"media_id", "position_ms", "duration_ms", "resume_position_ms", "completed", "watched_at", "updated_at"}),
	}).Create(state).Error
}

// MarkPlayed 取消已看只清进度，保留收藏及不可变的播放事件。
func (r *NFORepository) MarkPlayed(ctx context.Context, userID string, media model.MediaView, played bool) error {
	if userID == "" || media.ID == "" || media.CatalogSource != model.CatalogSourceNFO || !strings.HasPrefix(media.CatalogItemID, "nfo-") {
		return errors.New("本地媒体状态参数无效")
	}
	state := model.NFOUserState{UserID: userID, ItemID: strings.TrimPrefix(media.CatalogItemID, "nfo-"), MediaID: media.ID, Completed: played}
	if played {
		now := time.Now()
		state.WatchedAt, state.PositionMs, state.DurationMs = &now, media.ProbeDurationMS, media.ProbeDurationMS
	}
	return saveNFOProgress(r.db.WithContext(ctx), &state)
}

// MarkPreviousEpisodes 只补标同季、可见且有文件的较早单集，不产生事件。
func (r *NFORepository) MarkPreviousEpisodes(ctx context.Context, userID string, media model.MediaView, filter MediaQueryFilter) error {
	if media.SeasonID == "" || media.EpisodeNum <= 0 {
		return nil
	}
	q := (&MediaViewRepository{db: r.db}).nfoViewQuery(ctx, filter).
		Where("ns.id = ? AND ni.episode_num < ?", strings.TrimPrefix(media.SeasonID, "nfo-"), media.EpisodeNum)
	rows, err := scanNFOViews(q)
	if err != nil {
		return err
	}
	now := time.Now()
	seen := map[string]bool{}
	for _, row := range rows {
		id := strings.TrimPrefix(row.CatalogItemID, "nfo-")
		if seen[id] {
			continue
		}
		seen[id] = true
		state := model.NFOUserState{UserID: userID, ItemID: id, MediaID: row.ID, PositionMs: row.ProbeDurationMS, DurationMs: row.ProbeDurationMS, Completed: true, WatchedAt: &now}
		if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "item_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"media_id", "position_ms", "duration_ms", "resume_position_ms", "completed", "watched_at", "updated_at"}),
			Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "NOT nfo_user_states.completed"}}},
		}).Create(&state).Error; err != nil {
			return err
		}
	}
	return nil
}

// HasMedia 避免无本地资料文件时启动独立目录查询。
func (r *NFORepository) HasMedia(ctx context.Context) (bool, error) {
	var found bool
	// 固定来源保留为字面量，避免通用预编译计划按常见来源估算而顺序扫描。
	err := r.db.WithContext(ctx).Raw("SELECT EXISTS(SELECT 1 FROM media WHERE catalog_source = 'nfo')").Scan(&found).Error
	return found, err
}

// History 先按可见文件筛选及整剧归组，再分页；只投影公共响应，不写公共状态表。
func (r *NFORepository) History(ctx context.Context, userID string, limit int, completed *bool, filter MediaQueryFilter) ([]model.PlaybackHistory, error) {
	q := (&MediaViewRepository{db: r.db}).nfoViewQuery(ctx, filter).
		Joins("JOIN (?) st ON st.item_id = ni.id", PlaybackStates(ctx, r.db, "nfo", userID, filter)).
		Where("st.watched_at IS NOT NULL")
	key := "ni.id"
	if completed != nil {
		if *completed {
			q = q.Where("st.completed")
		}
		if !*completed {
			q = q.Where("st.position_ms >= 20000")
			key = "COALESCE(nw.id,ni.id)"
		}
	}
	q = q.Select("DISTINCT ON (" + key + ") 'nfo-' || ni.id AS id, 'nfo-' || ni.id AS metadata_id, st.user_id, m.id AS media_id, st.position_ms, st.duration_ms, st.completed, st.watched_at").
		Order(key + ", st.watched_at DESC, ni.id, CASE WHEN m.id = st.media_id THEN 0 ELSE 1 END, m.id")
	var rows []model.PlaybackHistory
	err := r.db.WithContext(ctx).Table("(?) AS nfo_history", q).Order("watched_at DESC, id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *NFORepository) FavoriteCards(ctx context.Context, userID string, filter MediaQueryFilter) ([]model.MediaView, error) {
	views := &MediaViewRepository{db: r.db}
	q := views.nfoViewQuery(ctx, filter).
		Joins("JOIN nfo_user_states st ON st.item_id = COALESCE(nw.id,ni.id) AND st.user_id = ? AND st.favorite", userID).
		Select("DISTINCT ON (st.item_id) st.item_id, m.id AS media_id").Order("st.item_id, st.updated_at DESC, m.created_at DESC, m.id DESC")
	var cards []struct{ ItemID, MediaID string }
	if err := q.Scan(&cards).Error; err != nil {
		return nil, err
	}
	result := make([]model.MediaView, 0, len(cards))
	for _, card := range cards {
		view, err := views.NFOPresentation(ctx, card.ItemID, filter.IncludeNSFW)
		if err != nil {
			return nil, err
		}
		if view == nil {
			continue
		}
		view.ID = card.MediaID
		if view.MetadataKind == model.MetadataKindSeries {
			view.SeriesID, view.SeriesTitle = view.CatalogItemID, view.Title
		}
		result = append(result, *view)
	}
	return result, nil
}
