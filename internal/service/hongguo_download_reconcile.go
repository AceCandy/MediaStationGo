package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var errHongGuoDownloadRemoved = errors.New("上游变化已核实，下载队列已更新")

// latestDownloadDetail 将下架证据与普通网络故障分开，单次未命中不能删除业务数据。
func (s *HongGuoDownloadService) latestDownloadDetail(ctx context.Context, row model.HongGuoDownload) (hongguo.Work, error) {
	work, err := s.client.Detail(ctx, row.SourceID)
	if errors.Is(err, hongguo.ErrNotFound) {
		work, err = s.client.Detail(ctx, row.SourceID)
		if errors.Is(err, hongguo.ErrNotFound) && row.Title != "" {
			found, searchErr := s.client.Search(ctx, row.Title)
			if searchErr != nil {
				return work, fmt.Errorf("%w: 下架复核失败：%v", errHongGuoDownloadSource, searchErr)
			}
			if !slices.ContainsFunc(found, func(w hongguo.Work) bool { return w.SourceID == row.SourceID }) {
				return work, s.removeUnavailableDownloads(ctx, row, nil)
			}
		}
	}
	if err != nil {
		return work, fmt.Errorf("%w: 获取最新分集列表失败：%v", errHongGuoDownloadSource, err)
	}
	if !completeDownloadEpisodes(work) {
		return work, nil
	}
	var previous []model.HongGuoDownload
	if err := s.repo.DB.WithContext(ctx).Where("source_id = ?", row.SourceID).Order("episode").Find(&previous).Error; err != nil {
		return work, err
	}
	positions := make(map[string]int, len(work.VideoIDs))
	for i, id := range work.VideoIDs {
		if id != "" {
			positions[id] = i + 1
		}
	}
	reordered := slices.ContainsFunc(previous, func(old model.HongGuoDownload) bool {
		position := positions[old.VideoID]
		return position > 0 && position != old.Episode
	})
	if len(previous) == 0 || previous[len(previous)-1].Episode == len(work.VideoIDs) && !slices.Contains(work.VideoIDs, "") && !reordered {
		return work, nil
	}
	confirmed, err := s.client.Detail(ctx, row.SourceID)
	if err != nil {
		return work, fmt.Errorf("%w: 分集变化复核失败：%v", errHongGuoDownloadSource, err)
	}
	if !completeDownloadEpisodes(confirmed) || !slices.Equal(work.VideoIDs, confirmed.VideoIDs) {
		return work, errors.New("上游分集列表尚不稳定，保留原下载任务")
	}
	oldRows := make(map[int]model.HongGuoDownload, len(previous))
	for _, old := range previous {
		oldRows[old.Episode] = old
	}
	for i, id := range confirmed.VideoIDs {
		if id != "" {
			continue
		}
		old, exists := oldRows[i+1]
		if !exists || old.Status == "completed" || old.RawSize > 0 || old.SHA256 != "" {
			continue
		}
		if !hongguo.ValidID(old.VideoID) {
			return work, errors.New("分集列表存在空位但缺少下架证据，保留原下载任务")
		}
		for range 2 {
			if _, err := s.client.ResolveDownloadSource(ctx, row.SourceID, old.VideoID, hongguo.DownloadApp); !errors.Is(err, hongguo.ErrVideoTakenDown) {
				return work, errors.New("分集空位未确认下架，保留原下载任务")
			}
		}
	}
	return work, s.removeUnavailableDownloads(ctx, row, &confirmed)
}

// 只有完结总数、已更新集数和完整视频列表一致，才能据此删除过期尾集。
func completeDownloadEpisodes(work hongguo.Work) bool {
	if !work.Completed || work.TotalEpisodes <= 0 || work.TotalEpisodes > 10000 || work.TotalEpisodes != work.EpisodeCount || work.TotalEpisodes != len(work.VideoIDs) {
		return false
	}
	seen := make(map[string]bool, len(work.VideoIDs))
	for _, id := range work.VideoIDs {
		if id == "" {
			continue // 空位还需原视频的明确下架响应，不能只凭列表缺失删除。
		}
		if !hongguo.ValidID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return len(seen) > 0
}

func (s *HongGuoDownloadService) removeUnavailableDownloads(ctx context.Context, row model.HongGuoDownload, detail *hongguo.Work) error {
	removed, err := s.repo.HongGuo.RemoveUnavailableHongGuoDownloads(ctx, row, detail)
	if err != nil {
		return err
	}
	currentRemoved := false
	for _, old := range removed {
		currentRemoved = currentRemoved || old.ID == row.ID
		// 只清理该租约自己的暂存文件；完成目录和任意外部路径不在清理范围。
		stage := filepath.Dir(old.StagingPath)
		if old.StagingPath == "" || filepath.Dir(stage) != "downloading" {
			continue
		}
		root, err := os.OpenRoot(old.Root)
		if err != nil {
			continue
		}
		_ = root.Remove(filepath.Join(stage, "source.bin"))
		_ = root.Remove(filepath.Join(stage, "ready.mp4"))
		_ = root.Remove(stage)
		root.Close()
	}
	if currentRemoved {
		return errHongGuoDownloadRemoved
	}
	return nil
}
