package huangguoai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// HLSDurationMismatchError 标记完整合并产物仅因时长异常需要人工确认。
type HLSDurationMismatchError struct{}

func (HLSDurationMismatchError) Error() string {
	return "视频时长与来源不一致，未发布"
}

// 合并恢复只使用已下载的资源；保持全部音轨，独立发布校验仍必须通过。
func mergeHLS(ctx context.Context, input, dir string, expected float64) (string, error) {
	transcodeAudio, correctTimestamps := false, false
	var sourceOffsets []float64
	var candidate string
	probeLimit := "30000000"
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		output := filepath.Join(dir, "output.mp4")
		if attempt > 0 {
			output = filepath.Join(dir, fmt.Sprintf("output-recovery-%d.mp4", attempt))
		}
		if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("HLS 合并暂存目标已存在或不可访问")
		}
		// 部分源的画面晚于音频出现，需要扩大输入探测范围才能取得视频尺寸。
		args := []string{"-nostdin", "-v", "error", "-protocol_whitelist", "file,crypto", "-allowed_extensions", "ALL", "-probesize", probeLimit, "-analyzeduration", probeLimit}
		if correctTimestamps {
			args = append(args, "-dts_delta_threshold", "1")
		}
		if transcodeAudio {
			if sourceOffsets == nil {
				var err error
				sourceOffsets, err = hlsAudioOffsets(ctx, input, probeLimit)
				if err != nil {
					return "", err
				}
			}
			// 未知采样率时自动起点调整可能把音画分开，按共同输入起点归零。
			// copyts 会关闭 DTS 跳变修正，因此两个恢复同时需要时不能保留它。
			if !correctTimestamps {
				args = append(args, "-copyts", "-start_at_zero")
			}
			args = append(args, "-xerror")
		}
		args = append(args, "-i", input, "-map", "0:v:0", "-map", "0:a?")
		if transcodeAudio {
			args = append(args, "-c:v", "copy", "-c:a", "aac")
		} else {
			args = append(args, "-c", "copy")
		}
		args = append(args, "-movflags", "+faststart", "-n", output)
		if _, err := exec.CommandContext(ctx, "ffmpeg", args...).Output(); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if probeLimit == "30000000" && hlsVideoDimensionsMissing(err) {
				probeLimit = "100000000"
				sourceOffsets = nil
				continue
			}
			if !transcodeAudio && hlsAudioParametersMissing(err) {
				transcodeAudio = true
				continue
			}
			return "", hlsMergeError(err)
		}
		duration, err := hlsVideoDuration(ctx, output)
		if err != nil {
			return "", err
		}
		if transcodeAudio {
			offsets, err := hlsAudioOffsets(ctx, output, probeLimit)
			if err != nil {
				return "", err
			}
			if len(offsets) != len(sourceOffsets) {
				return "", errors.New("HLS 音轨恢复数量不符，未发布")
			}
			for i, offset := range offsets {
				if math.Abs(offset-sourceOffsets[i]) > 0.25 {
					return "", errors.New("HLS 音画起点校验失败，未发布")
				}
			}
		}
		if math.Abs(duration-expected) <= math.Max(2, expected*0.02) {
			return output, nil
		}
		// 人工确认优先保留原时间轴，避免跳变恢复缩短已下载内容。
		if candidate == "" {
			candidate = output
		}
		// 只有时长校验失败才收紧 TS 跳变阈值，保留正常源的原始时间轴。
		if !correctTimestamps {
			correctTimestamps = true
			continue
		}
		return candidate, HLSDurationMismatchError{}
	}
	return "", errors.New("HLS 合并恢复失败，未发布")
}

// 比较每条音轨与视频的相对起点，保留原有前置音频，拒绝转码引入的错位。
func hlsAudioOffsets(ctx context.Context, path, probeLimit string) ([]float64, error) {
	args := []string{"-v", "error", "-protocol_whitelist", "file,crypto"}
	if strings.HasSuffix(path, ".m3u8") {
		args = append(args, "-allowed_extensions", "ALL", "-probesize", probeLimit, "-analyzeduration", probeLimit)
	}
	args = append(args, "-show_entries", "stream=codec_type,start_time", "-of", "json", path)
	data, err := exec.CommandContext(ctx, "ffprobe", args...).Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var probe struct {
		Streams []struct {
			Type  string `json:"codec_type"`
			Start string `json:"start_time"`
		} `json:"streams"`
	}
	if err != nil || json.Unmarshal(data, &probe) != nil {
		return nil, errors.New("HLS 音画起点探测失败，未发布")
	}
	var video float64
	hasVideo := false
	var audio []float64
	for _, stream := range probe.Streams {
		if stream.Type != "audio" && (stream.Type != "video" || hasVideo) {
			continue
		}
		start, err := strconv.ParseFloat(stream.Start, 64)
		if err != nil || math.IsNaN(start) || math.IsInf(start, 0) {
			return nil, errors.New("HLS 音画起点无效，未发布")
		}
		if stream.Type == "video" {
			video, hasVideo = start, true
		} else {
			audio = append(audio, start)
		}
	}
	if !hasVideo || len(audio) == 0 {
		return nil, errors.New("HLS 音轨恢复缺少原始轨道证据，未发布")
	}
	for i := range audio {
		audio[i] -= video
	}
	return audio, nil
}

// 只为探测不足扩大范围，不能把磁盘、权限或损坏输入误判为探测不足。
func hlsVideoDimensionsMissing(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	stderr := string(exit.Stderr)
	return strings.Contains(stderr, "dimensions not set") &&
		!strings.Contains(stderr, "No space left on device") &&
		!strings.Contains(stderr, "Permission denied") &&
		!strings.Contains(stderr, "Invalid data found when processing input")
}

// 只针对可解码但无法直接写入 MP4 的音轨参数错误恢复，不按退出码猜测。
func hlsAudioParametersMissing(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	stderr := string(exit.Stderr)
	return strings.Contains(stderr, "sample rate not set") &&
		!strings.Contains(stderr, "No space left on device") &&
		!strings.Contains(stderr, "Permission denied") &&
		!strings.Contains(stderr, "Invalid data found when processing input")
}

// 使用首条视频轨时长判断是否需要恢复；不得以较长的前置音频代替视频证据。
func hlsVideoDuration(ctx context.Context, path string) (float64, error) {
	data, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-select_streams", "v:0", "-show_entries", "stream=duration", "-of", "json", path).Output()
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, errors.New("HLS 视频时长探测失败")
	}
	var probe struct {
		Streams []struct {
			Duration string `json:"duration"`
		} `json:"streams"`
	}
	if json.Unmarshal(data, &probe) != nil || len(probe.Streams) != 1 {
		return 0, errors.New("视频轨道缺少有效时长，未发布")
	}
	duration, err := strconv.ParseFloat(probe.Streams[0].Duration, 64)
	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0, errors.New("视频轨道缺少有效时长，未发布")
	}
	return duration, nil
}
