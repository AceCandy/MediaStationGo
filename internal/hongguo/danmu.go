package hongguo

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const danmuAppOrigin = "https://api5-normal-sinfonlinea.fqnovel.com"

// Danmu 是来源弹幕的最小投影，不保留发送者身份和原始响应。
type Danmu struct {
	ID        string
	OffsetMS  int64
	Content   string
	CreatedAt int64
}

// Danmus 只拉取当前源作品的指定集；失败时返回此前成功窗口，不修改资料映射。
func (c *Client) Danmus(ctx context.Context, sourceID string, episode int, videoID string, app DanmuAppConfig) ([]Danmu, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if !ValidID(sourceID) || episode < 1 {
		return nil, errors.New("红果弹幕坐标无效")
	}
	if err := app.Validate(); err != nil {
		return nil, err
	}
	query, err := danmuQuery(app)
	if err != nil {
		return nil, err
	}
	resolve := func() error {
		work, err := c.Detail(ctx, sourceID)
		if err != nil {
			return err
		}
		if episode > len(work.VideoIDs) || !ValidID(work.VideoIDs[episode-1]) {
			return errors.New("红果分集映射不存在")
		}
		videoID = work.VideoIDs[episode-1]
		return nil
	}
	if !ValidID(videoID) {
		if err := resolve(); err != nil {
			return nil, err
		}
	}
	duration, err := c.danmuDuration(ctx, query, app, videoID)
	if errors.Is(err, ErrVideoTakenDown) {
		if err = resolve(); err == nil {
			duration, err = c.danmuDuration(ctx, query, app, videoID)
		}
	}
	if err != nil {
		return nil, err
	}
	var rows []Danmu
	seen := make(map[string]bool)
	start, cursor := int64(0), ""
	aid, _ := strconv.Atoi(query.Get("aid"))
	for page := 0; page < 128; page++ {
		if page > 0 {
			timer := time.NewTimer(180 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return rows, ctx.Err()
			case <-timer.C:
			}
		}
		body := map[string]any{"comment_source": 601, "server_channel": 1000, "group_id": videoID, "group_type": 30, "comment_type": 20, "sort": 1, "business_param": map[string]any{"book_id": sourceID, "start_offset_time": start, "playlet_item_duration": duration, "need_danmaku_guide_type": []int{}}, "count": 90, "cursor": cursor, "aid": aid, "compliance_status": 0}
		data, err := c.danmuRequest(ctx, "/novel/commentapi/comment/list/"+videoID+"/v1/", query, app, body, true)
		if err != nil {
			return rows, err
		}
		list, ok := data["data_list"].([]any)
		if !ok && data["data_list"] != nil {
			return rows, errors.New("红果弹幕列表格式无效")
		}
		if len(list) > 4096 {
			return rows, errors.New("红果弹幕窗口过大")
		}
		extra, info := object(data["extra"]), object(data["common_list_info"])
		next := start + 30000
		if value, exists := extra["next_query_danmaku_list_time"]; exists {
			next, err = strconv.ParseInt(scalar(value), 10, 64)
			if err != nil || next <= start || next > 86400000 {
				return rows, errors.New("红果弹幕时间窗口无效")
			}
		}
		for _, raw := range list {
			comment := object(object(raw)["comment"])
			common := object(comment["common"])
			expand := object(comment["expand"])
			id := scalar(comment["comment_id"])
			offset, err := strconv.ParseInt(scalar(expand["offset_time"]), 10, 64)
			// PostgreSQL text 和 XML 均不接受 NUL，不能让一条异常内容阻止整批新增。
			text := strings.ReplaceAll(scalar(object(common["content"])["text"]), "\x00", "")
			if seen[id] || !ValidID(id) || err != nil || offset < start || offset >= min(next, duration) || strings.TrimSpace(text) == "" || len(text) > 16384 {
				continue
			}
			// 兼容省略身份或状态的响应；明确提供时必须属于当前集且为公开状态。
			if group, exists := common["group_id"]; exists && scalar(group) != videoID {
				continue
			}
			if status, exists := common["status"]; exists && scalar(status) != "1" {
				continue
			}
			seen[id] = true
			created, _ := strconv.ParseInt(scalar(common["create_timestamp"]), 10, 64)
			rows = append(rows, Danmu{ID: id, OffsetMS: offset, Content: text, CreatedAt: max(0, created)})
		}
		if len(rows) > 20000 {
			return rows[:20000], errors.New("红果弹幕达到本次条数上限")
		}
		if next >= duration {
			return rows, nil
		}
		nextCursor := scalar(info["cursor"])
		if cursor != "" && nextCursor == cursor {
			return rows, errors.New("红果弹幕游标未前进")
		}
		start, cursor = next, nextCursor
	}
	return rows, errors.New("红果弹幕达到本次窗口上限")
}

