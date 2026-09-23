package repository

import (
	"context"
	"errors"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HongGuoAlbumJoin 为作品别名 w 投影官方合集 g；标题取最早已收录季，不按标题推断关系。
const HongGuoAlbumJoin = `LEFT JOIN LATERAL (
 SELECT aw.related_album_id AS id, aw.id AS work_id, aw.title FROM hongguo_works aw
 WHERE w.kind = 'series' AND w.related_album_id <> '' AND w.season_index > 0
 AND aw.related_album_id = w.related_album_id AND aw.kind = 'series' AND aw.season_index > 0
 ORDER BY aw.season_index, aw.source_id LIMIT 1
) g ON TRUE`

// HongGuoGroupDetail 是官方关系的只读投影，不拥有独立分组记录。
type HongGuoGroupDetail struct {
	ID      string             `json:"id"`
	Title   string             `json:"title"`
	Members []HongGuoGroupWork `json:"members"`
}

type HongGuoGroupWork struct {
	model.HongGuoWork
	SeasonNumber int `json:"season_number"`
}

func (r *HongGuoRepository) Group(ctx context.Context, id string) (*HongGuoGroupDetail, error) {
	if !hongguo.ValidID(id) {
		return nil, gorm.ErrRecordNotFound
	}
	group := &HongGuoGroupDetail{ID: id, Members: []HongGuoGroupWork{}}
	err := r.db.WithContext(ctx).Model(&model.HongGuoWork{}).
		Where("related_album_id = ? AND season_index > 0 AND kind = 'series'", id).
		Select("hongguo_works.*, season_index AS season_number").Order("season_index, source_id").Find(&group.Members).Error
	if err != nil {
		return nil, err
	}
	if len(group.Members) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	group.Title = group.Members[0].Title
	return group, nil
}

// SaveAlbum 仅保存经校验的官方关系；网页资料刷新不能覆盖该补充结果。
func (r *HongGuoRepository) SaveAlbum(ctx context.Context, sourceID string, album hongguo.Album) error {
	if !hongguo.ValidID(sourceID) || (album.ID != "" && (!hongguo.ValidID(album.ID) || album.Season < 1 || album.Season > 100000)) || (album.ID == "" && album.Season != 0) {
		return errors.New("红果官方合集关系无效")
	}
	var work model.HongGuoWork
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", sourceID).First(&work).Error; err != nil {
			return err
		}
		return tx.Model(&model.HongGuoWork{}).Where("id = ?", work.ID).Updates(map[string]any{"related_album_id": album.ID, "season_index": album.Season, "album_checked_at": time.Now(), "album_retry_at": nil}).Error
	})
	if err == nil {
		r.refreshSearchWork(ctx, work.ID, work.RelatedAlbumID, album.ID)
	}
	return err
}

func (r *HongGuoRepository) RetryAlbum(ctx context.Context, sourceID string, retryAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.HongGuoWork{}).Where("source_id = ?", sourceID).Update("album_retry_at", retryAt).Error
}

// PendingAlbums 以成功检查时间区分未知和无合集；游标避免同轮重复，重试标记不设冷却。
func (r *HongGuoRepository) PendingAlbums(ctx context.Context, after string, cutoff time.Time) ([]model.HongGuoWork, error) {
	rows := []model.HongGuoWork{}
	err := r.db.WithContext(ctx).Where("source_id > ? AND created_at <= ? AND (album_checked_at IS NULL OR album_retry_at IS NOT NULL)", after, cutoff).
		Order("source_id").Limit(100).Find(&rows).Error
	return rows, err
}
