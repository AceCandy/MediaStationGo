package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var errHongGuoDownloadSource = errors.New("下载来源不可用")
var errHongGuoAwaitVerification = errors.New("等待校验")

const maxHongGuoSourceTries = 9 // 三个来源各最多尝试三轮，跨校验阶段共用预算。

func nextHongGuoDownloadSource(priority, previous string, tries int) string {
	sources := hongguo.DownloadSources(priority)
	if tries > 0 {
		for i, source := range sources {
			if source == previous {
				return sources[(i+1)%len(sources)]
			}
		}
	}
	return sources[0]
}

// 只有源数据/网络错误触发换源；本地文件和工具错误保持原错误。
func downloadMediaCommandError(err error, message string) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		// ponytail: 仅识别明确媒体损坏诊断；未知工具错误不换源，新增协议时补充对应诊断测试。
		for _, invalid := range []string{"Invalid data found when processing input", "moov atom not found", "Error while decoding", "Invalid NAL unit", "corrupt decoded frame"} {
			if strings.Contains(string(exit.Stderr), invalid) {
				return fmt.Errorf("%w: %s", errHongGuoDownloadSource, message)
			}
		}
	}
	return errors.New(message)
}

func (s *HongGuoDownloadService) run(parent context.Context, row model.HongGuoDownload) {
	ctx, cancel := context.WithTimeout(parent, 90*time.Minute)
	defer cancel()
	var downloaded, total atomic.Int64
	downloaded.Store(row.Bytes)
	total.Store(row.TotalBytes)
	finished := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-finished:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				enabled, err := s.catalog.Enabled(ctx)
				if err != nil || !enabled {
					cancel()
					return
				}
				err = s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"lease_until": time.Now().Add(time.Minute), "bytes": downloaded.Load(), "total_bytes": total.Load()})
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	task := s.tasks.StartTriggered(TaskKindHongGuoDownload, TaskTriggerManual, fmt.Sprintf("红果下载：%s E%03d", row.Title, row.Episode), TaskUpdate{Stage: row.Status, DestPath: row.RelativePath})
	var err error
	if task == nil {
		err = errors.New("下载执行记录创建失败")
	} else {
		err = fmt.Errorf("%w: 已达到来源重试上限", errHongGuoDownloadSource)
		for row.SourceTries < maxHongGuoSourceTries || row.RawSize > 0 || row.SHA256 != "" {
			cfg, configErr := s.Config(ctx)
			if configErr != nil {
				err = configErr
				break
			}
			source := nextHongGuoDownloadSource(cfg.Priority, row.Source, row.SourceTries)
			verification := row.RawSize > 0 || row.SHA256 != ""
			err = s.executeDownload(ctx, &row, &downloaded, &total, task, source)
			if ctx.Err() == nil && errors.Is(err, errHongGuoDownloadSource) {
				if row.SourceErrors == nil {
					row.SourceErrors = make(map[string]string)
				}
				row.SourceErrors[row.Source] = sanitizeTaskLogError(err).Error()
				encoded, _ := json.Marshal(row.SourceErrors)
				if saveErr := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"source_errors": string(encoded)}); saveErr != nil {
					err = saveErr
				}
			}
			if err == nil || ctx.Err() != nil || row.SHA256 != "" || !errors.Is(err, errHongGuoDownloadSource) {
				break
			}
			row.RawSize = 0
			if verification {
				break
			} // 需要重下时交还调度器，重新取得传输名额。
			if row.SourceTries < maxHongGuoSourceTries {
				task.Update(TaskUpdate{Message: "下载失败，准备重试", Details: []string{sanitizeTaskLogError(err).Error()}})
				select {
				case <-ctx.Done():
				case <-time.After(2 * time.Second):
				}
			}
		}
	}
	close(finished)
	<-joined
	if errors.Is(err, errHongGuoDownloadRemoved) {
		task.Finish(nil, TaskUpdate{Stage: "removed", Message: errHongGuoDownloadRemoved.Error()})
		s.Wake()
		return
	}
	if errors.Is(err, errHongGuoAwaitVerification) {
		err = s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"status": "waiting_verify", "lease_token": "", "lease_until": nil, "bytes": downloaded.Load(), "total_bytes": total.Load()})
		if err == nil {
			s.Wake()
			task.Finish(nil, TaskUpdate{Stage: "waiting_verify", Message: "下载完成，等待独立校验"})
			return
		}
	}
	if err != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		state := "failed"
		message := sanitizeTaskLogError(err).Error()
		if errors.Is(err, errHongGuoDownloadSource) && row.SourceTries < maxHongGuoSourceTries {
			state = "queued"
			message = "来源校验失败，等待换源重下"
		}
		if parent.Err() != nil {
			state = "queued"
			message = "服务停止，等待恢复"
			if row.RawSize == 0 && row.SHA256 == "" && row.SourceTries > 0 {
				row.SourceTries--
			}
		} else if ctx.Err() != nil {
			message = "下载已中断或超时"
		}
		writeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.repo.HongGuo.UpdateHongGuoDownload(writeCtx, row.ID, row.LeaseToken, map[string]any{"status": state, "error": message, "raw_size": row.RawSize, "source_tries": row.SourceTries, "lease_token": "", "lease_until": nil})
		stop()
		s.Wake()
	}
	if task != nil {
		task.Finish(err, TaskUpdate{Message: map[bool]string{true: "文件已完成并发布，等待外部备份", false: "下载未完成"}[err == nil]})
	}
}

