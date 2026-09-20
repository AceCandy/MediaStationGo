package hongguo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Album 是官方跨季关系；空 ID 表示成功查询后没有有效合集。
type Album struct {
	ID     string
	Season int
}

func (c *Client) Album(ctx context.Context, sourceID string) (Album, error) {
	if !ValidID(sourceID) {
		return Album{}, errors.New("红果作品 ID 无效")
	}
	body, err := c.appRequest(ctx, "https://api5-normal-sinfonlineb.fqnovel.com/novel/player/video_detail/v1/", map[string]string{"series_id": sourceID})
	if err != nil {
		return Album{}, err
	}
	return parseAlbum(body, sourceID)
}

func parseAlbum(body []byte, sourceID string) (Album, error) {
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&root) != nil || root == nil {
		return Album{}, errors.New("红果官方合集响应 JSON 无效")
	}
	var tail any
	if dec.Decode(&tail) != io.EOF {
		return Album{}, errors.New("红果官方合集响应不完整")
	}
	if code := scalar(root["code"]); code != "0" {
		// 已下架的作品按自身 ID 保留独立第一季，不再等待官方合集。
		if code == "101001" {
			return Album{ID: sourceID, Season: 1}, nil
		}
		if value, err := strconv.ParseInt(code, 10, 32); err == nil && value != 0 {
			return Album{}, fmt.Errorf("红果官方合集业务错误（code=%d）", value)
		}
		return Album{}, errors.New("红果官方合集业务码 code 缺失或无效")
	}
	v := object(object(root["data"])["video_data"])
	id := scalar(v["series_id_str"])
	if id == "" {
		id = scalar(v["series_id"])
	}
	if id != sourceID {
		return Album{}, errors.New("红果官方合集作品身份不匹配")
	}
	albumID := scalar(v["related_album_id"])
	if v["related_album_id"] != nil && albumID == "" && v["related_album_id"] != "" {
		return Album{}, errors.New("红果官方合集 related_album_id 无效")
	}
	if albumID == "" || albumID == "0" {
		return Album{}, nil
	}
	if !ValidID(albumID) {
		return Album{}, errors.New("红果官方合集 related_album_id 无效")
	}
	season, err := strconv.Atoi(scalar(v["season_index"]))
	if errors.Is(err, strconv.ErrRange) {
		return Album{}, errors.New("红果官方合集 season_index 超出有效范围 1–100000")
	}
	if err != nil {
		// 官方未提供可解析的季号时，以第一季展示已有合集关系。
		season = 1
	}
	if season < 1 || season > 100000 {
		return Album{}, fmt.Errorf("红果官方合集 season_index=%d 超出有效范围 1–100000", season)
	}
	return Album{ID: albumID, Season: season}, nil
}
