package repository

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// WithBatchedHongGuoSearch 返回仅供当前串行批次使用的副本；调用者须在退出时刷新不足一批的已提交变更。
// 文件事务和旧绑定捕获保持逐条执行，副本不改变其他调用者的即时索引刷新。
func (r *MediaRepository) WithBatchedHongGuoSearch(limit int) (*MediaRepository, func(context.Context)) {
	batch := &hongGuoMediaSearchBatch{repo: r.hongGuo, limit: max(1, limit)}
	writer := *r
	writer.hongGuoSearchBatch = batch
	return &writer, batch.flush
}

type hongGuoMediaSearchBatch struct {
	repo   *HongGuoRepository
	works  []model.HongGuoWork
	failed bool
	count  int
	limit  int
}

func (b *hongGuoMediaSearchBatch) add(ctx context.Context, works []model.HongGuoWork, failed bool) {
	b.works = append(b.works, works...)
	b.failed = b.failed || failed
	b.count++
	if b.count >= b.limit {
		b.flush(ctx)
	}
}

func (b *hongGuoMediaSearchBatch) flush(ctx context.Context) {
	if b.count == 0 {
		return
	}
	works, failed := b.works, b.failed
	b.works, b.failed, b.count = nil, false, 0
	b.repo.refreshMediaSearch(ctx, works, failed)
}

// PrepareMediaSearchRefresh 捕获文件变更前的绑定；返回函数仅能在外层事务提交后调用。
// sourceIDs 用于即将新增/重绑的作品。同步失败只使索引回退，不改变已提交的数据库结果。
func (r *HongGuoRepository) PrepareMediaSearchRefresh(mediaQuery *gorm.DB, sourceIDs ...string) func() {
	return r.prepareMediaSearchRefresh(mediaQuery, nil, sourceIDs...)
}

func (r *HongGuoRepository) prepareMediaSearchRefresh(mediaQuery *gorm.DB, batch *hongGuoMediaSearchBatch, sourceIDs ...string) func() {
	if r == nil {
		return func() {}
	}
	if _, ok := r.searchBackend.(MediaSearchSyncBackend); !ok {
		return func() {}
	}
	ctx := mediaQuery.Statement.Context
	db := mediaQuery.Session(&gorm.Session{NewDB: true})
	var works []model.HongGuoWork
	bound := db.Table("hongguo_media_bindings").Select("work_id").Where("media_id IN (?)", mediaQuery.Select("id"))
	q := db.Model(&model.HongGuoWork{}).Where("id IN (?)", bound)
	if len(sourceIDs) > 0 {
		q = q.Or("source_id = ANY(?)", &sourceIDs)
	}
	err := q.Select("id, related_album_id").Find(&works).Error
	return func() {
		if batch != nil {
			batch.add(ctx, works, err != nil)
			return
		}
		r.refreshMediaSearch(ctx, works, err != nil)
	}
}

func (r *HongGuoRepository) refreshMediaSearch(ctx context.Context, works []model.HongGuoWork, failed bool) {
	if failed {
		r.invalidateMediaSearch()
		return
	}
	workIDs, ids := []string{}, []string{}
	for _, work := range works {
		workIDs = append(workIDs, work.ID)
		ids = append(ids, "hg-work-"+work.ID)
		if work.RelatedAlbumID != "" {
			ids = append(ids, "hg-group-"+work.RelatedAlbumID)
		}
	}
	if len(workIDs) == 0 {
		return
	}
	workIDs = uniqueNonEmptyStrings(workIDs)
	var albums []string
	if err := r.db.WithContext(ctx).Model(&model.HongGuoWork{}).Where("id = ANY(?)", &workIDs).Pluck("related_album_id", &albums).Error; err != nil {
		r.invalidateMediaSearch()
		return
	}
	for _, album := range uniqueNonEmptyStrings(albums) {
		ids = append(ids, "hg-group-"+album)
	}
	ids = uniqueNonEmptyStrings(ids)
	for start := 0; start < len(ids); start += 200 {
		r.searchIndex.refresh(ctx, ids[start:min(start+200, len(ids))], r.searchDocuments)
	}
}

// invalidateMediaSearch 在无法确定变更身份时同时阻止旧索引读取和不完整重建的激活。
func (r *HongGuoRepository) invalidateMediaSearch() {
	r.searchMu.Lock()
	defer r.searchMu.Unlock()
	r.searchFailed.Store(true)
	if r.searchRebuild {
		r.searchRebuildInvalid = true
	}
}
