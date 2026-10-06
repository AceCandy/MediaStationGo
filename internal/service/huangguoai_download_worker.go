package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/google/uuid"
)

var errHuangGuoAIWaitingVerify = errors.New("黄果 AI 等待独立校验")

func validHuangGuoAIStage(path, id string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if !filepath.IsLocal(path) || len(parts) != 3 || parts[0] != "downloading" || parts[2] != "ready.mp4" {
		return false
	}
	token, ok := strings.CutPrefix(parts[1], id+"-")
	_, err := uuid.Parse(token)
	return ok && err == nil
}
func (s *HuangGuoAIDownloadService) run(parent context.Context, row model.HuangGuoAIDownload) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Hour)
	defer cancel()
	var bytes atomic.Int64
	bytes.Store(row.Bytes)
	finished := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-finished:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				enabled, e := s.catalog.Enabled(ctx)
				if e != nil || !enabled {
					cancel()
					return
				}
				e = s.repo.HuangGuoAI.UpdateHuangGuoAIDownload(ctx, row.ID, row.LeaseToken, map[string]any{"lease_until": time.Now().Add(time.Minute), "bytes": bytes.Load()})
				if e != nil {
					cancel()
					return
				}
			}
		}
	}()
	// Source titles do not enter task logs; IDs are enough to diagnose queue failures.
	task := s.tasks.StartTriggered(TaskKindHuangGuoAIDownload, TaskTriggerManual, "黄果 AI 下载 "+row.SourceID, TaskUpdate{Stage: row.Status})
	var err error
	if task == nil {
		err = errors.New("下载任务记录不可用")
	} else {
		err = s.execute(ctx, &row, &bytes, task)
	}
	close(finished)
	<-joined
	if errors.Is(err, errHuangGuoAIWaitingVerify) {
		task.Finish(nil, TaskUpdate{Stage: "waiting_verify", Message: "传输完成，等待独立校验"})
		s.Wake()
		return
	}
	if err != nil {
		status := "failed"
		if parent.Err() != nil {
			status = "queued"
		}
		writeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.repo.HuangGuoAI.UpdateHuangGuoAIDownload(writeCtx, row.ID, row.LeaseToken, map[string]any{"status": status, "error": "下载未完成，详情见任务状态", "lease_token": "", "lease_until": nil, "raw_size": row.RawSize, "sha256": row.SHA256, "verified_size": row.VerifiedSize, "staging_path": row.StagingPath})
		stop()
	}
	safeErr := err
	if err != nil {
		safeErr = errors.New("黄果 AI 下载未完成")
	}
	if parent.Err() != nil {
		safeErr = context.Canceled
	}
	if task == nil {
		return
	}
	task.Finish(safeErr, TaskUpdate{Message: map[bool]string{true: "文件已完成并发布，等待整理入库", false: "文件未发布，等待重试"}[err == nil]})
	s.Wake()
}

