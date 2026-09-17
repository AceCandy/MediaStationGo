package service

import (
	"context"
	"time"
)

type hongGuoEventRefreshKey struct{}

// requestRefresh 合并发现/搜索的唤醒；真正的待补齐状态仍由业务表持久化。
func (s *HongGuoService) requestRefresh(ctx context.Context) {
	s.mu.Lock()
	if s.closed || ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.refreshRequested = true
	s.mu.Unlock()
	s.startRequestedRefresh()
}

// startRequestedRefresh 先占用刷新锁再启动，避免与手动任务竞争时丢失唤醒或重复启动。
func (s *HongGuoService) startRequestedRefresh() {
	s.mu.Lock()
	if s.closed || !s.refreshRequested || !s.runMu.TryLock() {
		s.mu.Unlock()
		return
	}
	s.refreshRequested = false
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), hongGuoEventRefreshKey{}, true))
	s.autoRefreshCancel = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			s.autoRefreshCancel = nil
			s.mu.Unlock()
			s.runMu.Unlock()
			s.startRequestedRefresh()
		}()
		pending, err := s.repo.HongGuo.PendingDiscoveries(ctx, "", time.Now())
		if ctx.Err() != nil || (err == nil && len(pending) == 0) {
			return
		}
		// 查询失败也走任务执行路径，由既有任务记录报告错误，持久化摘要留待下次重试。
		_ = s.runLocked(ctx, TaskKindHongGuoRefresh, "", "红果资料刷新", &s.cancel)
	}()
}
