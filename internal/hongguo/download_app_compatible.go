package hongguo

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// 固定协议盐用于地址密钥派生，按 ttsdk-ttplayer 1.52.1.13 的版本 1 解码流程核对。
var downloadAppURLSalt = [sha512.Size]byte{
	0x4d, 0xd4, 0xc2, 0xe6, 0xb8, 0x31, 0x62, 0x09, 0x0e, 0x52, 0xb3, 0xc7, 0xa6, 0x73, 0x3b, 0xa4,
	0x1c, 0xb2, 0x46, 0x2b, 0x82, 0x9a, 0xb5, 0x8a, 0x19, 0x6b, 0x39, 0xdb, 0x57, 0x17, 0x75, 0x24,
	0xf4, 0x9b, 0xaf, 0x7f, 0x08, 0xe8, 0xd6, 0x8d, 0x26, 0xa7, 0x2e, 0x37, 0xc1, 0xa9, 0x5a, 0x2f,
	0x1f, 0x05, 0xa5, 0x18, 0x92, 0xae, 0xf2, 0x94, 0x97, 0x32, 0xb6, 0x2a, 0x38, 0xaa, 0xdd, 0x58,
}

// resolveDownloadAppCompatible 仅用于全为 ByteVC2 的 App 响应，仍归属 App 来源及其重试预算。
func (c *Client) resolveDownloadAppCompatible(ctx context.Context, model map[string]any) (DownloadMedia, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return DownloadMedia{}, err
	}
	videoID := scalar(model["video_id"])
	raw := scalar(object(model["fallback_api"])["fallback_api"])
	address, err := url.Parse(raw)
	if err != nil || !ValidDownloadURL(raw) || address.Scheme != "https" || address.Host != "vas-lf-x.snssdk.com" ||
		videoID == "" || len(videoID) > 128 || url.PathEscape(videoID) != videoID ||
		!strings.HasPrefix(address.Path, "/video/fplay/1/") || !strings.HasSuffix(address.Path, "/"+videoID) {
		return DownloadMedia{}, errors.New("App 官方兼容接口地址或视频身份无效")
	}
	query, err := url.ParseQuery(address.RawQuery)
	if err != nil {
		return DownloadMedia{}, errors.New("App 官方兼容接口参数无效")
	}
	query.Del("force_fids")
	query.Set("codec_type", "1")
	address.RawQuery = query.Encode()
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := DownloadRequest(ctx, &client, DownloadMedia{URL: address.String(), Referer: "https://novel.snssdk.com/"})
	if err != nil {
		return DownloadMedia{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if ctx.Err() != nil {
		return DownloadMedia{}, ctx.Err()
	}
	if err != nil || len(body) > 4*1024*1024 {
		return DownloadMedia{}, errors.New("App 官方兼容响应读取失败或过大")
	}
	return parseDownloadAppCompatible(body, videoID)
}

// parseDownloadAppCompatible 核对原视频身份，将官方扁平候选交给现有清晰度及密钥选择逻辑。
func parseDownloadAppCompatible(body []byte, videoID string) (DownloadMedia, error) {
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil || result == nil {
		return DownloadMedia{}, errors.New("App 官方兼容响应格式无效")
	}
	info := object(result["video_info"])
	model := object(info["data"])
	if scalar(result["code"]) != "0" || scalar(info["code"]) != "0" || videoID == "" || scalar(model["video_id"]) != videoID {
		return DownloadMedia{}, errors.New("App 官方兼容响应状态或视频身份不一致")
	}
	seed := scalar(model["key_seed"])
	var entries []any
	if indexed, ok := model["video_list"].(map[string]any); ok {
		keys := make([]string, 0, len(indexed))
		for key := range indexed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			entries = append(entries, indexed[key])
		}
	} else {
		entries, _ = model["video_list"].([]any)
	}
	variants := make([]any, 0, len(entries))
	for _, entry := range entries {
		row := object(entry)
		address, err := decodeDownloadAppURL(scalar(row["main_url"]), seed)
		if err != nil {
			continue
		}
		// 复用现有选择器跳过不兼容编码和无效媒体密钥。
		variants = append(variants, map[string]any{"main_url": address, "gear_des_key": row["gear_des_key"], "video_meta": row, "encrypt_info": row})
	}
	model["video_list"] = variants
	return parseDownloadAppMedia(model)
}

// decodeDownloadAppURL 支持普通地址及已验证的版本 1 信封；未知格式不能作为媒体地址使用。
func decodeDownloadAppURL(value, seed string) (string, error) {
	invalid := errors.New("App 官方兼容媒体地址格式或版本不支持")
	if ValidDownloadURL(value) {
		return value, nil
	}
	if len(value) > 16*1024 {
		return "", invalid
	}
	raw, err := decodeDownloadBase64(value)
	if err != nil {
		return "", invalid
	}
	if seed == "" {
		if utf8.Valid(raw) && ValidDownloadURL(string(raw)) {
			return string(raw), nil
		}
		return "", invalid
	}
	if len(seed) > 128 || len(raw) <= 4 || binary.LittleEndian.Uint16(raw[:2]) != 0x00a8 ||
		binary.LittleEndian.Uint16(raw[2:4]) != 1 || (len(raw)-4)%aes.BlockSize != 0 {
		return "", invalid
	}
	keySeed, err := decodeDownloadBase64(seed)
	if err != nil || len(keySeed) != 32 {
		return "", invalid
	}
	first := sha512.Sum512(keySeed)
	material := sha512.Sum512(append(first[:], downloadAppURLSalt[:]...))
	block, err := aes.NewCipher(material[:16])
	if err != nil {
		return "", invalid
	}
	plain := make([]byte, len(raw)-4)
	cipher.NewCBCDecrypter(block, material[16:32]).CryptBlocks(plain, raw[4:])
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(plain) {
		return "", invalid
	}
	for _, b := range plain[len(plain)-padding:] {
		if int(b) != padding {
			return "", invalid
		}
	}
	plain = plain[:len(plain)-padding]
	if !utf8.Valid(plain) || !ValidDownloadURL(string(plain)) {
		return "", invalid
	}
	return string(plain), nil
}