func (s *HongGuoDownloadService) executeDownload(ctx context.Context, row *model.HongGuoDownload, downloaded, total *atomic.Int64, task *TaskHandle, source string) error {
	root, err := os.OpenRoot(row.Root)
	if err != nil {
		return errors.New("下载存储目录不可访问")
	}
	defer root.Close()
	target := filepath.Join("completed", row.RelativePath)
	if row.SHA256 != "" {
		if _, err := root.Lstat(target); err == nil {
			if err := downloadFileMatches(root, target, row.SHA256, row.VerifiedSize); err != nil {
				return err
			}
			return s.publishDownload(ctx, *row, root, target)
		} else if !os.IsNotExist(err) {
			return errors.New("无法检查已有输出文件")
		}
		if row.StagingPath != "" {
			if _, err := root.Stat(row.StagingPath); err == nil {
				if err := downloadFileMatches(root, row.StagingPath, row.SHA256, row.VerifiedSize); err != nil {
					return err
				}
				return s.publishDownload(ctx, *row, root, target)
			}
		}
		row.SHA256 = ""
		row.VerifiedSize = 0
		row.StagingPath = ""
		row.RawSize = 0
		if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"sha256": "", "verified_size": 0, "raw_size": 0, "staging_path": ""}); err != nil {
			return err
		}
		return fmt.Errorf("%w: 校验产物缺失，需要重新下载", errHongGuoDownloadSource)
	}
	if row.RawSize > 0 {
		return s.verifyDownloaded(ctx, row, root, task)
	}
	if _, err := root.Lstat(target); err == nil {
		return errors.New("目标文件已存在且无本任务完成凭据，保留原文件")
	} else if !os.IsNotExist(err) {
		return errors.New("目标路径不可访问")
	}
	if err := root.MkdirAll("downloading", 0o750); err != nil {
		return errors.New("临时目录不可写")
	}
	if row.StagingPath != "" {
		_ = root.Remove(filepath.Join(filepath.Dir(row.StagingPath), "source.bin"))
		_ = root.Remove(row.StagingPath)
		_ = root.Remove(filepath.Dir(row.StagingPath))
	}
	stage := filepath.Join("downloading", row.ID+"-"+row.LeaseToken)
	if err := root.MkdirAll(stage, 0o700); err != nil {
		return errors.New("临时任务目录不可写")
	}
	rawRel, readyRel := filepath.Join(stage, "source.bin"), filepath.Join(stage, "ready.mp4")
	defer func() {
		if row.RawSize == 0 {
			_ = root.Remove(rawRel)
		}
		if row.RawSize == 0 && row.SHA256 == "" {
			_ = root.Remove(readyRel)
		}
		_ = root.Remove(stage)
	}()
	row.Source, row.SourceTries = source, row.SourceTries+1
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"staging_path": readyRel, "status": "downloading", "source": source, "source_tries": row.SourceTries, "quality": 0, "width": 0, "height": 0, "codec": ""}); err != nil {
		return err
	}
	row.StagingPath = readyRel
	task.Update(TaskUpdate{Stage: "downloading", Message: "使用" + hongguo.DownloadSourceName(source) + "下载"})
	// 红果按 S01 集号定位；仅新传输刷新 ID，暂存文件恢复必须沿用原视频的密钥。
	work, err := s.latestDownloadDetail(ctx, *row)
	if err != nil {
		return err
	}
	if row.Episode < 1 || row.Episode > len(work.VideoIDs) || !hongguo.ValidID(work.VideoIDs[row.Episode-1]) {
		return errors.New("上游当前分集列表中没有该集的有效视频 ID")
	}
	row.VideoID = work.VideoIDs[row.Episode-1]
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"video_id": row.VideoID}); err != nil {
		return err
	}
	media, err := s.client.ResolveDownloadSource(ctx, row.SourceID, row.VideoID, source)
	if err != nil {
		return fmt.Errorf("%w: %v", errHongGuoDownloadSource, err)
	}
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"quality": media.Quality, "width": media.Width, "height": media.Height, "codec": media.Codec}); err != nil {
		return err
	}
	row.Quality, row.Width, row.Height, row.Codec = media.Quality, media.Width, media.Height, media.Codec
	resp, err := hongguo.DownloadRequest(ctx, s.http, media)
	if err != nil {
		return fmt.Errorf("%w: %v", errHongGuoDownloadSource, err)
	}
	defer resp.Body.Close()
	total.Store(resp.ContentLength)
	downloaded.Store(0)
	out, err := root.OpenFile(rawRel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("无法写入临时视频")
	}
	n, copyErr := io.Copy(out, &downloadCountingReader{reader: resp.Body, count: downloaded})
	syncErr := out.Sync()
	closeErr := out.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var pathErr *os.PathError
	if syncErr != nil || closeErr != nil || errors.As(copyErr, &pathErr) {
		return errors.New("视频下载或磁盘写入失败")
	}
	if copyErr != nil {
		return fmt.Errorf("%w: 视频传输中断", errHongGuoDownloadSource)
	}
	if n == 0 || (resp.ContentLength >= 0 && n != resp.ContentLength) {
		return fmt.Errorf("%w: 视频响应长度不完整", errHongGuoDownloadSource)
	}
	if err := syncDownloadStage(root, stage); err != nil {
		return err
	}
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"raw_size": n, "duration": media.Duration, "encrypted": len(media.Key) > 0}); err != nil {
		return err
	}
	row.RawSize, row.Duration, row.Encrypted = n, media.Duration, len(media.Key) > 0
	return errHongGuoAwaitVerification
}

