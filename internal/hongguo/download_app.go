package hongguo

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const downloadAppURL = "https://api5-normal-sinfonlineb.fqnovel.com/novel/player/video_model/v1/"
const downloadAppUserAgent = "com.phoenix.read/73532 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3; Cronet/TTNetVersion:04657795 2026-01-23 QuicVersion:c67e9834 2025-09-08)"

// ErrVideoTakenDown 仅表示当前视频下架，不能据此判定整部作品下架。
var ErrVideoTakenDown = errors.New("App 接口：当前视频已下架（101002）")

// resolveDownloadApp 只读取指定分集的播放信息，不登录、注册设备或持久化取流凭据。
func (c *Client) resolveDownloadApp(ctx context.Context, videoID string) (DownloadMedia, error) {
	data, err := c.appRequest(ctx, downloadAppURL, map[string]any{"video_id": videoID, "content_type": 1, "biz_param": map[string]any{"need_all_video_definition": true, "video_platform": 3}})
	if err != nil {
		return DownloadMedia{}, err
	}
	return parseDownloadApp(data)
}

// appRequest 只接收内部固定接口地址，签名和设备参数不持久化。
func (c *Client) appRequest(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, errors.New("无法准备 App 请求")
	}
	deviceID := func(b []byte) string {
		return strconv.FormatUint(1_000_000_000_000_000_000+binary.BigEndian.Uint64(b)%8_000_000_000_000_000_000, 10)
	}
	query := url.Values{
		"aid": {"8662"}, "app_name": {"novelread"}, "version_code": {"73532"}, "version_name": {"7.3.5.32"},
		"manifest_version_code": {"73532"}, "update_version_code": {"73532"}, "channel": {"update_64"},
		"device_platform": {"android"}, "os": {"android"}, "ssmix": {"a"}, "device_type": {"25053RT47C"},
		"device_brand": {"Redmi"}, "language": {"zh"}, "os_api": {"36"}, "os_version": {"16"},
		"resolution": {"1280*2772"}, "dpi": {"520"}, "ac": {"wifi"},
		"device_id": {deviceID(random[:8])}, "iid": {deviceID(random[8:])},
	}
	now := time.Now()
	query.Set("_rticket", strconv.FormatInt(now.UnixMilli(), 10))
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("App 请求内容无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("App 请求无效")
	}
	req.Header.Set("User-Agent", downloadAppUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("X-XS-From-Web", "0")
	req.Header.Set("Sdk-Version", "2")
	signDownloadAppRequest(req, body, now)
	client := *c.http
	// 不将签名请求跟随重定向发送到其他主机。
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, publicDownloadError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("App 接口 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 {
		return nil, errors.New("App 响应读取失败或过大")
	}
	return data, nil
}

// signDownloadAppRequest 签名仅用于固定 App 接口，不写入日志或业务记录。
func signDownloadAppRequest(req *http.Request, body []byte, now time.Time) {
	queryHash, bodyHash := md5.Sum([]byte(req.URL.RawQuery)), md5.Sum(body)
	var payload [20]byte
	copy(payload[:4], queryHash[:4])
	copy(payload[4:8], bodyHash[:4])
	copy(payload[12:16], []byte{0, 6, 11, 28})
	binary.BigEndian.PutUint32(payload[16:], uint32(now.Unix()))
	key := [...]byte{0x44, 0xb9, 0xb9, 0xd9, 0xa4, 0xae, 0xf9, 0xfc, 0xa4, 0x93, 0xaa, 0x75, 0x7c, 0xa3, 0xc2, 0xc4, 0xa4, 0x96, 0x93, 0x8f}
	for i := range payload {
		payload[i] ^= key[i]
	}
	for i := range payload {
		payload[i] = bits.Reverse8(bits.RotateLeft8(payload[i], 4)^payload[(i+1)%len(payload)]) ^ 0xff ^ byte(len(payload))
	}
	req.Header.Set("X-SS-STUB", fmt.Sprintf("%X", bodyHash))
	req.Header.Set("X-Khronos", strconv.FormatInt(now.Unix(), 10))
	req.Header.Set("X-Gorgon", hex.EncodeToString(append([]byte{0x84, 0x04, 0x40, 0x1c, 0, 0}, payload[:]...)))
	req.Header.Set("X-SS-Req-Ticket", strconv.FormatInt(now.UnixMilli(), 10))
}

