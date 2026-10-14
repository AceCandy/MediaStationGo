package repository

import (
	"context"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// 与 model.CatalogBasicsComplete 一致；图片按来源判断，不以本地下载失败触发详情刷新。
const tmdbMissingBasicsSQL = `(btrim(title) = '' OR title !~ '[一-鿿]' OR title ~ '[ぁ-ヿ가-힣]'
 OR btrim(overview) = '' OR COALESCE(NULLIF(btrim(fields->>CASE WHEN kind='episode' THEN 'still_path' ELSE 'poster_path' END),''),'') = ''
 OR (kind='episode' AND (btrim(release_date)='' OR title ~* '^(episode|ep\.?)[\s._-]*0*[0-9]+$|^第\s*[0-9]+\s*(集|话|話)$')))`

func (r *TMDbCatalogRepository) MaintenanceCandidates(ctx context.Context, kind string, inventory bool, limit int) ([]model.TMDbSubject, error) {
	rows := []model.TMDbSubject{}
	q := r.db.WithContext(ctx).Model(&model.TMDbSubject{})
	if inventory {
		q = q.Where("kind IN ('series','season') AND (next_inventory_at IS NULL OR next_inventory_at <= ?)", time.Now()).
			Where(`(kind='series' AND status IN ('Returning Series','In Production','Planned')) OR
 (kind='season' AND (COALESCE(fields->>'last_air_date','') >= ? OR (COALESCE(fields->>'last_air_date','')='' AND EXISTS(SELECT 1 FROM tmdb_subjects parent WHERE parent.id=tmdb_subjects.parent_id AND parent.status IN ('Returning Series','In Production','Planned')))))`, time.Now().UTC().AddDate(0, 0, -30).Format(time.DateOnly)).Order("next_inventory_at NULLS FIRST,id")
	} else {
		q = q.Where("kind=? AND (next_maintenance_at IS NULL OR next_maintenance_at <= ?)", kind, time.Now()).
			Where("(" + tmdbMissingBasicsSQL + " OR quality<2)").Order("next_maintenance_at NULLS FIRST,id")
	}
	if !inventory && kind == "season" {
		// 有资源的季由持久复查队列维护，不能绕过所属季冷却再次请求。
		q = q.Where(`NOT EXISTS(SELECT 1 FROM metadata_identifiers i JOIN metadata_items m ON m.id=i.metadata_id
 WHERE i.provider='tmdb' AND i.entity_kind='season' AND i.external_id=tmdb_subjects.tmdb_id::text
 AND (EXISTS(SELECT 1 FROM media f WHERE f.metadata_id=m.id) OR EXISTS(SELECT 1 FROM metadata_items e JOIN media f ON f.metadata_id=e.id WHERE e.parent_id=m.id AND e.kind='episode')))`)
	}
	err := q.Limit(limit).Find(&rows).Error
	return rows, err
}

func TMDbMaintenanceDelay(subject *model.TMDbSubject, at time.Time) time.Duration {
	return tmdbTTL(subject, "base", at)
}

// FinishMaintenance 只推进本任务检查时间，不改写来源内容。
func (r *TMDbCatalogRepository) FinishMaintenance(ctx context.Context, observed *model.TMDbSubject, inventory bool, next time.Time) error {
	column := "next_maintenance_at"
	if inventory {
		column = "next_inventory_at"
	}
	return r.db.WithContext(ctx).Model(&model.TMDbSubject{}).Where("id=?", observed.ID).UpdateColumn(column, next).Error
}

// 豆瓣只解析基础正文；网页补充和本地海报引用不决定是否需要请求基础接口。
const doubanBaseDocSQL = `(CASE WHEN jsonb_typeof(payload->'subject')='object' THEN payload->'subject' WHEN jsonb_typeof(payload->'data')='object' THEN payload->'data' ELSE payload END)`
const doubanMissingBasicsSQL = `(payload IS NULL OR quality <> 'complete'
 OR COALESCE(NULLIF(` + doubanBaseDocSQL + `->>'title',''),` + doubanBaseDocSQL + `->>'name','') !~ '[一-鿿]'
 OR COALESCE(NULLIF(` + doubanBaseDocSQL + `->>'title',''),` + doubanBaseDocSQL + `->>'name','') ~ '[ぁ-ヿ가-힣]'
 OR btrim(COALESCE(NULLIF(` + doubanBaseDocSQL + `->>'short_comment',''),NULLIF(` + doubanBaseDocSQL + `->>'intro',''),NULLIF(` + doubanBaseDocSQL + `->>'summary',''),` + doubanBaseDocSQL + `->>'abstract',''))=''
 OR btrim(COALESCE(NULLIF(` + doubanBaseDocSQL + `#>>'{cover,image,large,url}',''),` + doubanBaseDocSQL + `#>>'{pic,large}',''))='')`

func (r *DoubanRepository) maintenanceQuery(ctx context.Context, kind string, rating bool, before, now time.Time) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.DoubanSubject{}).Where("entity_kind=? AND supplement_pending=false", kind)
	if rating {
		return q.Where("(rating_checked_at IS NULL OR rating_checked_at<=?) AND (rating_retry_at IS NULL OR rating_retry_at<=?) AND (next_retry_at IS NULL OR next_retry_at<=?)", before, now, now)
	}
	return q.Where(doubanMissingBasicsSQL).Where("(fetched_at IS NULL OR fetched_at<=? OR quality='partial') AND (next_retry_at IS NULL OR next_retry_at<=?)", before, now)
}

func (r *DoubanRepository) MaintenanceCandidates(ctx context.Context, kind string, rating bool, before, now time.Time, limit int) ([]model.DoubanSubject, error) {
	rows := []model.DoubanSubject{}
	order := "fetched_at NULLS FIRST,id"
	if rating {
		order = "rating_checked_at NULLS FIRST,id"
	}
	err := r.maintenanceQuery(ctx, kind, rating, before, now).Order(order).Limit(limit).Find(&rows).Error
	return rows, err
}

// SaveRating 仅保存评分证据，不能用本次评分请求推进基础正文的成功时间。
func (r *DoubanRepository) SaveRating(ctx context.Context, observed *model.DoubanSubject, raw []byte, at time.Time) (bool, error) {
	updates := map[string]any{"rating_checked_at": at, "rating_retry_at": nil, "rating_payload": string(raw)}
	if rating := model.DoubanRating(string(raw)); rating > 0 {
		updates["rating"] = rating
	}
	result := r.db.WithContext(ctx).Model(&model.DoubanSubject{}).
		Where("id=? AND rating_checked_at IS NOT DISTINCT FROM ? AND rating_retry_at IS NOT DISTINCT FROM ?", observed.ID, observed.RatingCheckedAt, observed.RatingRetryAt).UpdateColumns(updates)
	return result.RowsAffected > 0, result.Error
}

func (r *DoubanRepository) FailRating(ctx context.Context, observed *model.DoubanSubject, retry time.Time) error {
	return r.db.WithContext(ctx).Model(&model.DoubanSubject{}).
		Where("id=? AND rating_checked_at IS NOT DISTINCT FROM ? AND rating_retry_at IS NOT DISTINCT FROM ?", observed.ID, observed.RatingCheckedAt, observed.RatingRetryAt).
		UpdateColumn("rating_retry_at", retry).Error
}
