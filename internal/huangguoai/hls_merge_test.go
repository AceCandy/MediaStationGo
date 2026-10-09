package huangguoai

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHLSMergeRecoversTimestampJump(t *testing.T) {
	dir := t.TempDir()
	segment := filepath.Join(dir, "synthetic.ts")
	// 四秒内容在中间跳过五秒时间戳，默认十秒阈值不会修正。
	args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=4", "-f", "lavfi", "-i", "sine=frequency=440:duration=4", "-vf", "setpts=PTS+if(gte(N\\,20)\\,5/TB\\,0)", "-af", "asetpts=PTS+if(gte(N\\,88200)\\,5/TB\\,0)", "-fps_mode", "passthrough", "-c:v", "mpeg2video", "-c:a", "aac", "-f", "mpegts", segment}
	if err := exec.CommandContext(t.Context(), "ffmpeg", args...).Run(); err != nil {
		t.Fatal("generate timestamp jump:", err)
	}
	input := filepath.Join(dir, "input.m3u8")
	if err := os.WriteFile(input, []byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nsynthetic.ts\n#EXT-X-ENDLIST\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := mergeHLS(t.Context(), input, dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	original, err := hlsVideoDuration(t.Context(), filepath.Join(dir, "output.mp4"))
	if err != nil || original < 8 {
		t.Fatalf("fixture did not reproduce stretched timeline: %.3f %v", original, err)
	}
	assertHLSMergeTracks(t, output, 4, 1, 40)
	// 清单确实包含停顿时，原时间轴匹配，不应该启动修正。
	unchanged, err := mergeHLS(t.Context(), input, t.TempDir(), 9)
	if err != nil || filepath.Base(unchanged) != "output.mp4" {
		t.Fatalf("valid source timeline changed: %v", err)
	}
	duration, err := hlsVideoDuration(t.Context(), unchanged)
	if err != nil || math.Abs(duration-9) > 0.1 {
		t.Fatalf("valid pause lost: %.3f %v", duration, err)
	}
	// 错误清单不能靠时间戳恢复放行。
	candidate, err := mergeHLS(t.Context(), input, t.TempDir(), 20)
	var mismatch HLSDurationMismatchError
	if !errors.As(err, &mismatch) || filepath.Base(candidate) != "output.mp4" {
		t.Fatalf("wrong playlist lost original review candidate: %s %v", candidate, err)
	}
	duration, err = hlsVideoDuration(t.Context(), candidate)
	if err != nil || math.Abs(duration-9) > 0.1 {
		t.Fatalf("review candidate timeline changed: %.3f %v", duration, err)
	}
}

func TestHLSMergeAudioRecoveryPreservesEveryTrack(t *testing.T) {
	dir := t.TempDir()
	segment := filepath.Join(dir, "synthetic.ts")
	args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=4", "-f", "lavfi", "-i", "sine=frequency=440:duration=4", "-f", "lavfi", "-i", "sine=frequency=880:duration=4", "-map", "0:v:0", "-map", "1:a:0", "-map", "2:a:0", "-c:v", "mpeg2video", "-c:a", "aac", "-f", "mpegts", segment}
	if err := exec.CommandContext(t.Context(), "ffmpeg", args...).Run(); err != nil {
		t.Fatal("generate multiple audio tracks:", err)
	}
	input := filepath.Join(dir, "input.m3u8")
	if err := os.WriteFile(input, []byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nsynthetic.ts\n#EXT-X-ENDLIST\n"), 0600); err != nil {
		t.Fatal(err)
	}
	realFFmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	// 复现流复制缺少采样率错误；恢复仍交给真实编码器，验证全部音轨。
	script := "#!/bin/sh\nfor arg in \"$@\"; do\nif [ \"$arg\" = copy ]; then\ncase \" $* \" in *' -c copy '*) printf 'private-path token=private sample rate not set' >&2; exit 234;; esac\nfi\ndone\nexec '" + strings.ReplaceAll(realFFmpeg, "'", "'\\''") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(tools, "ffmpeg"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := mergeHLS(t.Context(), input, dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	assertHLSMergeTracks(t, output, 4, 2, 40)
	// 音轨错误和时间戳跳变同时出现，两个恢复步骤均不能丢轨或改帧数。
	combinedDir := t.TempDir()
	combinedArgs := append([]string(nil), args[:len(args)-1]...)
	combinedArgs = append(combinedArgs, "-vf", "setpts=PTS+if(gte(N\\,20)\\,5/TB\\,0)", "-af", "asetpts=PTS+if(gte(N\\,88200)\\,5/TB\\,0)", "-fps_mode", "passthrough", filepath.Join(combinedDir, "synthetic.ts"))
	if err := exec.CommandContext(t.Context(), realFFmpeg, combinedArgs...).Run(); err != nil {
		t.Fatal("generate combined failures:", err)
	}
	manifest, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	combinedInput := filepath.Join(combinedDir, "input.m3u8")
	if err := os.WriteFile(combinedInput, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	combinedOutput, err := mergeHLS(t.Context(), combinedInput, combinedDir, 4)
	if err != nil {
		t.Fatal(err)
	}
	assertHLSMergeTracks(t, combinedOutput, 4, 2, 40)
	delayedDir := t.TempDir()
	delayedArgs := []string{"-nostdin", "-v", "error", "-itsoffset", "8", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:d=4", "-f", "lavfi", "-i", "sine=frequency=440:duration=12", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "mpeg2video", "-c:a", "aac", "-f", "mpegts", filepath.Join(delayedDir, "synthetic.ts")}
	if err := exec.CommandContext(t.Context(), realFFmpeg, delayedArgs...).Run(); err != nil {
		t.Fatal("generate leading audio:", err)
	}
	delayedInput := filepath.Join(delayedDir, "input.m3u8")
	if err := os.WriteFile(delayedInput, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	delayedOutput, err := mergeHLS(t.Context(), delayedInput, delayedDir, 4)
	if err != nil {
		t.Fatal(err)
	}
	offsets, err := hlsAudioOffsets(t.Context(), delayedOutput)
	if err != nil || len(offsets) != 1 || offsets[0] > -7.9 {
		t.Fatalf("leading audio lost: %v %v", offsets, err)
	}
	audioDuration, err := exec.CommandContext(t.Context(), "ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=duration", "-of", "csv=p=0", delayedOutput).Output()
	seconds, parseErr := strconv.ParseFloat(strings.TrimSpace(string(audioDuration)), 64)
	if err != nil || parseErr != nil || seconds < 11.9 {
		t.Fatalf("audio was trimmed: %.3f %v %v", seconds, err, parseErr)
	}
	// 已有恢复产物不能覆盖；磁盘错误也不能当作采样率错误重试。
	sentinel, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mergeHLS(t.Context(), input, dir, 4); err == nil {
		t.Fatal("overwrote existing recovery output")
	}
	after, err := os.ReadFile(output)
	if err != nil || string(after) != string(sentinel) {
		t.Fatal("existing output changed")
	}
	if err := os.WriteFile(filepath.Join(tools, "ffmpeg"), []byte("#!/bin/sh\nprintf 'private-path No space left on device' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	failedDir := t.TempDir()
	if _, err := mergeHLS(t.Context(), input, failedDir, 4); err == nil || err.Error() != "HLS 合并失败：存储空间不足" {
		t.Fatalf("unsafe or missing disk error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(failedDir, "output-recovery-1.mp4")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("disk error triggered recovery")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := mergeHLS(ctx, input, t.TempDir(), 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled merge was retried: %v", err)
	}
}

func assertHLSMergeTracks(t *testing.T, output string, expected float64, audioTracks, frames int) {
	t.Helper()
	data, err := exec.CommandContext(t.Context(), "ffprobe", "-v", "error", "-count_frames", "-show_entries", "stream=codec_type,duration,nb_read_frames,sample_rate", "-of", "json", output).Output()
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Streams []struct {
			Type       string `json:"codec_type"`
			Duration   string `json:"duration"`
			Frames     string `json:"nb_read_frames"`
			SampleRate string `json:"sample_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatal(err)
	}
	audio, video := 0, 0
	for _, stream := range probe.Streams {
		duration, _ := strconv.ParseFloat(stream.Duration, 64)
		if math.Abs(duration-expected) > 0.2 {
			t.Fatalf("unexpected track duration: %.3f", duration)
		}
		if stream.Type == "video" {
			video++
			count, _ := strconv.Atoi(stream.Frames)
			if count != frames {
				t.Fatalf("video frames lost or inserted: %d", count)
			}
		} else if stream.Type == "audio" {
			audio++
			if rate, _ := strconv.Atoi(stream.SampleRate); rate <= 0 {
				t.Fatal("audio sample rate missing")
			}
		}
	}
	if video != 1 || audio != audioTracks {
		t.Fatalf("lost tracks: video=%d audio=%d", video, audio)
	}
	if err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-err_detect", "explode", "-i", output, "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-").Run(); err != nil {
		t.Fatal("full decode failed:", err)
	}
}

func TestHLSMergeRejectsAudioTimingLoss(t *testing.T) {
	tools := t.TempDir()
	ffmpeg := "#!/bin/sh\ncase \" $* \" in *' -c copy '*) printf 'private-token sample rate not set' >&2; exit 234;; esac\nfor output; do :; done\nprintf synthetic > \"$output\"\n"
	if err := os.WriteFile(filepath.Join(tools, "ffmpeg"), []byte(ffmpeg), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tt := range []struct{ name, streams, want string }{
		{"desynchronized", `[{"codec_type":"video","start_time":"4473"},{"codec_type":"audio","start_time":"120"},{"codec_type":"audio","start_time":"120"}]`, "音画起点校验失败"},
		{"missing_audio", `[{"codec_type":"video","start_time":"0"}]`, "缺少原始轨道证据"},
		{"lost_second_audio", `[{"codec_type":"video","start_time":"0"},{"codec_type":"audio","start_time":"0"}]`, "音轨恢复数量不符"},
		{"invalid_start", `[{"codec_type":"video","start_time":"0"},{"codec_type":"audio","start_time":"NaN"}]`, "音画起点无效"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// 时长正常，但恢复丢轨或引入起点错位时必须拒绝。
			script := "#!/bin/sh\ncase \" $* \" in\n*stream=duration*) printf '%s' '{\"streams\":[{\"duration\":\"121\"}]}';;\n*input.m3u8*) printf '%s' '{\"streams\":[{\"codec_type\":\"video\",\"start_time\":\"4475\"},{\"codec_type\":\"audio\",\"start_time\":\"4475\"},{\"codec_type\":\"audio\",\"start_time\":\"4475\"}]}';;\n*) printf '%s' '{\"streams\":" + tt.streams + "}';;\nesac\n"
			if err := os.WriteFile(filepath.Join(tools, "ffprobe"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			for _, expected := range []float64{121, 200} {
				output, err := mergeHLS(t.Context(), "input.m3u8", t.TempDir(), expected)
				if output != "" || err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "private-token") {
					t.Fatalf("invalid recovery accepted or unsafe error: %s %v", output, err)
				}
			}
		})
	}
	for _, message := range []string{"No space left on device", "Permission denied", "Invalid data found when processing input"} {
		err := exec.CommandContext(t.Context(), "sh", "-c", "exit 1").Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		exit.Stderr = []byte("sample rate not set " + message)
		if hlsAudioParametersMissing(exit) {
			t.Fatalf("unsafe error triggered audio recovery: %s", message)
		}
	}
}
