package hongguo

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/bits"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const downloadFallbackURL = "https://djapi.999888456.xyz/api/hongguo/play"

var downloadQuality = regexp.MustCompile(`[0-9]+`)

// resolveDownloadFallback 适配备用解析协议，响应材料仅在本次请求中使用。
func (c *Client) resolveDownloadFallback(ctx context.Context, sourceID, videoID string) (DownloadMedia, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	reference, _ := json.Marshal(map[string]any{"content_type": 1004, "from_video_id": "", "series_id": sourceID, "vid": videoID, "video_platform": 3})
	address := downloadFallbackURL + "?" + url.Values{"id": {base64.StdEncoding.EncodeToString(reference)}}.Encode()
	resp, err := DownloadRequest(ctx, c.http, DownloadMedia{URL: address, Referer: BaseURL + "/"})
	if err != nil {
		return DownloadMedia{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(body) > 1024*1024 {
		return DownloadMedia{}, errors.New("备用解析响应读取失败或过大")
	}
	return parseDownloadFallback(body)
}

func parseDownloadFallback(body []byte) (DownloadMedia, error) {
	decoded, err := decodeDownloadEnvelope(strings.TrimSpace(string(body)))
	if err != nil {
		return DownloadMedia{}, err
	}
	var response struct {
		Parse   json.RawMessage `json:"parse"`
		JX      json.RawMessage `json:"jx"`
		Options []struct {
			Name     string `json:"name"`
			URL      string `json:"src"`
			KeyID    string `json:"kid"`
			Material string `json:"spade_a"`
		} `json:"key_urls"`
	}
	if json.Unmarshal(decoded, &response) != nil {
		return DownloadMedia{}, errors.New("备用解析格式无效")
	}
	for _, flag := range []json.RawMessage{response.Parse, response.JX} {
		switch strings.TrimSpace(string(flag)) {
		case "", "null", "false", "0", `"0"`, `""`:
		default:
			return DownloadMedia{}, errors.New("备用解析未返回直接媒体")
		}
	}
	best := -1
	var selected DownloadMedia
	var keyErr error
	for _, option := range response.Options {
		if !ValidDownloadURL(option.URL) {
			continue
		}
		id, err := hex.DecodeString(option.KeyID)
		if err != nil || len(id) != 16 {
			keyErr = errors.New("媒体密钥标识无效")
			continue
		}
		key, err := decodeDownloadKey(option.Material)
		if err != nil {
			keyErr = err
			continue
		}
		quality, _ := strconv.Atoi(downloadQuality.FindString(option.Name))
		if quality > best {
			best = quality
			selected = DownloadMedia{URL: option.URL, Referer: "https://novel.snssdk.com/", Key: key, Quality: quality}
		}
	}
	if best < 0 {
		if keyErr != nil {
			return DownloadMedia{}, keyErr
		}
		return DownloadMedia{}, errors.New("备用接口未返回该集可用的媒体地址和密钥")
	}
	return selected, nil
}

func decodeDownloadBase64(value string) ([]byte, error) {
	data, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return base64.RawStdEncoding.Strict().DecodeString(value)
	}
	return data, nil
}

func decodeDownloadEnvelope(text string) ([]byte, error) {
	invalid := errors.New("备用解析加密响应无效")
	if !strings.HasPrefix(text, "v2.") {
		return []byte(text), nil
	}
	parts := strings.SplitN(text, ".", 3)
	if len(parts) != 3 || len(parts[1]) <= 4 || len(parts[1]) > 1028 {
		return nil, invalid
	}
	encoded, err := hex.DecodeString(parts[1][4:])
	if err != nil || len(encoded) < 32 {
		return nil, invalid
	}
	mask := []byte{104, 64, 70, 166, 190, 168, 143, 130, 225, 254, 251, 217, 196, 34, 45, 60, 29, 20, 103, 105}
	material := make([]byte, len(encoded))
	for i, b := range encoded {
		previous := byte(109)
		if i > 0 {
			previous = encoded[i-1]
		}
		slot := i % len(mask)
		material[i] = previous ^ (mask[slot] ^ byte(90+13*slot) ^ 85) ^ bits.RotateLeft8(byte(int(b)+215-11*i), 3)
	}
	encrypted, err := decodeDownloadBase64(parts[2])
	if err != nil || len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return nil, invalid
	}
	block, err := aes.NewCipher(material[:16])
	if err != nil {
		return nil, invalid
	}
	plain := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, material[16:32]).CryptBlocks(plain, encrypted)
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > 16 || padding > len(plain) {
		return nil, invalid
	}
	for _, b := range plain[len(plain)-padding:] {
		if int(b) != padding {
			return nil, invalid
		}
	}
	return plain[:len(plain)-padding], nil
}

func decodeDownloadKey(value string) ([]byte, error) {
	invalid := errors.New("媒体密钥格式或版本不支持")
	if len(value) > 1024 {
		return nil, invalid
	}
	raw, err := decodeDownloadBase64(value)
	if err != nil || len(raw) < 3 {
		return nil, invalid
	}
	tagLen := int(raw[0]^raw[1]^raw[2]) - 48
	contentLen := len(raw) - tagLen - 1
	if tagLen < 1 || contentLen < 33 || contentLen >= len(raw) {
		return nil, invalid
	}
	seed := raw[len(raw)-tagLen-2] ^ raw[len(raw)-tagLen-1]
	tag := make([]byte, tagLen)
	for i := range tag {
		tag[i] = raw[len(raw)-tagLen+i] ^ seed
	}
	if string(tag) == "app_v2" || string(tag) == "web_v2" {
		return nil, invalid
	}
	decoded := make([]byte, contentLen)
	previous := [2]byte{250, 85}
	for i, b := range raw[1 : 1+contentLen] {
		decoded[i] = byte(int(previous[i%2]^b) - 21 - bits.OnesCount(uint(i)))
		previous[i%2] = b
	}
	padding, err := strconv.ParseUint(string(decoded[:1]), 36, 8)
	if err != nil || contentLen-int(padding)-1 != 32 {
		return nil, invalid
	}
	key, err := hex.DecodeString(string(decoded[1:33]))
	if err != nil || len(key) != 16 {
		return nil, invalid
	}
	return key, nil
}