// verifyDownloaded 从持久化原始文件恢复；每次租约使用独立处理目录，避免取消后立即重试相互覆盖。
func (s *HongGuoDownloadService) verifyDownloaded(ctx context.Context, row *model.HongGuoDownload, root *os.Root, task *TaskHandle) error {
	cfg, err := s.Config(ctx)
	if err != nil {
		return err
	}
	oldStage := filepath.Dir(row.StagingPath)
	oldRaw := filepath.Join(oldStage, "source.bin")
	info, err := root.Lstat(oldRaw)
	if err != nil && !os.IsNotExist(err) {
		return errors.New("下载暂存文件不可访问")
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() != row.RawSize {
		return fmt.Errorf("%w: 下载暂存文件缺失或长度不符", errHongGuoDownloadSource)
	}
	stage := filepath.Join("downloading", row.ID+"-"+row.LeaseToken)
	if err := root.MkdirAll(stage, 0o700); err != nil {
		return errors.New("校验目录不可写")
	}
	rawRel, readyRel := filepath.Join(stage, "source.bin"), filepath.Join(stage, "ready.mp4")
	if err := root.Link(oldRaw, rawRel); err != nil {
		return errors.New("校验暂存文件不可访问")
	}
	adopted := false
	defer func() {
		if !adopted {
			_ = root.Remove(rawRel)
			_ = root.Remove(stage)
		}
	}()
	if err := syncDownloadStage(root, stage); err != nil {
		return err
	}
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"staging_path": readyRel}); err != nil {
		return err
	}
	row.StagingPath = readyRel
	adopted = true
	_ = root.Remove(oldRaw)
	_ = root.Remove(filepath.Join(oldStage, "ready.mp4"))
	_ = root.Remove(oldStage)
	defer func() {
		if row.RawSize == 0 || row.SHA256 != "" {
			_ = root.Remove(rawRel)
		}
		if row.SHA256 == "" {
			_ = root.Remove(readyRel)
		}
		_ = root.Remove(stage)
	}()
	media := hongguo.DownloadMedia{Duration: row.Duration}
	if row.Encrypted {
		resolved, err := s.client.ResolveDownloadSource(ctx, row.SourceID, row.VideoID, row.Source)
		if err != nil {
			return fmt.Errorf("%w: 无法恢复媒体解密信息：%v", errHongGuoDownloadSource, err)
		}
		if len(resolved.Key) == 0 {
			return fmt.Errorf("%w: 媒体解密信息缺失", errHongGuoDownloadSource)
		}
		media.Key = resolved.Key
	}
	task.Update(TaskUpdate{Stage: "verifying", Message: "下载完成，正在校验音视频完整性"})
	rawPath, readyPath := filepath.Join(row.Root, rawRel), filepath.Join(row.Root, readyRel)
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-f", "mov"}
	if len(media.Key) > 0 {
		args = append(args, "-decryption_key", hex.EncodeToString(media.Key))
	}
	args = append(args, "-i", rawPath, "-map", "0:v:0", "-map", "0:a:0?", "-c", "copy", "-movflags", "+faststart", "-y", readyPath)
	if _, err := exec.CommandContext(ctx, "ffmpeg", args...).Output(); err != nil {
		return downloadMediaCommandError(err, "媒体处理失败：需可用 FFmpeg 和支持的完整 MP4 媒体")
	}
	if err := verifyHongGuoDownload(ctx, readyPath, media.Duration, cfg.FullVerification, cfg.HardwareVerification, task, &media); err != nil {
		return err
	}
	row.Width, row.Height, row.Codec = media.Width, media.Height, media.Codec
	row.Quality = min(media.Width, media.Height)
	file, err := root.Open(readyRel)
	if err != nil {
		return errors.New("无法读取校验产物")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	syncErr := file.Sync()
	file.Close()
	if err != nil || syncErr != nil {
		return errors.New("视频校验和计算失败")
	}
	row.SHA256 = hex.EncodeToString(hash.Sum(nil))
	row.VerifiedSize = size
	row.StagingPath = readyRel
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"status": "publishing", "sha256": row.SHA256, "verified_size": size, "staging_path": readyRel, "quality": row.Quality, "width": row.Width, "height": row.Height, "codec": row.Codec}); err != nil {
		row.SHA256 = ""
		return err
	}
	return s.publishDownload(ctx, *row, root, filepath.Join("completed", row.RelativePath))
}