func danmuQuery(app DanmuAppConfig) (url.Values, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, errors.New("红果临时身份生成失败")
	}
	q := url.Values{"aid": {"8662"}, "app_name": {"novelread"}, "version_code": {"68132"}, "version_name": {"6.8.1.32"}, "manifest_version_code": {"68132"}, "update_version_code": {"68132"}, "channel": {"update_64"}, "device_platform": {"android"}, "os": {"android"}, "ssmix": {"a"}, "device_type": {"25053RT47C"}, "device_brand": {"Redmi"}, "language": {"zh"}, "os_api": {"36"}, "os_version": {"16"}, "resolution": {"1280*2772"}, "dpi": {"520"}, "ac": {"wifi"}}
	for key, value := range app.Query {
		q.Set(key, value)
	}
	q.Set("device_id", app.DeviceID)
	q.Set("iid", app.IID)
	for n, key := range []string{"device_id", "iid"} {
		if q.Get(key) == "" {
			q.Set(key, strconv.FormatUint(1_000_000_000_000_000_000+binary.BigEndian.Uint64(random[n*8:])%8_000_000_000_000_000_000, 10))
		}
	}
	return q, nil
}

func (c *Client) danmuDuration(ctx context.Context, q url.Values, app DanmuAppConfig, video string) (int64, error) {
	data, err := c.danmuRequest(ctx, "/novel/player/video_model/v1/", q, app, map[string]any{"video_id": video, "content_type": 1, "biz_param": map[string]any{"video_platform": 3}}, false)
	if err != nil {
		return 0, err
	}
	m := object(data["video_model"])
	if raw, ok := data["video_model"].(string); ok {
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&m) != nil {
			return 0, errors.New("红果时长格式无效")
		}
	}
	seconds, err := strconv.ParseFloat(scalar(m["video_duration"]), 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 86400 {
		return 0, errors.New("红果时长无效")
	}
	return int64(math.Round(seconds * 1000)), nil
}

func (c *Client) danmuRequest(ctx context.Context, path string, q url.Values, app DanmuAppConfig, payload any, comment bool) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("红果请求格式无效")
	}
	now := time.Now()
	q.Set("_rticket", strconv.FormatInt(now.UnixMilli(), 10))
	q.Set("ts", strconv.FormatInt(now.Unix(), 10))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, danmuAppOrigin+path+"?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("红果请求无效")
	}
	ua := app.UserAgent
	if ua == "" {
		ua = downloadAppUserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	if app.Cookie != "" {
		req.Header.Set("Cookie", app.Cookie)
	}
	if app.Token != "" {
		req.Header.Set("X-Tt-Token", app.Token)
	}
	var stub []byte
	if comment {
		req.Header.Set("Comment-Source", "601")
		req.Header.Set("Server-Channel", "1000")
		req.Header.Set("X-SS-STUB", "")
	} else {
		hash := md5.Sum(body)
		stub = hash[:]
		req.Header.Set("X-SS-STUB", fmt.Sprintf("%X", hash))
	}
	if err := signDanmuRequest(req, stub, now); err != nil {
		return nil, errors.New("红果请求签名失败")
	}
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("红果弹幕网络请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("红果弹幕 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, errors.New("红果弹幕响应读取失败或过大")
	}
	var result map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&result) != nil || result == nil {
		return nil, errors.New("红果弹幕响应格式无效")
	}
	for _, v := range []any{result["code"], result["status_code"], object(result["BaseResp"])["StatusCode"]} {
		code := scalar(v)
		if code == "101002" {
			return nil, ErrVideoTakenDown
		}
		if code != "" && code != "0" {
			return nil, errors.New("红果弹幕业务请求失败")
		}
	}
	data := object(result["data"])
	if data == nil {
		return nil, errors.New("红果弹幕响应缺少数据")
	}
	return data, nil
}
