package service

import (
	"context"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type PlayerRequestLogItem struct {
	ID          string              `json:"id"`
	RequestedAt time.Time           `json:"requested_at"`
	Method      string              `json:"method"`
	Route       string              `json:"route"`
	Status      int                 `json:"status"`
	DurationMS  int64               `json:"duration_ms"`
	IP          string              `json:"ip"`
	PathParams  map[string][]string `json:"path_params"`
	Headers     map[string][]string `json:"headers"`
	Query       map[string][]string `json:"query"`
}

type PlayerRequestLogPage struct {
	Items    []PlayerRequestLogItem `json:"items"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Total    int64                  `json:"total"`
}

type PlayerRequestLogFilter struct {
	Month    time.Time
	Route    string
	Method   string
	Status   *int
	Page     int
	PageSize int
}

// PlayerRequestLogService 负责播放器请求日志的分区准备、持久化和查询。
type PlayerRequestLogService struct {
	repo           *repository.PlayerRequestLogRepository
	sse            *SSEHub
	partitionMu    sync.Mutex
	partitionMonth string
}

func NewPlayerRequestLogService(repo *repository.PlayerRequestLogRepository, sse *SSEHub) *PlayerRequestLogService {
	return &PlayerRequestLogService{repo: repo, sse: sse}
}

func (s *PlayerRequestLogService) Record(ctx context.Context, row *model.PlayerRequestLog) error {
	if err := s.ensurePartitions(row.RequestedAt); err != nil {
		return err
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return err
	}
	if s.sse != nil {
		s.sse.Broadcast(EventTypePlayerRequestLog, struct{}{})
	}
	return nil
}

func (s *PlayerRequestLogService) ensurePartitions(at time.Time) error {
	month := at.UTC().Format("2006-01")
	s.partitionMu.Lock()
	defer s.partitionMu.Unlock()
	if s.partitionMonth == month {
		return nil
	}
	if err := database.EnsurePlayerRequestLogPartitions(s.repo.DB(), at); err != nil {
		return err
	}
	s.partitionMonth = month
	return nil
}

func (s *PlayerRequestLogService) List(ctx context.Context, filter PlayerRequestLogFilter) (PlayerRequestLogPage, error) {
	start := time.Date(filter.Month.UTC().Year(), filter.Month.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	rows, total, err := s.repo.List(ctx, repository.PlayerRequestLogQuery{
		Start: start, End: start.AddDate(0, 1, 0), Route: filter.Route,
		Method: filter.Method, Status: filter.Status, Page: filter.Page, PageSize: filter.PageSize,
	})
	if err != nil {
		return PlayerRequestLogPage{}, err
	}
	items := make([]PlayerRequestLogItem, len(rows))
	for i, row := range rows {
		items[i] = PlayerRequestLogItem{
			ID: row.ID, RequestedAt: row.RequestedAt, Method: row.Method, Route: row.Route,
			Status: row.Status, DurationMS: row.DurationMS, IP: row.IP,
			PathParams: row.PathParams, Headers: row.Headers, Query: row.Query,
		}
	}
	return PlayerRequestLogPage{Items: items, Page: filter.Page, PageSize: filter.PageSize, Total: total}, nil
}