// 同步任务目录及其父目录，保证数据库交接前新建目录和文件名均已落盘。
func syncDownloadStage(root *os.Root, stage string) error {
	for _, path := range []string{stage, filepath.Dir(stage)} {
		dir, err := root.Open(path)
		if err != nil {
			return errors.New("临时下载目录不可访问")
		}
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return errors.New("临时下载目录无法持久化")
		}
	}
	return nil
}

type downloadCountingReader struct {
	reader io.Reader
	count  *atomic.Int64
}

func (r *downloadCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count.Add(int64(n))
	return n, err
}

func verifyHongGuoDownload(ctx context.Context, path string, expected float64, full, hardware bool, task *TaskHandle, media *hongguo.DownloadMedia) error {
	data, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=duration:stream=codec_type,codec_name,width,height", "-of", "json", path).Output()
	if err != nil {
		return downloadMediaCommandError(err, "视频探测失败，请确认 FFprobe 已安装且文件有效")
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type   string `json:"codec_type"`
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return fmt.Errorf("%w: 视频探测结果无效", errHongGuoDownloadSource)
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	video := false
	for _, stream := range probe.Streams {
		if stream.Type == "video" && !video && media != nil {
			media.Width, media.Height, media.Codec = stream.Width, stream.Height, stream.Codec
		}
		video = video || stream.Type == "video"
	}
	if err != nil || !video || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return fmt.Errorf("%w: 视频缺少有效时长或画面轨道", errHongGuoDownloadSource)
	}
	if expected > 0 && math.Abs(duration-expected) > math.Max(2, expected*0.02) {
		return fmt.Errorf("%w: 视频时长与来源不一致，未发布", errHongGuoDownloadSource)
	}
	if full {
		return decodeHongGuoDownload(ctx, path, hardware, task)
	}
	return nil
}

