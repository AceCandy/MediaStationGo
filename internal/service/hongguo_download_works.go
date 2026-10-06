package service

import (
	"context"
	"errors"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HongGuoDownloadSummary 汇总整部作品所有任务，不受展开分集的分页影响。
type HongGuoDownloadSummary struct {
	SourceID      string `json:"source_id"`
	Title         string `json:"title"`
	Total         int64  `json:"total"`
	Queued        int64  `json:"queued"`
	Downloading   int64  `json:"downloading"`
	Verifying     int64  `json:"verifying"`
	WaitingVerify int64  `json:"waiting_verify"`
	Publishing    int64  `json:"publishing"`
	Completed     int64  `json:"completed"`
	Failed        int64  `json:"failed"`
	Cancelled     int64  `json:"cancelled"`
}

func (s *HongGuoDownloadService) ListWorks(ctx context.Context, page int, status, keyword string) ([]HongGuoDownloadSummary, int64, error) {
	rows := []HongGuoDownloadSummary{}
	filter := ""
	args := []any{}
	if status != "" {
		condition := "d.status = ?"
		switch status {
		case "queued", "downloading", "waiting_verify", "verifying", "publishing", "completed", "failed", "cancelled":
			// 仅固定白名单写入 SQL，使通用预编译计划也能区分常见、稀少和空状态。
			condition = "d.status = '" + status + "'"
		default:
			args = append(args, status)
		}
		filter = `WHERE EXISTS (SELECT 1 FROM hongguo_downloads d WHERE d.source_id COLLATE "C" = w.source_id COLLATE "C" AND ` + condition + `)`
	}
	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		if filter == "" {
			filter = "WHERE "
		} else {
			filter += " AND "
		}
		filter += "(w.title ILIKE ? OR w.source_id = ?)"
		pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(keyword) + "%"
		args = append(args, pattern, keyword)
	}
	args = append(args, (page-1)*50)
	// 同一份候选同时用于计数和分页，避免重复探测每部作品；空位置由首任务查询排除。
	// 排序仍取当前最早任务，分集清理后不能用位置创建时间替代。
	// 来源 ID 以字节匹配对应索引，页面的来源 ID 排序保留数据库原排序规则。
	query := `WITH download_candidates AS MATERIALIZED (
	SELECT w.source_id, first_task.created_at FROM hongguo_download_works w
	JOIN LATERAL (SELECT created_at FROM hongguo_downloads d
	WHERE d.source_id COLLATE "C" = w.source_id COLLATE "C" ORDER BY created_at LIMIT 1) first_task ON TRUE ` + filter + `
	), paged_sources AS (
	SELECT source_id FROM download_candidates ORDER BY created_at DESC, source_id LIMIT 50 OFFSET ?
	), summaries AS (
	SELECT source_id, MAX(title) AS title, COUNT(*) AS total, MIN(created_at) AS first_task_at,
	COUNT(*) FILTER (WHERE status = 'queued') AS queued,
	COUNT(*) FILTER (WHERE status = 'downloading') AS downloading,
	COUNT(*) FILTER (WHERE status = 'verifying') AS verifying,
	COUNT(*) FILTER (WHERE status = 'waiting_verify') AS waiting_verify,
	COUNT(*) FILTER (WHERE status = 'publishing') AS publishing,
	COUNT(*) FILTER (WHERE status = 'completed') AS completed,
	COUNT(*) FILTER (WHERE status = 'failed') AS failed,
	COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled
	FROM hongguo_downloads WHERE source_id IN (SELECT source_id FROM paged_sources) GROUP BY source_id
	), totals AS (SELECT COUNT(*) AS work_total FROM download_candidates)
	SELECT summaries.*, totals.work_total FROM totals LEFT JOIN summaries ON TRUE
	ORDER BY summaries.first_task_at DESC, summaries.source_id`
	var result []struct {
		HongGuoDownloadSummary
		WorkTotal int64
	}
	if err := s.repo.DB.WithContext(ctx).Raw(query, args...).Scan(&result).Error; err != nil {
		return nil, 0, err
	}
	var total int64
	for _, item := range result {
		total = item.WorkTotal
		// 越界或空页仍由 LEFT JOIN 返回总数，空的来源 ID 不代表真实作品。
		if item.SourceID != "" {
			rows = append(rows, item.HongGuoDownloadSummary)
		}
	}
	return rows, total, nil
}

func (s *HongGuoDownloadService) ListEpisodes(ctx context.Context, sourceID string, page int) ([]model.HongGuoDownload, int64, error) {
	rows := []model.HongGuoDownload{}
	var total int64
	db := s.repo.DB.WithContext(ctx).Model(&model.HongGuoDownload{}).Where("source_id = ?", sourceID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order(`CASE status WHEN 'downloading' THEN 0 WHEN 'verifying' THEN 1 WHEN 'publishing' THEN 1 WHEN 'waiting_verify' THEN 2 WHEN 'failed' THEN 3 WHEN 'queued' THEN 4 WHEN 'cancelled' THEN 5 ELSE 6 END, episode, id`).Limit(50).Offset((page - 1) * 50).Find(&rows).Error
	return rows, total, err
}

// RetryFailedWork 只锁定并重试本作品失败任务；缺少来源资料的分集保留失败状态。
func (s *HongGuoDownloadService) RetryFailedWork(ctx context.Context, sourceID string) (int, int, error) {
	if !hongguo.ValidID(sourceID) {
		return 0, 0, errors.New("红果作品 ID 无效")
	}
	added, skipped := 0, 0
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.HongGuoDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ? AND status = ?", sourceID, "failed").Order("id").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if err := retryHongGuoDownload(tx, row); errors.Is(err, errDownloadEpisodeMissing) {
				skipped++
			} else if err != nil {
				return err
			} else {
				added++
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	if added > 0 {
		s.refreshWorkTask(ctx, sourceID)
		s.Wake()
	}
	return added, skipped, nil
}
