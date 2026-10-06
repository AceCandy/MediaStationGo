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

// HongGuoWorkIdentitySQL 供已补齐关系的短剧作品列表使用。
const HongGuoWorkIdentitySQL = "'hg-group-' || w.related_album_id"

// HongGuoFavoriteIdentitySQL 与展示合集共用身份；未归组作品使用可重建的源 ID。
const HongGuoFavoriteIdentitySQL = "CASE WHEN w.kind = 'series' AND w.related_album_id <> '' AND w.season_index > 0 THEN 'hg-group-' || w.related_album_id ELSE w.source_id END"

// HongGuoReadyWorkSQL 暂缺合集的剧等待补充任务，不在列表中回退为独立作品。
const HongGuoReadyWorkSQL = "(w.related_album_id <> '' AND w.season_index > 0)"

// FilterHongGuoWorkIDs 用原生索引列限定作品别名 w；展示身份仍由调用方校验。
func FilterHongGuoWorkIDs(q *gorm.DB, ids []string) *gorm.DB {
	works, albums := []string{}, []string{}
	for _, id := range ids {
		if work, ok := strings.CutPrefix(id, "hg-work-"); ok && work != "" {
			works = append(works, work)
		} else if album, ok := strings.CutPrefix(id, "hg-group-"); ok && album != "" {
			albums = append(albums, album)
		}
	}
	return q.Where("w.id = ANY(?) OR (w.related_album_id = ANY(?) AND w.kind = 'series' AND w.season_index > 0)", &works, &albums)
}

// HongGuoAlbumJoin 为作品别名 w 投影官方合集 g；标题取最早已收录季，不按标题推断关系。
const HongGuoAlbumJoin = `LEFT JOIN LATERAL (
 SELECT aw.related_album_id AS id, aw.id AS work_id, aw.title FROM hongguo_works aw
 WHERE w.kind = 'series' AND w.related_album_id <> '' AND w.season_index > 0
 AND aw.related_album_id = w.related_album_id AND aw.kind = 'series' AND aw.season_index > 0
 ORDER BY aw.season_index, aw.source_id LIMIT 1
) g ON TRUE`

// HongGuoLatestMediaAddedSQL 以全局成员季计算合集时间，不受当前库文件资格限制。
const HongGuoLatestMediaAddedSQL = `CASE WHEN g.id IS NULL THEN w.latest_media_added_at ELSE (
 SELECT MAX(member.latest_media_added_at) FROM hongguo_works member
 WHERE member.related_album_id=g.id AND member.kind='series' AND member.season_index>0
) END`

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

// SaveAlbum 仅保存已核验的合集或自身第一季关系；网页资料刷新不能覆盖该结果。
func (r *HongGuoRepository) SaveAlbum(ctx context.Context, sourceID string, album hongguo.Album) error {
	if !hongguo.ValidID(sourceID) || !hongguo.ValidID(album.ID) || album.Season < 1 || album.Season > 100000 {
		return errors.New("红果官方合集关系无效")
	}
	var work model.HongGuoWork
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", sourceID).First(&work).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.HongGuoWork{}).Where("id = ?", work.ID).Updates(map[string]any{"related_album_id": album.ID, "season_index": album.Season, "album_checked_at": time.Now(), "album_retry_at": nil}).Error; err != nil {
			return err
		}
		if work.Kind != model.MetadataKindSeries {
			return nil
		}
		return promoteHongGuoFavorite(tx, sourceID, album.ID)
	})
	if err == nil {
		r.refreshSearchWork(ctx, work.ID, work.RelatedAlbumID, album.ID)
	}
	return err
}

func (r *HongGuoRepository) RetryAlbum(ctx context.Context, sourceID string, retryAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.HongGuoWork{}).Where("source_id = ?", sourceID).Update("album_retry_at", retryAt).Error
}

// PendingAlbums 补齐缺失关系并重试失败项；已核验的自身第一季不重复处理。
func (r *HongGuoRepository) PendingAlbums(ctx context.Context, after string, cutoff time.Time) ([]model.HongGuoWork, error) {
	rows := []model.HongGuoWork{}
	err := r.db.WithContext(ctx).Where("source_id > ? AND created_at <= ? AND (album_checked_at IS NULL OR album_retry_at IS NOT NULL OR related_album_id = '' OR season_index <= 0)", after, cutoff).
		Order("source_id").Limit(100).Find(&rows).Error
	return rows, err
}
