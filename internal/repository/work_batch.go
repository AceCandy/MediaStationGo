package repository

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
)

// WorkBatchPage 按稳定 ordinal 每批取 50 个候选；资格查询返回 work_batch 的 ordinal。
// 以行序号而非作品 ID 关联，保留同一作品在不同分组中的独立资格和计数。
// 计数与补取共享只读快照；countEligible 只允许改变执行计划，不得改变资格。
func (r *MediaViewRepository) WorkBatchPage(ctx context.Context, candidates, eligible *gorm.DB, start, limit int, count bool, countEligible ...*gorm.DB) (ids []string, total int64, err error) {
	page, total, err := r.workBatchPage(ctx, candidates, eligible, start, limit, count, false, countEligible...)
	for _, row := range page {
		ids = append(ids, row.ID)
	}
	return
}

type workBatchRow struct {
	ID       string
	Latest   *time.Time
	Eligible bool
}

// workBatchPage 可把已有 latest 随选页返回，避免最近添加在页后重算候选与日期。
func (r *MediaViewRepository) workBatchPage(ctx context.Context, candidates, eligible *gorm.DB, start, limit int, count, includeLatest bool, countEligible ...*gorm.DB) (page []workBatchRow, total int64, err error) {
	columns := "b.id"
	if includeLatest {
		columns += ", b.latest"
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if count {
			qualified := eligible
			if len(countEligible) > 0 {
				qualified = countEligible[0]
			}
			if err := tx.Raw("WITH work_batch AS MATERIALIZED (?) SELECT COUNT(*) FROM (?) qualified", candidates, qualified).Scan(&total).Error; err != nil {
				return err
			}
			if int64(start) >= total {
				return nil
			}
		}
		// ponytail: 多种排序复用 OFFSET；极大稀疏偏移成为瓶颈时再按排序键改为游标。
		for offset := 0; ; offset += 50 {
			batch := candidates.Session(&gorm.Session{}).Limit(50).Offset(offset)
			var rows []workBatchRow
			if err := tx.Raw(`WITH work_batch AS MATERIALIZED (?), qualified AS MATERIALIZED (?)
SELECT `+columns+`, q.ordinal IS NOT NULL AS eligible FROM work_batch b
LEFT JOIN qualified q ON q.ordinal=b.ordinal ORDER BY b.ordinal`, batch, eligible).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if !row.Eligible {
					continue
				}
				if start > 0 {
					start--
					continue
				}
				page = append(page, row)
				if limit > 0 && len(page) == limit {
					return nil
				}
			}
			if len(rows) < 50 {
				return nil
			}
		}
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return
}

// FilterVisibleWorkLibraries 用现存库集合预筛权限；NULL 归属仍须精确检查文件。
func FilterVisibleWorkLibraries(db, q *gorm.DB, column string, libraries []string, filter MediaQueryFilter) *gorm.DB {
	q = FilterWorkLibraries(q, column, libraries)
	q = FilterWorkLibraries(q, column, filter.AllowedLibraryIDs)
	if len(libraries) == 0 && len(filter.AllowedLibraryIDs) == 0 && len(filter.HiddenLibraryIDs) == 0 {
		return q
	}
	visible := db.Table("jsonb_array_elements_text(" + column + ") AS library(id)").Select("1")
	if len(libraries) > 0 {
		visible = visible.Where("library.id = ANY(?)", &libraries)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		visible = visible.Where("library.id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		visible = visible.Where("library.id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	unknown := column + " IS NULL"
	if len(libraries) == 0 && len(filter.AllowedLibraryIDs) == 0 {
		unknown += " OR " + column + " = '[]'::jsonb"
	}
	return q.Where("("+unknown+" OR EXISTS (?))", visible)
}
