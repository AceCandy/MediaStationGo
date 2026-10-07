package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrPlaybackProgressConflict 表示主服进度已被其他操作修改，调用方不得自动覆盖。
var ErrPlaybackProgressConflict = errors.New("播放进度版本已变化")

// ProgressIdentity 是已校验可见性的用户逻辑条目，MediaID 仅为本次具体文件。
type ProgressIdentity struct {
	UserID, Source, ItemID, MediaID string
	EpisodeNumber                   int
}

func (id ProgressIdentity) table() (string, string) {
	switch id.Source {
	case "nfo":
		return "nfo_user_states", "item_id"
	case "hongguo":
		return "hongguo_user_states", "source_id"
	case "huangguoai":
		return "huangguoai_user_states", "source_id"
	default:
		return "playback_histories", "metadata_id"
	}
}

func (id ProgressIdentity) query(db *gorm.DB) *gorm.DB {
	table, column := id.table()
	q := db.Table(table).Where("user_id = ? AND "+column+" = ?", id.UserID, id.ItemID)
	if id.Source == "hongguo" || id.Source == "huangguoai" {
		q = q.Where("episode_number = ?", id.EpisodeNumber)
	}
	if id.Source == "legacy" {
		q = q.Where("deleted_at IS NULL")
	}
	return q
}

func (id ProgressIdentity) revision(db *gorm.DB) (int64, error) {
	var row model.PlaybackProgressRevision
	err := db.Where("user_id = ? AND source = ? AND item_id = ? AND episode_number = ?",
		id.UserID, id.Source, id.ItemID, id.EpisodeNumber).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return row.Revision, err
}

// PlaybackProgressSnapshot 将有效状态和版本放在同一个数据库快照内读取。
type PlaybackProgressSnapshot struct {
	PositionMs, DurationMs, Revision int64
	Completed                        bool
}

func ReadPlaybackProgress(ctx context.Context, db *gorm.DB, id ProgressIdentity, filter MediaQueryFilter) (PlaybackProgressSnapshot, error) {
	var result PlaybackProgressSnapshot
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, column := id.table()
		q := tx.Table("(?) state", PlaybackStates(ctx, tx, id.Source, id.UserID, filter)).Where(column+" = ?", id.ItemID)
		if id.Source == "hongguo" || id.Source == "huangguoai" {
			q = q.Where("episode_number = ?", id.EpisodeNumber)
		}
		if err := q.Take(&result).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var err error
		result.Revision, err = id.revision(tx)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

// WritePlaybackProgress 准确同步零/短进度及已看位；不走自动门槛、不创建统计事件。
// 锁序必须是实际状态行→版本行，与旧写入及其 AFTER 触发器一致；冲突连占位行一起回滚。
func WritePlaybackProgress(ctx context.Context, db *gorm.DB, id ProgressIdentity, expected, position, duration int64, played bool) (int64, error) {
	if expected < 0 || position < 0 || duration <= 0 || position > duration {
		return 0, errors.New("播放进度参数无效")
	}
	var revision int64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var placeholder any
		switch id.Source {
		case "nfo":
			placeholder = &model.NFOUserState{UserID: id.UserID, ItemID: id.ItemID, MediaID: id.MediaID}
		case "hongguo":
			placeholder = &model.HongGuoUserState{UserID: id.UserID, SourceID: id.ItemID, EpisodeNumber: id.EpisodeNumber, MediaID: id.MediaID}
		case "huangguoai":
			placeholder = &model.HuangGuoAIUserState{UserID: id.UserID, SourceID: id.ItemID, EpisodeNumber: id.EpisodeNumber, MediaID: id.MediaID}
		default:
			placeholder = &model.PlaybackHistory{UserID: id.UserID, MetadataID: id.ItemID, MediaID: id.MediaID}
		}
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(placeholder)
		if insert.Error != nil {
			return insert.Error
		}
		var row struct{ UserID string }
		if err := id.query(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Select("user_id").Take(&row).Error; err != nil {
			return err
		}
		current, err := id.revision(tx)
		if err != nil {
			return err
		}
		// 自己首次建行触发一次递增；不能把这个递增误认成其他端修改。
		if current != expected+insert.RowsAffected {
			return ErrPlaybackProgressConflict
		}
		now := time.Now()
		if err := id.query(tx).Updates(map[string]any{
			"media_id": id.MediaID, "position_ms": position, "duration_ms": duration,
			"resume_position_ms": position, "completed": played, "watched_at": now, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		revision, err = id.revision(tx)
		return err
	})
	return revision, err
}
