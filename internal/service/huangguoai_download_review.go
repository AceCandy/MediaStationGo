package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// openHuangGuoAIReview 只打开任务独占的完整候选，不允许符号链接替换产物。
func openHuangGuoAIReview(row model.HuangGuoAIDownload) (*os.Root, *os.File, error) {
	if row.Status != "pending_review" || row.RawSize <= 0 || row.SHA256 == "" || row.VerifiedSize != row.RawSize || !validHuangGuoAIStage(row.StagingPath, row.ID) {
		return nil, nil, errors.New("任务没有待确认文件")
	}
	root, err := os.OpenRoot(row.Root)
	if err != nil {
		return nil, nil, errors.New("候选存储不可访问")
	}
	fail := func() (*os.Root, *os.File, error) {
		root.Close()
		return nil, nil, errors.New("候选文件缺失或已变更")
	}
	dir, err := root.Lstat(filepath.Dir(row.StagingPath))
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return fail()
	}
	info, err := root.Lstat(row.StagingPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() != row.RawSize {
		return fail()
	}
	file, err := root.Open(row.StagingPath)
	if err != nil {
		return fail()
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() != row.RawSize {
		file.Close()
		return fail()
	}
	return root, file, nil
}

// ReviewFile 返回已打开的私有候选；调用者必须关闭，不创建公开媒体记录。
func (s *HuangGuoAIDownloadService) ReviewFile(ctx context.Context, id, token string) (*os.File, error) {
	var row model.HuangGuoAIDownload
	if err := s.repo.DB.WithContext(ctx).First(&row, "id=?", id).Error; err != nil {
		return nil, err
	}
	if token == "" || row.ReviewToken != token {
		return nil, errors.New("候选版本已变更")
	}
	root, file, err := openHuangGuoAIReview(row)
	if err != nil {
		return nil, err
	}
	root.Close()
	return file, nil
}

// ConfirmReview 只记录当前候选的人工决定，完整摘要校验由发布 worker 执行。
func (s *HuangGuoAIDownloadService) ConfirmReview(ctx context.Context, id, token, userID string) error {
	if userID == "" {
		return errors.New("确认身份无效")
	}
	var sourceID string
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.HuangGuoAIDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id=?", id).Error; err != nil {
			return err
		}
		if token == "" || row.ReviewToken != token {
			return errors.New("候选版本已变更")
		}
		root, file, err := openHuangGuoAIReview(row)
		if err != nil {
			return err
		}
		defer root.Close()
		defer file.Close()
		sourceID = row.SourceID
		return tx.Model(&row).Updates(map[string]any{"status": "waiting_verify", "confirmed_by": userID, "confirmed_at": time.Now(), "lease_token": "", "lease_until": nil}).Error
	})
	if err == nil {
		s.refreshWorkTask(ctx, sourceID)
		s.Wake()
	}
	return err
}
