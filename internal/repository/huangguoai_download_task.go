package repository

import (
	"context"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
)

// HuangGuoAIDownloadTaskID 按体系和作品隔离摘要，重试及恢复保持同一身份。
func HuangGuoAIDownloadTaskID(sourceID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("huangguoai_download:"+sourceID)).String()
}

// RefreshHuangGuoAIDownloadTask 只汇总黄果分集队列，不参与下载执行。
func (r *HuangGuoAIRepository) RefreshHuangGuoAIDownloadTask(ctx context.Context, sourceID string) (*model.TaskExecution, error) {
	return refreshDownloadWorkTask(ctx, r.db, sourceID, model.TaskSystemHuangGuoAI)
}
