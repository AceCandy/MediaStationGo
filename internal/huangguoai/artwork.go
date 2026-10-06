package huangguoai

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var imageParameter = regexp.MustCompile(`\b(media_key|media_iv)\s*:\s*["']([0-9a-f_]+)["']`)

// DecodeArtwork implements the website's AES-CBC image transport, with runtime-only parameters.
func DecodeArtwork(data, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil || len(iv) != aes.BlockSize || len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("黄果 AI 图片加密数据无效")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return out, nil
}

func imageParameters(script []byte) ([]byte, []byte, error) {
	if !regexp.MustCompile(`mode\s*:\s*["']CBC["']`).Match(script) {
		return nil, nil, errors.New("黄果 AI 图片模式未支持")
	}
	values := map[string][]byte{}
	for _, m := range imageParameter.FindAllSubmatch(script, -1) {
		raw := string(m[2])
		var value []byte
		for _, part := range strings.Split(raw, "_") {
			n, e := strconv.ParseUint(part, 10, 8)
			if e != nil {
				return nil, nil, errors.New("黄果 AI 图片协议参数无效")
			}
			value = append(value, byte(n))
		}
		if values[string(m[1])] != nil {
			return nil, nil, errors.New("黄果 AI 图片协议参数重复")
		}
		values[string(m[1])] = value
	}
	key, iv := values["media_key"], values["media_iv"]
	if (len(key) != 16 && len(key) != 24 && len(key) != 32) || len(iv) != 16 {
		return nil, nil, errors.New("黄果 AI 图片协议结构未确认")
	}
	return key, iv, nil
}

// Artwork fetches bounded bytes through the same guarded transport used for all source resources.
func (c *Client) Artwork(ctx context.Context, raw string) ([]byte, error) {
	data, _, err := c.request(ctx, raw, c.base+"/", 10<<20)
	if err != nil {
		return nil, err
	}
	// Normal unencrypted posters are also supported; callers validate the decoded image and dimensions.
	if len(data) >= 3 && (string(data[:3]) == "\xff\xd8\xff" || (len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n") || string(data[:3]) == "GIF" || (len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP")) {
		return data, nil
	}
	script, _, err := c.request(ctx, c.base+"/static/web/js/plugins/crypto-worker.js", c.base+"/", 2<<20)
	if err != nil {
		return nil, err
	}
	key, iv, err := imageParameters(script)
	if err != nil {
		return nil, err
	}
	return DecodeArtwork(data, key, iv)
}