// 硬解仅用于全流解码检查；失败时以软件校验为准，取消不能触发额外解码。
func decodeHongGuoDownload(ctx context.Context, path string, hardware bool, task *TaskHandle) error {
	base := []string{"-nostdin", "-v", "error", "-xerror", "-err_detect", "explode"}
	input := []string{"-protocol_whitelist", "file,pipe", "-i", path, "-map", "0:v:0", "-map", "0:a:0?", "-f", "null", "-"}
	if hardware {
		task.Update(TaskUpdate{Stage: "verifying", Message: "正在使用 VAAPI 核显解码校验"})
		args := append(append([]string{}, base...), "-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD128", "-hwaccel_output_format", "vaapi")
		_, err := exec.CommandContext(ctx, "ffmpeg", append(args, input...)...).Output()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		task.Update(TaskUpdate{Stage: "verifying", Message: "硬件解码不可用或校验失败，回退软件完整校验"})
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err := exec.CommandContext(ctx, "ffmpeg", append(base, input...)...).Output(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return downloadMediaCommandError(err, "音视频解码校验失败，未发布")
	}
	return nil
}

func downloadFileMatches(root *os.Root, path, digest string, size int64) error {
	info, err := root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("已有文件与任务完成凭据不符，未覆盖")
	}
	f, err := root.Open(path)
	if err != nil {
		return errors.New("无法校验已有文件")
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.New("已有文件校验失败")
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("已有文件校验和不符，未覆盖")
	}
	return nil
}

func (s *HongGuoDownloadService) publishDownload(ctx context.Context, row model.HongGuoDownload, root *os.Root, target string) error {
	if err := s.repo.HongGuo.UpdateHongGuoDownload(ctx, row.ID, row.LeaseToken, map[string]any{"status": "publishing"}); err != nil {
		return err
	}
	err := s.repo.HongGuo.PublishHongGuoDownload(ctx, row, func() error {
		if _, err := root.Lstat(target); err == nil {
			return downloadFileMatches(root, target, row.SHA256, row.VerifiedSize)
		} else if !os.IsNotExist(err) {
			return errors.New("无法检查输出文件")
		}
		if err := root.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return errors.New("输出目录不可写")
		}
		// Link 原子地发布完整文件且绝不覆盖；随后移除临时目录中的名字。
		if err := root.Link(row.StagingPath, target); err != nil {
			return errors.New("发布失败：目标已存在或目录不支持同文件系统原子发布")
		}
		dir, err := root.Open(filepath.Dir(target))
		if err != nil {
			return errors.New("输出目录无法持久化")
		}
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return errors.New("输出目录持久化失败，等待恢复核对")
		}
		return nil
	})
	if err == nil && row.StagingPath != "" {
		_ = root.Remove(row.StagingPath)
		_ = root.Remove(filepath.Dir(row.StagingPath))
	}
	return err
}
