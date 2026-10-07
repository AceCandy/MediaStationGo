package huangguoai

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
)

const maxMediaBytes int64 = 8 << 30
const maxSegmentBytes int64 = 128 << 20
const maxSegments = 10000

type Resource struct {
	URL            string
	Offset, Length int64
}
type Segment struct {
	Resource
	KeyGeneration int
	Duration      float64
	KeyURL, IV    string
	Map           *Resource
	Discontinuity bool
}
type Variant struct {
	URL       string
	Bandwidth int64
}
type Playlist struct {
	Segments []Segment
	Variants []Variant
	Sequence int64
	Duration float64
}

// ParsePlaylist rejects open-ended and unsupported encryption before downloading resources.
func ParsePlaylist(body []byte, base string) (Playlist, error) {
	p := Playlist{}
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "#EXTM3U") {
		return p, errors.New("媒体不是有效 HLS 清单")
	}
	resolve := func(raw string) (string, error) {
		u, e := url.Parse(raw)
		if e != nil {
			return "", errors.New("HLS 地址无效")
		}
		b, e := url.Parse(base)
		if e != nil {
			return "", errors.New("HLS 基址无效")
		}
		full := b.ResolveReference(u).String()
		if !hongguo.ValidDownloadURL(full) {
			return "", errors.New("HLS 地址无效")
		}
		return full, nil
	}
	var duration float64
	var key, iv string
	keyGeneration := 0
	var init *Resource
	var discontinuity bool
	var length, offset int64
	var previousURL string
	var previousEnd int64
	var implicit bool
	var bandwidth int64
	end := false
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if end && !strings.HasPrefix(line, "#") {
			return p, errors.New("HLS 结束标记后出现媒体")
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			a, e := attributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			if e != nil {
				return p, e
			}
			bandwidth, _ = strconv.ParseInt(a["BANDWIDTH"], 10, 64)
			if bandwidth <= 0 {
				return p, errors.New("HLS 画质参数无效")
			}
		case strings.HasPrefix(line, "#EXT-X-MEDIA:"):
			a, e := attributes(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))
			if e != nil {
				return p, e
			}
			if a["TYPE"] == "AUDIO" && a["URI"] != "" {
				return p, errors.New("暂不支持独立音轨 HLS")
			}
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			n, e := strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 64)
			if e != nil || n < 0 {
				return p, errors.New("HLS 媒体序列无效")
			}
			p.Sequence = n
		case strings.HasPrefix(line, "#EXTINF:"):
			n, e := strconv.ParseFloat(strings.SplitN(strings.TrimPrefix(line, "#EXTINF:"), ",", 2)[0], 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 3600 || duration != 0 {
				return p, errors.New("HLS 分片时长无效")
			}
			duration = n
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			keyGeneration++
			a, e := attributes(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			if e != nil {
				return p, e
			}
			if a["METHOD"] == "NONE" {
				key = ""
				iv = ""
				continue
			}
			if a["METHOD"] != "AES-128" || (a["KEYFORMAT"] != "" && a["KEYFORMAT"] != "identity") {
				return p, errors.New("暂不支持该 HLS 加密格式")
			}
			key, e = resolve(a["URI"])
			if e != nil || a["URI"] == "" {
				return p, errors.New("HLS 密钥地址无效")
			}
			iv = a["IV"]
			if iv != "" {
				iv = strings.TrimPrefix(strings.ToLower(iv), "0x")
				if len(iv) > 32 {
					return p, errors.New("HLS IV 无效")
				}
				iv = strings.Repeat("0", 32-len(iv)) + iv
				if _, e = hex.DecodeString(iv); e != nil {
					return p, errors.New("HLS IV 无效")
				}
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			a, e := attributes(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			if e != nil {
				return p, e
			}
			if key != "" {
				return p, errors.New("暂不支持加密的 HLS 初始化段")
			}
			raw, e := resolve(a["URI"])
			if e != nil || a["URI"] == "" {
				return p, errors.New("HLS 初始化段无效")
			}
			r := Resource{URL: raw}
			if a["BYTERANGE"] != "" {
				r.Length, r.Offset, _, e = parseRange(a["BYTERANGE"])
				if e != nil || !strings.Contains(a["BYTERANGE"], "@") {
					return p, errors.New("HLS 初始化段范围无效")
				}
			}
			init = &r
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			var e error
			length, offset, implicit, e = parseRange(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"))
			if e != nil {
				return p, e
			}
		case line == "#EXT-X-DISCONTINUITY":
			discontinuity = true
		case line == "#EXT-X-ENDLIST":
			end = true
		case strings.HasPrefix(line, "#EXT-X-PART:") || strings.HasPrefix(line, "#EXT-X-SKIP:") || strings.HasPrefix(line, "#EXT-X-GAP") || strings.HasPrefix(line, "#EXT-X-I-FRAMES-ONLY"):
			return p, errors.New("暂不支持不完整或低延迟 HLS")
		case strings.HasPrefix(line, "#"):
			continue
		default:
			raw, e := resolve(line)
			if e != nil {
				return p, e
			}
			if bandwidth > 0 {
				p.Variants = append(p.Variants, Variant{URL: raw, Bandwidth: bandwidth})
				bandwidth = 0
				continue
			}
			if duration <= 0 {
				return p, errors.New("HLS 分片缺少时长")
			}
			if implicit {
				if previousURL != raw || previousEnd <= 0 {
					return p, errors.New("HLS 隐式字节范围无效")
				}
				offset = previousEnd
			}
			p.Segments = append(p.Segments, Segment{Resource: Resource{URL: raw, Offset: offset, Length: length}, Duration: duration, KeyGeneration: keyGeneration, KeyURL: key, IV: iv, Map: init, Discontinuity: discontinuity})
			p.Duration += duration
			previousURL = raw
			previousEnd = offset + length
			duration = 0
			offset = 0
			length = 0
			implicit = false
			discontinuity = false
			if len(p.Segments) > maxSegments {
				return p, errors.New("HLS 分片过多")
			}
		}
	}
	if scanner.Err() != nil || duration != 0 || bandwidth != 0 {
		return p, errors.New("HLS 清单不完整")
	}
	if len(p.Variants) > 0 {
		if len(p.Segments) > 0 || len(p.Variants) > 32 {
			return p, errors.New("HLS 主清单无效")
		}
		sort.SliceStable(p.Variants, func(i, j int) bool { return p.Variants[i].Bandwidth > p.Variants[j].Bandwidth })
		return p, nil
	}
	if !end || len(p.Segments) == 0 {
		return p, errors.New("HLS 未提供完整 VOD 结束证据")
	}
	return p, nil
}

func parseRange(s string) (int64, int64, bool, error) {
	parts := strings.Split(s, "@")
	n, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || n <= 0 || n > maxSegmentBytes || len(parts) > 2 {
		return 0, 0, false, errors.New("HLS 字节范围无效")
	}
	if len(parts) == 1 {
		return n, 0, true, nil
	}
	o, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || o < 0 || o > maxMediaBytes-n {
		return 0, 0, false, errors.New("HLS 字节范围无效")
	}
	return n, o, false, nil
}

func attributes(s string) (map[string]string, error) {
	result := map[string]string{}
	for s != "" {
		key, rest, ok := strings.Cut(s, "=")
		if !ok || key == "" {
			return nil, errors.New("HLS 属性无效")
		}
		s = rest
		var value string
		if strings.HasPrefix(s, "\"") {
			end := strings.Index(s[1:], "\"")
			if end < 0 {
				return nil, errors.New("HLS 属性无效")
			}
			value = s[1 : 1+end]
			s = s[end+2:]
			if s != "" && !strings.HasPrefix(s, ",") {
				return nil, errors.New("HLS 属性无效")
			}
			s = strings.TrimPrefix(s, ",")
		} else {
			value, s, _ = strings.Cut(s, ",")
		}
		if _, ok := result[key]; ok {
			return nil, errors.New("HLS 重复属性")
		}
		result[key] = value
	}
	return result, nil
}

// Download fetches every resource through the guarded client, then uses only local inputs in FFmpeg.
// The caller owns the private attempt directory and must remove it on every exit path.
func (c *Client) Download(ctx context.Context, media Media, dir string, progress func(int64)) (string, float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	if !hongguo.ValidDownloadURL(media.URL) {
		return "", 0, errors.New("媒体地址无效")
	}
	resp, err := hongguo.DownloadRequest(ctx, c.http, hongguo.DownloadMedia{URL: media.URL, Referer: media.Referer})
	if err != nil {
		return "", 0, err
	}
	reader := bufio.NewReader(resp.Body)
	prefix, _ := reader.Peek(7)
	input := filepath.Join(dir, "input.mp4")
	if string(prefix) != "#EXTM3U" {
		defer resp.Body.Close()
		f, e := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", 0, errors.New("无法创建下载暂存文件")
		}
		n, e := io.Copy(f, io.LimitReader(reader, maxMediaBytes+1))
		syncErr := f.Sync()
		closeErr := f.Close()
		if e != nil || syncErr != nil || closeErr != nil || n > maxMediaBytes || (resp.ContentLength >= 0 && n != resp.ContentLength) {
			return "", 0, errors.New("媒体传输未完成")
		}
		if progress != nil {
			progress(n)
		}
		return input, 0, nil
	}
	body, err := io.ReadAll(io.LimitReader(reader, (2<<20)+1))
	final := resp.Request.URL.String()
	resp.Body.Close()
	if err != nil || len(body) > 2<<20 {
		return "", 0, errors.New("HLS 清单读取失败")
	}
	p, err := ParsePlaylist(body, final)
	if err != nil {
		return "", 0, err
	}
	seen := map[string]bool{final: true}
	for depth := 0; len(p.Variants) > 0; depth++ {
		if depth >= 3 {
			return "", 0, errors.New("HLS 清单嵌套过深")
		}
		raw := p.Variants[0].URL
		if seen[raw] {
			return "", 0, errors.New("HLS 清单循环")
		}
		seen[raw] = true
		body, final, err = c.request(ctx, raw, media.Referer, 2<<20)
		if err != nil {
			return "", 0, err
		}
		p, err = ParsePlaylist(body, final)
		if err != nil {
			return "", 0, err
		}
	}
	var total int64
	var manifest strings.Builder
	manifest.WriteString("#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:3600\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	keys := map[int]string{}
	maps := map[Resource]string{}
	previousMap := ""
	for i, segment := range p.Segments {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		if segment.Discontinuity {
			manifest.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		if segment.Map != nil {
			name := maps[*segment.Map]
			if name == "" {
				name = fmt.Sprintf("map-%04d.mp4", len(maps))
				n, e := c.fetchResource(ctx, *segment.Map, media.Referer, filepath.Join(dir, name), maxSegmentBytes)
				if e != nil {
					return "", 0, fmt.Errorf("HLS 第 %d 个分片初始化资源失败：%w", i+1, e)
				}
				total += n
				maps[*segment.Map] = name
			}
			if name != previousMap {
				fmt.Fprintf(&manifest, "#EXT-X-MAP:URI=\"%s\"\n", name)
				previousMap = name
			}
		}
		if segment.KeyURL != "" {
			name := keys[segment.KeyGeneration]
			if name == "" {
				name = fmt.Sprintf("key-%04d.bin", len(keys))
				n, e := c.fetchResource(ctx, Resource{URL: segment.KeyURL}, media.Referer, filepath.Join(dir, name), 16)
				if e != nil {
					return "", 0, fmt.Errorf("HLS 第 %d 个分片密钥读取失败：%w", i+1, e)
				}
				if n != 16 {
					return "", 0, fmt.Errorf("HLS 第 %d 个分片密钥长度无效", i+1)
				}
				keys[segment.KeyGeneration] = name
			}
			iv := segment.IV
			if iv == "" {
				iv = fmt.Sprintf("%032x", uint64(p.Sequence)+uint64(i))
			}
			fmt.Fprintf(&manifest, "#EXT-X-KEY:METHOD=AES-128,URI=\"%s\",IV=0x%s\n", name, iv)
		} else {
			manifest.WriteString("#EXT-X-KEY:METHOD=NONE\n")
		}
		extension := ".ts"
		if segment.Map != nil {
			extension = ".m4s"
		}
		name := fmt.Sprintf("segment-%06d%s", i, extension)
		n, e := c.fetchResource(ctx, segment.Resource, media.Referer, filepath.Join(dir, name), maxSegmentBytes)
		if e != nil {
			return "", 0, fmt.Errorf("HLS 第 %d/%d 个分片下载失败：%w", i+1, len(p.Segments), e)
		}
		total += n
		if total > maxMediaBytes {
			return "", 0, errors.New("媒体超出大小限制")
		}
		if progress != nil {
			progress(total)
		}
		fmt.Fprintf(&manifest, "#EXTINF:%.6f,\n%s\n", segment.Duration, name)
	}
	manifest.WriteString("#EXT-X-ENDLIST\n")
	input = filepath.Join(dir, "input.m3u8")
	if os.WriteFile(input, []byte(manifest.String()), 0600) != nil {
		return "", 0, errors.New("无法保存本地 HLS 清单")
	}
	output := filepath.Join(dir, "output.mp4")
	// 部分源的画面晚于音频出现，需要扩大输入探测范围才能取得视频尺寸。
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-protocol_whitelist", "file,crypto", "-allowed_extensions", "ALL", "-probesize", "30000000", "-analyzeduration", "30000000", "-i", input, "-map", "0:v:0", "-map", "0:a?", "-c", "copy", "-movflags", "+faststart", "-n", output)
	if _, err := cmd.Output(); err != nil {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		return "", 0, hlsMergeError(err)
	}
	return output, p.Duration, nil
}

// 仅公开固定诊断和退出码；FFmpeg 原文可能包含源标题、路径或密钥，不能进入任务日志。
func hlsMergeError(err error) error {
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrPermission) {
		return errors.New("HLS 合并失败：FFmpeg 未安装或不可执行")
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// Output 将 stderr 限制为头尾片段，分类后不保留原始文本。
		stderr := string(exit.Stderr)
		switch {
		case strings.Contains(stderr, "No space left on device"):
			return errors.New("HLS 合并失败：存储空间不足")
		case strings.Contains(stderr, "Permission denied"):
			return errors.New("HLS 合并失败：暂存文件权限不足")
		case strings.Contains(stderr, "dimensions not set"):
			return errors.New("HLS 合并失败：未识别到视频尺寸")
		case strings.Contains(stderr, "Invalid data found when processing input"):
			return errors.New("HLS 合并失败：分片或密钥无法解析")
		default:
			return fmt.Errorf("HLS 合并失败：FFmpeg 退出码 %d", exit.ExitCode())
		}
	}
	return errors.New("HLS 合并失败：FFmpeg 无法启动")
}

// 分片、初始化资源和密钥共用有限重试；已完成资源不会重新获取。
func (c *Client) fetchResource(ctx context.Context, r Resource, referer, path string, max int64) (int64, error) {
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, retry, err := c.fetchResourceOnce(ctx, r, referer, path, max)
		if err == nil || !retry || attempt == 3 {
			if err != nil && retry {
				err = fmt.Errorf("%w（已尝试 %d 次）", err, attempt)
			}
			return n, err
		}
		timer := time.NewTimer(time.Duration(attempt) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
}

// 仅临时请求或读取故障允许重试，存储及协议错误立即失败。
func (c *Client) fetchResourceOnce(ctx context.Context, r Resource, referer, path string, max int64) (n int64, retry bool, err error) {
	if !hongguo.ValidDownloadURL(r.URL) || strings.ContainsAny(referer, "\r\n") {
		return 0, false, errors.New("HLS 资源地址无效")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", r.URL, nil)
	if err != nil {
		return 0, false, errors.New("HLS 请求无效")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", referer)
	if r.Length > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.Offset, r.Offset+r.Length-1))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, false, ctx.Err()
		}
		// http.Client 包装的 url.Error 本身也实现 net.Error，需检查底层错误。
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			err = requestErr.Err
		}
		var network net.Error
		retry = errors.As(err, &network) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
		return 0, retry, errors.New("HLS 资源网络请求失败")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return 0, true, fmt.Errorf("HLS 资源 HTTP %d", resp.StatusCode)
	}
	if r.Length > 0 {
		var start, end, size int64
		if resp.StatusCode != http.StatusPartialContent || func() bool {
			_, e := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &size)
			return e != nil || resp.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, size)
		}() || start != r.Offset || end != r.Offset+r.Length-1 || size <= end {
			return 0, false, errors.New("HLS 字节范围响应无效")
		}
		max = r.Length
	} else if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("HLS 资源 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > max {
		return 0, false, errors.New("HLS 资源超出大小限制")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, false, errors.New("无法创建 HLS 暂存文件")
	}
	defer func() {
		if err != nil {
			if e := os.Remove(path); e != nil {
				retry = false
				err = errors.New("HLS 失败暂存文件清理失败")
			}
		}
	}()
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, max+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	var writeErr *os.PathError
	switch {
	case errors.As(copyErr, &writeErr) || syncErr != nil || closeErr != nil:
		return 0, false, errors.New("HLS 资源写盘失败")
	case ctx.Err() != nil:
		return 0, false, ctx.Err()
	case n > max:
		return 0, false, errors.New("HLS 资源超出大小限制")
	case copyErr != nil:
		return 0, true, errors.New("HLS 资源读取中断")
	case (r.Length > 0 && n != r.Length) || (resp.ContentLength >= 0 && n != resp.ContentLength):
		return 0, true, fmt.Errorf("HLS 资源长度不足：收到 %d 字节", n)
	}
	return n, false, nil
}
