package service

import (
	"context"
	"errors"

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

func (s *HongGuoDownloadService) ListWorks(ctx context.Context, page int, failedOnly bool) ([]HongGuoDownloadSummary, int64, error) {
	rows := []HongGuoDownloadSummary{}
	var total int64
	db := s.repo.DB.WithContext(ctx).Model(&model.HongGuoDownload{})
	if failedOnly {
		// 只筛选作品身份，外层仍汇总该作品的所有分集状态。
		failed := s.repo.DB.Model(&model.HongGuoDownload{}).Select("source_id").Where("status = ?", "failed")
		db = db.Where("source_id IN (?)", failed)
	}
	if err := db.Session(&gorm.Session{}).Distinct("source_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Select(`source_id, MAX(title) AS title, COUNT(*) AS total,
	 COUNT(*) FILTER (WHERE status = 'queued') AS queued,
	 COUNT(*) FILTER (WHERE status = 'downloading') AS downloading,
	 COUNT(*) FILTER (WHERE status = 'verifying') AS verifying,
	 COUNT(*) FILTER (WHERE status = 'waiting_verify') AS waiting_verify,
	 COUNT(*) FILTER (WHERE status = 'publishing') AS publishing,
	 COUNT(*) FILTER (WHERE status = 'completed') AS completed,
	 COUNT(*) FILTER (WHERE status = 'failed') AS failed,
	 COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled`).Group("source_id").Order("MIN(created_at) DESC, source_id").Limit(50).Offset((page - 1) * 50).Scan(&rows).Error
	return rows, total, err
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
		s.Wake()
	}
	return added, skipped, nil
}