func (s *HuangGuoAIDownloadService) execute(ctx context.Context, row *model.HuangGuoAIDownload, bytes *atomic.Int64, task *TaskHandle) error {
	root, err := os.OpenRoot(row.Root)
	if err != nil {
		return errors.New("下载存储目录不可访问")
	}
	defer root.Close()
	if !filepath.IsLocal(row.RelativePath) {
		return errors.New("下载输出路径无效")
	}
	target := filepath.Join("completed", row.RelativePath)
	if row.SHA256 != "" {
		if err = downloadFileMatches(root, target, row.SHA256, row.VerifiedSize); err == nil {
			return s.publish(ctx, *row, root, target)
		}
		if !validHuangGuoAIStage(row.StagingPath, row.ID) {
			return errors.New("恢复暂存路径无效")
		}
		if err = downloadFileMatches(root, row.StagingPath, row.SHA256, row.VerifiedSize); err != nil {
			if _, e := root.Lstat(target); !os.IsNotExist(e) {
				return errors.New("完成文件冲突，需人工检查")
			}
			row.SHA256, row.RawSize, row.VerifiedSize = "", 0, 0
			return errors.New("已校验暂存文件缺失，重试将重新下载")
		}
		return s.publish(ctx, *row, root, target)
	}
	if row.RawSize > 0 {
		if !validHuangGuoAIStage(row.StagingPath, row.ID) {
			return errors.New("校验暂存路径无效")
		}
		info, e := root.Lstat(row.StagingPath)
		if e != nil || !info.Mode().IsRegular() || info.Size() != row.RawSize {
			row.RawSize = 0
			row.StagingPath = ""
			return errors.New("待校验文件缺失或长度不符")
		}
		task.Update(TaskUpdate{Stage: "verifying", Message: "正在完整解码并核对时长"})
		if err = verifyHongGuoDownload(ctx, filepath.Join(row.Root, row.StagingPath), row.Duration, true, false, task, nil); err != nil {
			if ctx.Err() == nil {
				_ = root.Remove(row.StagingPath)
				_ = root.Remove(filepath.Dir(row.StagingPath))
				row.RawSize = 0
				row.StagingPath = ""
			}
			return err
		}
		file, e := root.Open(row.StagingPath)
		if e != nil {
			return errors.New("校验文件不可读取")
		}
		digest := sha256.New()
		size, e := io.Copy(digest, file)
		file.Close()
		if e != nil {
			return errors.New("文件摘要计算失败")
		}
		row.SHA256 = hex.EncodeToString(digest.Sum(nil))
		row.VerifiedSize = size
		if err = s.repo.HuangGuoAI.UpdateHuangGuoAIDownload(ctx, row.ID, row.LeaseToken, map[string]any{"status": "publishing", "sha256": row.SHA256, "verified_size": size}); err != nil {
			return err
		}
		return s.publish(ctx, *row, root, target)
	}
	oldStage := row.StagingPath
	stage := filepath.Join("downloading", row.ID+"-"+row.LeaseToken)
	if err = root.MkdirAll(stage, 0700); err != nil {
		return errors.New("下载暂存目录不可写")
	}
	retain := false
	defer func() {
		if !retain {
			_ = root.RemoveAll(stage)
		}
	}()
	ready := filepath.Join(stage, "ready.mp4")
	if err = s.repo.HuangGuoAI.UpdateHuangGuoAIDownload(ctx, row.ID, row.LeaseToken, map[string]any{"staging_path": ready}); err != nil {
		return err
	}
	row.StagingPath = ready
	if validHuangGuoAIStage(oldStage, row.ID) && filepath.Dir(oldStage) != stage {
		_ = root.RemoveAll(filepath.Dir(oldStage))
	}
	media, err := s.catalog.client.Resolve(ctx, row.SourceID, row.Episode)
	if err != nil {
		return err
	}
	input, _, err := s.catalog.client.Download(ctx, media, filepath.Join(row.Root, stage), func(n int64) { bytes.Store(n) })
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(row.Root, input)
	if err != nil || !filepath.IsLocal(rel) || filepath.Dir(rel) != stage {
		return errors.New("下载产物路径无效")
	}
	if err = root.Rename(rel, ready); err != nil {
		return errors.New("下载产物无法交接")
	}
	// Transport keys and segment files are removed before handing the plaintext file to the verifier.
	dir, err := root.Open(stage)
	if err != nil {
		return errors.New("暂存目录不可读")
	}
	names, err := dir.Readdirnames(-1)
	dir.Close()
	if err != nil {
		return errors.New("暂存目录不可读")
	}
	for _, name := range names {
		if name != "ready.mp4" {
			if e := root.Remove(filepath.Join(stage, name)); e != nil {
				return errors.New("传输临时文件清理失败")
			}
		}
	}
	if err = syncDownloadStage(root, stage); err != nil {
		return err
	}
	info, err := root.Stat(ready)
	if err != nil || info.Size() <= 0 {
		return errors.New("下载产物为空")
	}
	row.StagingPath = ready
	row.RawSize = info.Size()
	row.Duration = media.ExpectedDuration
	if err = s.repo.HuangGuoAI.UpdateHuangGuoAIDownload(ctx, row.ID, row.LeaseToken, map[string]any{"status": "waiting_verify", "raw_size": row.RawSize, "duration": row.Duration, "staging_path": ready, "lease_token": "", "lease_until": nil, "bytes": row.RawSize, "total_bytes": row.RawSize}); err != nil {
		row.RawSize = 0
		row.StagingPath = ""
		return err
	}
	retain = true
	return errHuangGuoAIWaitingVerify
}

func (s *HuangGuoAIDownloadService) publish(ctx context.Context, row model.HuangGuoAIDownload, root *os.Root, target string) error {
	err := s.repo.HuangGuoAI.PublishHuangGuoAIDownload(ctx, row, func() error {
		if _, e := root.Lstat(target); e == nil {
			return downloadFileMatches(root, target, row.SHA256, row.VerifiedSize)
		} else if !os.IsNotExist(e) {
			return errors.New("输出文件不可检查")
		}
		if !validHuangGuoAIStage(row.StagingPath, row.ID) {
			return errors.New("发布暂存路径无效")
		}
		if e := downloadFileMatches(root, row.StagingPath, row.SHA256, row.VerifiedSize); e != nil {
			return e
		}
		if e := root.MkdirAll(filepath.Dir(target), 0750); e != nil {
			return errors.New("输出目录不可写")
		}
		if e := root.Link(row.StagingPath, target); e != nil {
			return errors.New("发布失败，目标存在或不支持原子发布")
		}
		dir, e := root.Open(filepath.Dir(target))
		if e != nil {
			return errors.New("输出目录不可持久化")
		}
		e = dir.Sync()
		dir.Close()
		if e != nil {
			return errors.New("输出目录持久化失败")
		}
		return nil
	})
	if err == nil && validHuangGuoAIStage(row.StagingPath, row.ID) {
		_ = root.Remove(row.StagingPath)
		_ = root.Remove(filepath.Dir(row.StagingPath))
	}
	return err
}
