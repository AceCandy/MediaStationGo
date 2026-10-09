package huangguoai

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHLSMergeRecoversAudioTimestampRollback(t *testing.T) {
	dir := t.TempDir()
	segment := filepath.Join(dir, "synthetic.ts")
	args := []string{"-nostdin", "-v", "error", "-itsoffset", "0.4", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=30:d=12", "-f", "lavfi", "-i", "sine=frequency=440:duration=12", "-f", "lavfi", "-i", "sine=frequency=880:duration=12", "-map", "0:v:0", "-map", "1:a:0", "-map", "2:a:0", "-c:v", "mpeg2video", "-bf", "2", "-c:a", "aac", "-f", "mpegts", segment}
	if err := exec.CommandContext(t.Context(), "ffmpeg", args...).Run(); err != nil {
		t.Fatal("generate source", err)
	}
	data, err := os.ReadFile(segment)
	if err != nil {
		t.Fatal(err)
	}
	// 只压缩音频 PES 起点，包内 AAC 采样仍连续；下一包的时间戳会回退。
	first := map[int]uint64{}
	for i := 0; i+188 <= len(data); i += 188 {
		packet := data[i : i+188]
		if packet[0] != 0x47 || packet[1]&0x40 == 0 || packet[3]&0x10 == 0 {
			continue
		}
		pos := 4
		if packet[3]&0x20 != 0 {
			pos += int(packet[4]) + 1
		}
		if pos+14 > 188 || packet[pos] != 0 || packet[pos+1] != 0 || packet[pos+2] != 1 || packet[pos+3]&0xe0 != 0xc0 || packet[pos+7]&0xc0 != 0x80 {
			continue
		}
		pid := int(packet[1]&0x1f)<<8 | int(packet[2])
		b := packet[pos+9 : pos+14]
		pts := uint64(b[0]&0x0e)<<29 | uint64(b[1])<<22 | uint64(b[2]&0xfe)<<14 | uint64(b[3])<<7 | uint64(b[4]>>1)
		base, ok := first[pid]
		if !ok {
			base = pts
			first[pid] = pts
		}
		pts = base + (pts-base)/10
		b[0] = b[0]&0xf0 | byte(pts>>29)&0x0e | 1
		b[1] = byte(pts >> 22)
		b[2] = byte(pts>>14)&0xfe | 1
		b[3] = byte(pts >> 7)
		b[4] = byte(pts<<1) | 1
	}
	if len(first) != 2 {
		t.Fatal("fixture did not rewrite both audio tracks")
	}
	if err := os.WriteFile(segment, data, 0600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "input.m3u8")
	if err := os.WriteFile(input, []byte("#EXTM3U\n#EXT-X-TARGETDURATION:12\n#EXTINF:12,\nsynthetic.ts\n#EXT-X-ENDLIST\n"), 0600); err != nil {
		t.Fatal(err)
	}
	timelines, err := hlsPacketTimelines(t.Context(), input, "a", "30000000")
	if err != nil || len(timelines) != 2 {
		t.Fatal("missing source audio packet evidence", err)
	}
	for _, track := range timelines {
		if !track.rollback || track.gap {
			t.Fatal("fixture did not produce audio packet rollback without pauses")
		}
	}
	output, err := mergeHLS(t.Context(), input, dir, 12)
	if err != nil {
		t.Fatal("audio rollback still produces review candidate", err)
	}
	if filepath.Base(output) != "output-audio-timestamps.mp4" {
		t.Fatal("fixture did not exercise audio timestamp recovery")
	}
	original, err := hlsVideoDuration(t.Context(), filepath.Join(dir, "output.mp4"))
	if err != nil || math.Abs(original-12) <= 2 {
		t.Fatal("fixture did not reproduce stretched video", original, err)
	}
	assertHLSMergeTracks(t, output, 12, 2, 360)
	if restored, err := recoverHLSAudioTimestamps(t.Context(), input, t.TempDir(), "30000000", 30); err != nil || restored != "" {
		t.Fatal("wrong playlist duration enabled audio timestamp recovery", err)
	}
	if restored, err := recoverHLSAudioTimestamps(t.Context(), input, dir, "30000000", 12); err == nil || restored != "" {
		t.Fatal("existing recovery output was overwritten")
	}
	before, err := hlsAudioOffsets(t.Context(), input, "30000000")
	if err != nil {
		t.Fatal(err)
	}
	after, err := hlsAudioOffsets(t.Context(), output, "30000000")
	if err != nil || len(before) != len(after) {
		t.Fatal("audio offset evidence lost", err)
	}
	for i := range before {
		if math.Abs(before[i]-after[i]) > 0.25 {
			t.Fatal("audio start changed")
		}
	}
}

func TestHLSAudioTimestampRecoveryRejectsAudioPause(t *testing.T) {
	tools := t.TempDir()
	// 视频连续；音频既有回退又有真实停顿，不能用连续采样计数抹掉停顿。
	probe := "#!/bin/sh\ncase \" $* \" in\n*' -select_streams v:0 '*) printf '0,0,0,0.03\\n0,0.03,0.03,0.03\\n0,0.06,0.06,0.03\\n' ;;\n*' -select_streams a '*) printf '1,0.03,0.03,0.02\\n1,0,0,0.02\\n1,1,1,0.02\\n' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(tools, "ffprobe"), []byte(probe), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	output, err := recoverHLSAudioTimestamps(t.Context(), "synthetic.m3u8", dir, "30000000", 0.09)
	if err != nil || output != "" {
		t.Fatal("audio pause enabled timestamp recovery", output, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected source produced recovery output", err)
	}
}

func TestHLSPacketTimelineRejectsInvalidEvidence(t *testing.T) {
	for _, packets := range []string{"0,N/A,1,0.03\n", "0,NaN,1,0.03\n", "0,1,Inf,0.03\n", "0,1,1,N/A\n"} {
		t.Run(packets, func(t *testing.T) {
			tools := t.TempDir()
			if err := os.WriteFile(filepath.Join(tools, "ffprobe"), []byte("#!/bin/sh\nprintf '"+packets+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
			if _, err := hlsPacketTimelines(t.Context(), "synthetic.m3u8", "a", "30000000"); err == nil {
				t.Fatal("invalid packet evidence accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := hlsPacketTimelines(ctx, "synthetic.m3u8", "v:0", "30000000"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled probe not stopped", err)
	}
}
