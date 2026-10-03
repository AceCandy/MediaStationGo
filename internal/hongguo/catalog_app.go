package hongguo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

const appCatalogURL = "https://api5-normal-sinfonlineb.fqnovel.com/reading/distribution/category/landpage/v/"

// ScanAppCatalog 逐页读取一个官方分类；每页保存成功后才前进，匿名身份仅存在于本次扫描。
func (c *Client) ScanAppCatalog(ctx context.Context, category string, save func([]Work) error) error {
	genre, scene := "", ""
	switch category {
	case "real-drama":
		genre, scene = "short_play", "default"
	case "comic-drama":
		genre, scene = "comic_series", "comic_series"
	case "ai-drama":
		genre, scene = "ai_series", "ai_series"
	default:
		return errors.New("红果 App 分类无效")
	}
	identity, err := danmuQuery(DanmuAppConfig{})
	if err != nil {
		return err
	}
	offset, session := 0, ""
	previousPage := ""
	seen := map[string]bool{}
	for page := 0; page < MaxCategoryPage; page++ {
		if page > 0 {
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		requestType := 3
		if offset > 0 {
			requestType = 2
		}
		payload := map[string]any{"req_scene": scene, "offset": offset, "limit": 18, "req_type": "only_content", "need_selector_panel": false, "client_req_type": requestType, "session_id": session, "filter_ids": "", "select_items": map[string]any{"genre": []string{genre}, "sort": []string{"online_time"}, "gender": []string{}, "category_dim_theme": []string{}, "category_dim_role": []string{}, "category_dim_epoch": []string{}, "online_time": []string{}, "creation_status": []string{}}}
		var works []Work
		var next int
		var nextSession, signature string
		var more bool
		for recovery := 0; recovery < 2; recovery++ {
			var raw []byte
			for attempt := 0; attempt < 3; attempt++ {
				if attempt > 0 {
					timer := time.NewTimer(time.Duration(attempt) * time.Second)
					select {
					case <-ctx.Done():
						timer.Stop()
						return ctx.Err()
					case <-timer.C:
					}
				}
				raw, err = c.appRequestWithIdentity(ctx, appCatalogURL, payload, identity.Get("device_id"), identity.Get("iid"))
				if err == nil {
					break
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			if err != nil {
				return err
			}
			works, next, nextSession, more, err = parseAppCatalog(raw, offset)
			if err == nil {
				ids := make([]string, 0, len(works))
				for _, w := range works {
					ids = append(ids, w.SourceID)
				}
				sort.Strings(ids)
				signature = strings.Join(ids, ",")
				if more && signature == previousPage {
					err = errors.New("红果 App 目录分页重复，未确认扫描完成")
				}
			}
			if err == nil {
				break
			}
			if recovery == 1 {
				return err
			}
			payload["session_id"] = ""
		}
		fresh := make([]Work, 0, len(works))
		for _, work := range works {
			if !seen[work.SourceID] {
				seen[work.SourceID] = true
				fresh = append(fresh, work)
			}
		}
		previousPage = signature

		if err := save(fresh); err != nil {
			return err
		}
		if !more {
			return nil
		}
		offset, session = next, nextSession
	}
	return errors.New("红果 App 目录达到分页上限，未确认扫描完成")
}

// parseAppCatalog 校验接口分页证据；不足请求条数不能用于推断 App 分类结束。
func parseAppCatalog(raw []byte, offset int) ([]Work, int, string, bool, error) {
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil {
		return nil, 0, "", false, errors.New("红果 App 目录响应无效")
	}
	for _, code := range []any{result["code"], result["status_code"], object(result["BaseResp"])["StatusCode"]} {
		if s := scalar(code); s != "" && s != "0" {
			return nil, 0, "", false, errors.New("红果 App 目录业务请求失败")
		}
	}
	data := object(result["data"])
	rows, ok := data["video_data"].([]any)
	more, paginationOK := data["has_more"].(bool)
	if !ok || !paginationOK {
		return nil, 0, "", false, errors.New("红果 App 目录缺少分页数据")
	}
	next, err := strconv.Atoi(scalar(data["next_offset"]))
	if err != nil {
		if more {
			return nil, 0, "", false, errors.New("红果 App 目录游标无效")
		}
		next = offset
	}
	if more && (next <= offset || next > 1000000) {
		return nil, 0, "", false, errors.New("红果 App 目录游标未前进")
	}
	// 到末页后服务端可能把 next_offset 归零；只在仍有下一页时要求前进。
	if !more && (next < offset || next > 1000000) {
		next = offset
	}
	session := scalar(data["session_id"])
	if len(session) > 4096 || strings.ContainsAny(session, "\r\n\x00") {
		return nil, 0, "", false, errors.New("红果 App 目录会话无效")
	}
	works := make([]Work, 0, len(rows))
	for _, raw := range rows {
		row := object(raw)
		video := object(row["video_data"])
		if len(video) == 0 {
			video = row
		}
		id, title, cover, overview := "", "", "", ""
		for _, value := range []any{video["series_id_str"], video["series_id"], row["series_id_str"], row["series_id"], video["keyword"], row["keyword"]} {
			if id = scalar(value); id != "" {
				break
			}
		}
		for _, value := range []any{video["series_title"], video["series_name"], video["title"], row["series_name"], row["name"]} {
			if title = scalar(value); title != "" {
				break
			}
		}
		for _, value := range []any{video["series_cover"], video["cover"], row["series_cover"]} {
			if cover = scalar(value); cover != "" {
				break
			}
		}
		for _, value := range []any{video["series_intro"], video["video_desc"]} {
			if overview = scalar(value); overview != "" {
				break
			}
		}
		if !ValidID(id) || title == "" {
			continue
		}
		works = append(works, Work{SourceID: id, Title: title, Overview: overview, CoverURL: cover, EpisodeCount: count(video["episode_cnt"]), UpdateText: scalar(video["episode_right_text"])})
	}
	if len(rows) > 0 && len(works) == 0 || more && len(works) == 0 {
		return nil, 0, "", false, errors.New("红果 App 目录未返回可识别作品")
	}
	return works, next, session, more, nil
}
