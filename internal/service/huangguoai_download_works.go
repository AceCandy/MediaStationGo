package service

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"time"
)

type HuangGuoAIDownloadWorkSummary struct {
	SourceID      string `json:"source_id"`
	Title         string `json:"title"`
	Total         int64  `json:"total"`
	Completed     int64  `json:"completed"`
	Failed        int64  `json:"failed"`
	Active        int64  `json:"active"`
	Downloading   int64  `json:"downloading"`
	WaitingVerify int64  `json:"waiting_verify"`
	Verifying     int64  `json:"verifying"`
	Publishing    int64  `json:"publishing"`
	Queued        int64  `json:"queued"`
	Cancelled     int64  `json:"cancelled"`
	Bytes         int64  `json:"bytes"`
}

func validHuangGuoAIDownloadStatus(status string) bool {
	switch status {
	case "", "queued", "downloading", "waiting_verify", "verifying", "publishing", "failed", "cancelled", "completed":
		return true
	}
	return false
}

// Works 作品级分页，状态筛选先限定作品，汇总保留该作品全部分集。
func (s *HuangGuoAIDownloadService) Works(ctx context.Context, page int, status string) ([]HuangGuoAIDownloadWorkSummary, int64, error) {
	rows := []HuangGuoAIDownloadWorkSummary{}
	var total int64
	if page < 1 || page > 1000000 || !validHuangGuoAIDownloadStatus(status) {
		return nil, 0, errors.New("下载筛选无效")
	}
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Table("huangguoai_download_works w").Where("EXISTS (SELECT 1 FROM huangguoai_downloads d WHERE d.source_id=w.source_id)")
		if status != "" {
			q = q.Where("EXISTS (SELECT 1 FROM huangguoai_downloads d WHERE d.source_id=w.source_id AND d.status=?)", status)
		}
		if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return err
		}
		pageWorks := q.Select("w.*").Order("w.created_at DESC,w.source_id").Offset((page - 1) * 50).Limit(50)
		return tx.Table("(?) w", pageWorks).Joins("JOIN huangguoai_downloads d ON d.source_id=w.source_id").Select(`w.source_id,w.title,COUNT(*) AS total,COUNT(*) FILTER (WHERE d.status='completed') AS completed,COUNT(*) FILTER (WHERE d.status='failed') AS failed,COUNT(*) FILTER (WHERE d.status IN ('downloading','verifying','publishing','waiting_verify')) AS active,COUNT(*) FILTER (WHERE d.status='downloading') AS downloading,COUNT(*) FILTER (WHERE d.status='waiting_verify') AS waiting_verify,COUNT(*) FILTER (WHERE d.status='verifying') AS verifying,COUNT(*) FILTER (WHERE d.status='publishing') AS publishing,COUNT(*) FILTER (WHERE d.status='queued') AS queued,COUNT(*) FILTER (WHERE d.status='cancelled') AS cancelled,SUM(d.bytes) AS bytes`).Group("w.source_id,w.title,w.created_at").Order("w.created_at DESC,w.source_id").Scan(&rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return rows, total, err
}
func (s *HuangGuoAIDownloadService) Episodes(ctx context.Context, id string, page int) ([]model.HuangGuoAIDownload, int64, error) {
	if !huangguoai.ValidID(id) || page < 1 || page > 1000000 {
		return nil, 0, errors.New("分集分页无效")
	}
	rows := []model.HuangGuoAIDownload{}
	var total int64
	q := s.repo.DB.WithContext(ctx).Model(&model.HuangGuoAIDownload{}).Where("source_id=?", id)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("episode,id").Offset((page - 1) * 50).Limit(50).Find(&rows).Error
	return rows, total, err
}
func (s *HuangGuoAIDownloadService) WorkAction(ctx context.Context, id, action string) (int64, error) {
	if !huangguoai.ValidID(id) {
		return 0, errors.New("来源ID无效")
	}
	q := s.repo.DB.WithContext(ctx).Model(&model.HuangGuoAIDownload{}).Where("source_id=?", id)
	values := map[string]any{}
	switch action {
	case "cancel":
		q = q.Where("status<>'completed'")
		values["status"] = "cancelled"
	case "retry":
		q = q.Where("status IN ('failed','cancelled') AND (lease_until IS NULL OR lease_until<=?)", time.Now())
		values = map[string]any{"status": "queued", "error": "", "lease_token": "", "lease_until": nil}
	default:
		return 0, errors.New("下载操作无效")
	}
	result := q.Updates(values)
	if result.Error == nil {
		s.refreshWorkTask(ctx, id)
		s.Wake()
	}
	return result.RowsAffected, result.Error
}
