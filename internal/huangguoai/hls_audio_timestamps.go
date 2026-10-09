package huangguoai

import (
	"bufio"
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// hlsPacketTimeline 保留包计数、连续性和采样时长证据，不缓存完整包列表。
type hlsPacketTimeline struct {
	count                        int
	firstPTS, lastPTS, lastDTS   float64
	duration                     float64
	rollback, gap, discontinuous bool
}

// hlsPacketTimelines 流式读取数字字段；源地址、密钥和 FFprobe 原文不进入错误信息。
func hlsPacketTimelines(ctx context.Context, path, streams, probeLimit string) ([]hlsPacketTimeline, error) {
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := []string{"-v", "error", "-protocol_whitelist", "file,crypto", "-select_streams", streams}
	if strings.HasSuffix(path, ".m3u8") {
		args = append(args, "-allowed_extensions", "ALL", "-probesize", probeLimit, "-analyzeduration", probeLimit)
	}
	args = append(args, "-show_packets", "-show_entries", "packet=stream_index,pts_time,dts_time,duration_time", "-of", "csv=p=0", path)
	cmd := exec.CommandContext(probeCtx, "ffprobe", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New("HLS 时间轴探测失败")
	}
	if err = cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("HLS 时间轴探测失败")
	}
	timelines := map[int]*hlsPacketTimeline{}
	scanner := bufio.NewScanner(stdout)
	valid := true
	for scanner.Scan() {
		if scanner.Text() == "" {
			continue
		}
		fields := strings.Split(scanner.Text(), ",")
		if len(fields) < 4 {
			valid = false
			break
		}
		index, indexErr := strconv.Atoi(fields[0])
		pts, ptsErr := strconv.ParseFloat(fields[1], 64)
		dts, dtsErr := strconv.ParseFloat(fields[2], 64)
		if indexErr != nil || ptsErr != nil || dtsErr != nil || math.IsNaN(pts) || math.IsInf(pts, 0) || math.IsNaN(dts) || math.IsInf(dts, 0) {
			valid = false
			break
		}
		timeline := timelines[index]
		if timeline == nil {
			timeline = &hlsPacketTimeline{firstPTS: pts, lastPTS: pts}
			timelines[index] = timeline
		} else {
			delta := dts - timeline.lastDTS
			timeline.rollback = timeline.rollback || delta < 0
			timeline.gap = timeline.gap || delta > 0.2
			timeline.discontinuous = timeline.discontinuous || delta <= 0 || delta > 0.2
			timeline.firstPTS = math.Min(timeline.firstPTS, pts)
			timeline.lastPTS = math.Max(timeline.lastPTS, pts)
		}
		timeline.lastDTS = dts
		timeline.count++
		if duration, err := strconv.ParseFloat(fields[3], 64); err == nil && duration > 0 && !math.IsNaN(duration) && !math.IsInf(duration, 0) {
			timeline.duration += duration
		} else if streams == "a" {
			valid = false
			break
		}
	}
	if !valid || scanner.Err() != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !valid || scanner.Err() != nil || waitErr != nil {
		return nil, errors.New("HLS 时间轴探测失败")
	}
	indices := make([]int, 0, len(timelines))
	for index := range timelines {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	result := make([]hlsPacketTimeline, 0, len(indices))
	for _, index := range indices {
		result = append(result, *timelines[index])
	}
	return result, nil
}

// recoverHLSAudioTimestamps 只修复音频回退且视频连续的源，保留视频时间轴和各音轨起点。
func recoverHLSAudioTimestamps(ctx context.Context, input, dir, probeLimit string, expected float64) (string, error) {
	video, err := hlsPacketTimelines(ctx, input, "v:0", probeLimit)
	if err != nil {
		return "", err
	}
	if len(video) != 1 || video[0].count < 2 || video[0].discontinuous {
		return "", nil
	}
	span := (video[0].lastPTS - video[0].firstPTS) * float64(video[0].count) / float64(video[0].count-1)
	if math.Abs(span-expected) > math.Max(2, expected*0.02) {
		return "", nil
	}
	audio, err := hlsPacketTimelines(ctx, input, "a", probeLimit)
	if err != nil {
		return "", err
	}
	rollback := false
	for _, track := range audio {
		if track.gap {
			return "", nil
		}
		rollback = rollback || track.rollback
	}
	if !rollback {
		return "", nil
	}
	offsets, err := hlsAudioOffsets(ctx, input, probeLimit)
	if err != nil {
		return "", err
	}
	output := filepath.Join(dir, "output-audio-timestamps.mp4")
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("HLS 合并暂存目标已存在或不可访问")
	}
	args := []string{"-nostdin", "-v", "error", "-protocol_whitelist", "file,crypto", "-allowed_extensions", "ALL", "-probesize", probeLimit, "-analyzeduration", probeLimit, "-copyts", "-start_at_zero", "-xerror", "-i", input, "-map", "0:v:0", "-map", "0:a?", "-c:v", "copy", "-c:a", "aac", "-af", "asetpts=N/SR/TB+STARTPTS", "-movflags", "+faststart", "-n", output}
	if _, err := exec.CommandContext(ctx, "ffmpeg", args...).Output(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", hlsMergeError(err)
	}
	restoredVideo, err := hlsPacketTimelines(ctx, output, "v:0", probeLimit)
	if err != nil {
		return "", err
	}
	if len(restoredVideo) != 1 || restoredVideo[0].count != video[0].count || restoredVideo[0].discontinuous {
		return "", errors.New("HLS 视频时间轴恢复校验失败，未发布")
	}
	duration, err := hlsVideoDuration(ctx, output)
	if err != nil {
		return "", err
	}
	if math.Abs(duration-expected) > math.Max(2, expected*0.02) {
		return "", errors.New("HLS 视频时长恢复校验失败，未发布")
	}
	restoredAudio, err := hlsPacketTimelines(ctx, output, "a", probeLimit)
	if err != nil {
		return "", err
	}
	restoredOffsets, err := hlsAudioOffsets(ctx, output, probeLimit)
	if err != nil {
		return "", err
	}
	if len(restoredAudio) != len(audio) || len(restoredOffsets) != len(offsets) {
		return "", errors.New("HLS 音轨恢复数量不符，未发布")
	}
	for i := range audio {
		if math.Abs(restoredAudio[i].duration-audio[i].duration) > 0.25 || math.Abs(restoredOffsets[i]-offsets[i]) > 0.25 {
			return "", errors.New("HLS 音轨时长或起点恢复校验失败，未发布")
		}
	}
	return output, nil
}