func parseDownloadApp(body []byte) (DownloadMedia, error) {
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil || result == nil {
		return DownloadMedia{}, errors.New("App 播放信息格式无效")
	}
	for _, code := range []string{scalar(result["code"]), scalar(result["status_code"]), scalar(object(result["BaseResp"])["StatusCode"])} {
		if code == "101002" {
			return DownloadMedia{}, ErrVideoTakenDown
		}
		if code != "" && code != "0" {
			if _, err := strconv.ParseInt(code, 10, 32); err == nil {
				return DownloadMedia{}, fmt.Errorf("App 接口业务错误（%s）", code)
			}
			return DownloadMedia{}, errors.New("App 接口返回异常状态")
		}
	}
	model := object(object(result["data"])["video_model"])
	if text, ok := object(result["data"])["video_model"].(string); ok {
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		if decoder.Decode(&model) != nil {
			return DownloadMedia{}, errors.New("App 视频信息格式无效")
		}
	}
	rows, _ := model["video_list"].([]any)
	if indexed, ok := model["video_list"].(map[string]any); ok {
		keys := make([]string, 0, len(indexed))
		for key := range indexed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			rows = append(rows, indexed[key])
		}
	}
	var selected DownloadMedia
	var keyErr error
	best := -1
	for _, entry := range rows {
		row := object(entry)
		meta, encryption := object(row["video_meta"]), object(row["encrypt_info"])
		codec := strings.ToLower(scalar(meta["codec_type"]))
		if codec == "bytevc2" || strings.Contains(strings.ToLower(scalar(row["gear_des_key"])), "bytevc2") || !ValidDownloadURL(scalar(row["main_url"])) {
			continue
		}
		media := DownloadMedia{URL: scalar(row["main_url"]), Referer: "https://novel.snssdk.com/"}
		material := scalar(encryption["spade_a"])
		if material != "" || encryption["encrypt"] == true || scalar(encryption["encryption_method"]) == "cenc-aes-ctr" {
			var err error
			media.Key, err = decodeDownloadKey(material)
			if err != nil {
				keyErr = err
				continue
			}
		}
		media.Quality, _ = strconv.Atoi(downloadQuality.FindString(scalar(meta["definition"])))
		media.Width, _ = strconv.Atoi(scalar(meta["vwidth"]))
		media.Height, _ = strconv.Atoi(scalar(meta["vheight"]))
		if media.Width < 0 || media.Width > 16384 || media.Height < 0 || media.Height > 16384 {
			continue
		}
		if media.Quality <= 0 {
			media.Quality = min(media.Width, media.Height)
		}
		if media.Quality < 0 || media.Quality > 4320 {
			continue
		}
		// 只公开已知编码名，避免将来源的任意字符串写入记录。
		switch codec {
		case "bytevc1", "hevc", "h265":
			media.Codec = "hevc"
		case "h264", "avc1":
			media.Codec = "h264"
		case "av1":
			media.Codec = "av1"
		}
		media.Duration, _ = strconv.ParseFloat(scalar(model["video_duration"]), 64)
		if media.Duration <= 0 {
			media.Duration, _ = strconv.ParseFloat(scalar(model["duration"]), 64)
		}
		if media.Duration < 0 || math.IsNaN(media.Duration) || math.IsInf(media.Duration, 0) {
			media.Duration = 0
		}
		score := media.Quality * 10
		if media.Codec == "h264" {
			score++
		}
		if score > best {
			selected, best = media, score
		}
	}
	if best >= 0 {
		return selected, nil
	}
	if keyErr != nil {
		return DownloadMedia{}, keyErr
	}
	return DownloadMedia{}, errors.New("App 接口未返回可用的兼容媒体（已排除不支持的编码）")
}
