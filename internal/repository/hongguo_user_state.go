package repository

import (
	"context"
	"errors"
	"slices"
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
	if episode == 0 {
		itemID, err := hongGuoFavoriteItemID(r.db.WithContext(ctx), sourceID)
		if err != nil {
			return state, err
		}
		state.Favorite, err = hongGuoFavorite(r.db.WithContext(ctx), userID, itemID, nil)
		return state, err
	}
	filter := MediaQueryFilter{}
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
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 与 SaveAlbum 共用作品行锁，避免补齐合集与源收藏同时发生时遗漏提升。
		itemID, err := hongGuoFavoriteItemID(tx.Clauses(clause.Locking{Strength: "UPDATE"}), sourceID)
		if err != nil {
			return err
		}
		_, err = hongGuoFavorite(tx, userID, itemID, &favorite)
		return err
	})
}

func hongGuoFavoriteItemID(db *gorm.DB, sourceID string) (string, error) {
	itemID := sourceID
	err := db.Table("hongguo_works w").Where("w.source_id = ?", sourceID).Select(HongGuoFavoriteIdentitySQL).Scan(&itemID).Error
	return itemID, err
}

// promoteHongGuoFavorite 与归组/类型更新共用事务；合集收藏不随单个成员改组而迁走。
func promoteHongGuoFavorite(tx *gorm.DB, sourceID, albumID string) error {
	if err := tx.Exec(`INSERT INTO hongguo_favorites (user_id,item_id,favorite,updated_at)
SELECT user_id, ?, favorite, updated_at FROM hongguo_favorites WHERE item_id = ?
ON CONFLICT (user_id,item_id) DO NOTHING`, "hg-group-"+albumID, sourceID).Error; err != nil {
		return err
	}
	return tx.Where("item_id = ?", sourceID).Delete(&model.HongGuoFavorite{}).Error
}

func hongGuoFavorite(db *gorm.DB, userID, itemID string, favorite *bool) (bool, error) {
	state := model.HongGuoFavorite{UserID: userID, ItemID: itemID}
	if favorite == nil {
		err := db.Where("user_id = ? AND item_id = ?", userID, itemID).Take(&state).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		}
		return state.Favorite, err
	}
	state.Favorite = *favorite
	err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "item_id"}}, DoUpdates: clause.Assignments(map[string]any{
		"favorite":   gorm.Expr("EXCLUDED.favorite"),
		"updated_at": gorm.Expr("CASE WHEN hongguo_favorites.favorite = EXCLUDED.favorite THEN hongguo_favorites.updated_at ELSE EXCLUDED.updated_at END"),
	})}).Create(&state).Error
	return state.Favorite, err
}

func (r *HongGuoRepository) MarkPlayed(ctx context.Context, userID string, media model.MediaView, played bool) error {
	return r.MarkPlayedBatch(ctx, userID, []model.MediaView{media}, played)
}

// MarkPlayedBatch 原子更新已按源作品/集号去重的可见分集，不生成或删除播放事件。
func (r *HongGuoRepository) MarkPlayedBatch(ctx context.Context, userID string, media []model.MediaView, played bool) error {
	if len(media) == 0 {
		return nil
	}
	states := make([]model.HongGuoUserState, 0, len(media))
	now := time.Now()
	for _, view := range media {
		if userID == "" || view.ID == "" || view.CatalogSource != model.TaskSystemHongGuo || !hongguo.ValidID(view.LookupCatalogID) {
			return errors.New("红果已看参数无效")
		}
		states = append(states, model.HongGuoUserState{UserID: userID, SourceID: view.LookupCatalogID, EpisodeNumber: max(1, view.EpisodeNum), MediaID: view.ID,
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
			if err := tx.Where("user_id = ? AND (source_id,episode_number) IN ?", userID, keys).Delete(&model.HongGuoUserState{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
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
	q := r.db.WithContext(ctx)
	if tab == "favourites" {
		// 原生索引先定位收藏成员，不能只用 CASE 身份连接而扫描所有作品。
		q = q.Table("hongguo_favorites s").Joins(`JOIN hongguo_works w ON
(w.source_id = s.item_id OR (s.item_id LIKE 'hg-group-%' AND w.related_album_id = SUBSTRING(s.item_id FROM 10)))
AND ` + HongGuoFavoriteIdentitySQL + " = s.item_id").Where("s.favorite")
	} else {
		q = q.Table("(?) AS s", PlaybackStates(ctx, r.db, "hongguo", userID, filter)).Joins("JOIN hongguo_works w ON w.source_id = s.source_id")
		q = q.Where("s.episode_number > 0 AND s.episode_number = COALESCE(ep.number,1) AND s.watched_at IS NOT NULL")
		if tab == "continue" {
			q = q.Where("s.position_ms > 0")
		}
	}
	q = q.
		Joins("JOIN hongguo_media_bindings b ON b.work_id = w.id").
		Joins("JOIN media m ON m.id = b.media_id AND m.catalog_source = 'hongguo'").
		Joins("LEFT JOIN hongguo_episodes ep ON ep.id = b.episode_id").
		Joins(HongGuoAlbumJoin).
		Where("s.user_id = ?", userID)
	if len(filter.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	key := "s.source_id || ':' || s.episode_number"
	fields := "s.episode_number, s.position_ms, s.duration_ms, s.completed"
	order := "s.updated_at DESC, CASE WHEN m.id = s.media_id THEN 0 ELSE 1 END, m.id"
	if tab == "favourites" {
		key = "s.item_id"
		fields = "0 AS episode_number, 0 AS position_ms, 0 AS duration_ms, FALSE AS completed"
		order = "w.season_index, w.source_id, m.id"
	}
	if tab == "continue" {
		// 继续观看按展示剧集聚合；完整历史仍保留每个源分集。
		key = "CASE WHEN g.id IS NOT NULL THEN 'group:' || g.id ELSE 'work:' || s.source_id END"
	}
	q = q.Select("DISTINCT ON (" + key + ") w.source_id, COALESCE(g.title,w.title) AS title, w.kind, m.id AS media_id, CASE WHEN g.id IS NULL THEN 1 ELSE w.season_index END AS season_number, " + fields + ", s.updated_at").Order(key + ", " + order)
	outer := r.db.WithContext(ctx).Table("(?) AS cards", q)
	var total int64
	if err := outer.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []HongGuoUserCard{}
	err := outer.Order("updated_at DESC, source_id, episode_number").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	return rows, total, err
}
