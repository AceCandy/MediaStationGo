package service

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// Search 只读取官网和本地状态；补录仍使用管理员详情刷新入口。
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
	return s.repo.HongGuo.SearchResults(ctx, works)
}
