package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type PlayerRequestLogQuery struct {
	Start    time.Time
	End      time.Time
	Route    string
	Method   string
	Status   *int
	Page     int
	PageSize int
}

// PlayerRequestLogRepository 读写按月分区的播放器请求日志。
type PlayerRequestLogRepository struct{ db *gorm.DB }

func (r *PlayerRequestLogRepository) DB() *gorm.DB { return r.db }

func (r *PlayerRequestLogRepository) Create(ctx context.Context, row *model.PlayerRequestLog) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *PlayerRequestLogRepository) List(ctx context.Context, query PlayerRequestLogQuery) ([]model.PlayerRequestLog, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.PlayerRequestLog{}).
		Where("requested_at >= ? AND requested_at < ?", query.Start, query.End)
	if route := strings.TrimSpace(query.Route); route != "" {
		route = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(route)
		db = db.Where(`route ILIKE ? ESCAPE '\'`, "%"+route+"%")
	}
	if method := strings.TrimSpace(query.Method); method != "" {
		db = db.Where("method = ?", strings.ToUpper(method))
	}
	if query.Status != nil {
		db = db.Where("status = ?", *query.Status)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.PlayerRequestLog{}
	err := db.Order("requested_at DESC, id DESC").
		Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).
		Find(&rows).Error
	return rows, total, err
}
