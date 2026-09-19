package hongguo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	if dec.Decode(&root) != nil || scalar(root["code"]) != "0" {
		return Album{}, errors.New("红果官方合集响应无效")
	}
	var tail any
	if dec.Decode(&tail) != io.EOF {
		return Album{}, errors.New("红果官方合集响应不完整")
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
		return Album{}, errors.New("红果官方合集 ID 无效")
	}
	if albumID == "" || albumID == "0" {
		return Album{}, nil
	}
	season, err := strconv.Atoi(scalar(v["season_index"]))
	if !ValidID(albumID) || err != nil || season < 1 || season > 100000 {
		return Album{}, errors.New("红果官方合集 ID 或季号无效")
	}
	return Album{ID: albumID, Season: season}, nil
}
