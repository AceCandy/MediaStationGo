package service

import (
	"context"
	"errors"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
)

type catalogRankKey struct{}

// RefreshRank 与定时榜单任务共享互斥、取消和执行记录，只刷新指定榜。
func (s *HongGuoService) RefreshRank(ctx context.Context, key string) error {
	if !hongguo.ValidRank(key) {
		return errors.New("红果榜单无效")
	}
	ctx = context.WithValue(ctx, catalogRankKey{}, key)
	return s.Run(context.WithValue(ctx, schedulerManualRunKey{}, true), TaskKindHongGuoRank, "")
}

func (s *HuangGuoAIService) RefreshRank(ctx context.Context, key string) error {
	if !huangguoai.ValidRank(key) {
		return errors.New("黄果 AI 榜单无效")
	}
	ctx = context.WithValue(ctx, catalogRankKey{}, key)
	return s.Run(context.WithValue(ctx, schedulerManualRunKey{}, true), TaskKindHuangGuoAIRank, "")
}
