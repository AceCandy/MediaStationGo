package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// Search 合并官网与本地状态，登记缺失资料的摘要并唤醒资料刷新任务。
func (s *HongGuoService) Search(ctx context.Context, keyword string) ([]repository.HongGuoListWork, error) {
	enabled, err := s.Enabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrHongGuoDisabled
	}
	works, err := s.client.Search(ctx, keyword)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.HongGuo.SearchResults(ctx, works)
	if err != nil {
		return nil, err
	}
	if err := s.repo.HongGuo.QueueMissingSearchResults(ctx, rows); err != nil {
		return nil, err
	}
	s.requestRefresh(ctx)
	return rows, nil
}
