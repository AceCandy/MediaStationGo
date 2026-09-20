package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// refreshAlbum 独立保存补充结果，失败不回滚网页详情或清空已有关系。
func (s *HongGuoService) refreshAlbum(ctx context.Context, sourceID string) error {
	// 详情附带查询与独立补充任务共用请求间隔，取消立即生效。
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	album, err := s.client.Album(ctx, sourceID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		if saveErr := s.repo.HongGuo.SaveAlbum(ctx, sourceID, album); saveErr != nil {
			return errors.Join(errHongGuoCheckpoint, saveErr)
		}
		return nil
	}
	if saveErr := s.repo.HongGuo.RetryAlbum(ctx, sourceID, time.Now()); saveErr != nil {
		return errors.Join(errHongGuoCheckpoint, saveErr)
	}
	return err
}

// backfillAlbums 按作品持久化状态续跑；成功空关系也完成检查，失败项下一轮重试。
func (s *HongGuoService) backfillAlbums(ctx context.Context, report func(string, error)) error {
	cutoff, after, failures := time.Now(), "", 0
	for {
		rows, err := s.repo.HongGuo.PendingAlbums(ctx, after, cutoff)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			err := s.refreshAlbum(ctx, row.SourceID)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, errHongGuoCheckpoint) {
				return err
			}
			if err != nil {
				failures++
			}
			report("官方合集 "+row.SourceID, err)
			after = row.SourceID
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d 个官方合集查询失败，已安排重试", failures)
	}
	return nil
}
