package repository

import (
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// PrepareMediaSearchRefresh 捕获文件变更前的绑定；返回函数仅能在外层事务提交后调用。
// sourceIDs 用于即将新增/重绑的作品。同步失败只使索引回退，不改变已提交的数据库结果。
func (r *HongGuoRepository) PrepareMediaSearchRefresh(mediaQuery *gorm.DB, sourceIDs ...string) func() {
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
		if err != nil {
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
