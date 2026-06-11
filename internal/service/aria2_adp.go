// Package service — Aria2 下载适配器。
//
// Aria2Adapter 实现了 DownloadAdapter 接口，通过 Aria2 JSON-RPC API
// 管理下载任务。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// aria2Request 是 Aria2 JSON-RPC 请求结构。
type aria2Request struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	ID      string        `json:"id"`
	Params  []interface{} `json:"params"`
}

// aria2Response 是 Aria2 JSON-RPC 响应结构。
type aria2Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *aria2Error     `json:"error"`
}

// aria2Error 是 Aria2 JSON-RPC 错误结构。
type aria2Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Aria2Adapter 是 Aria2 的 DownloadAdapter 实现。
type Aria2Adapter struct {
	mu     sync.Mutex
	cfg    DownloadClientConfig
	client *http.Client
	idSeq  int
}

// NewAria2Adapter 创建新的 Aria2 适配器。
func NewAria2Adapter() *Aria2Adapter {
	return &Aria2Adapter{
		client: NewInternalHTTPClient(20 * time.Second),
	}
}

// Initialize 配置并初始化 Aria2 RPC 连接。
func (a *Aria2Adapter) Initialize(ctx context.Context, cfg DownloadClientConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	endpoint, err := normalizeDownloadClientEndpoint("aria2", cfg.Host)
	if err != nil {
		return err
	}
	cfg.Host = endpoint
	a.cfg = cfg
	a.idSeq = 0
	return a.getVersionLocked(ctx)
}

// Ping 测试连接。
func (a *Aria2Adapter) Ping(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.getVersionLocked(ctx)
}

// getVersionLocked 内部版本检查（调用者必须持有锁）。
func (a *Aria2Adapter) getVersionLocked(ctx context.Context) error {
	rpcURL, err := downloadClientRPCURL("aria2", a.cfg.Host)
	if err != nil {
		return err
	}

	req := &aria2Request{
		JSONRPC: "2.0",
		Method:  "aria2.getVersion",
		ID:      a.nextID(),
		Params:  []interface{}{"token:" + a.cfg.Password},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := newDownloadClientHTTPRequest(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.cfg.Username != "" {
		httpReq.SetBasicAuth(a.cfg.Username, a.cfg.Password)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("aria2 rpc: %d", resp.StatusCode)
	}
	return nil
}

// rpcLocked 发送 JSON-RPC 请求（调用者必须持有锁）。
func (a *Aria2Adapter) rpcLocked(ctx context.Context, method string, params []interface{}) (json.RawMessage, error) {
	rpcURL, err := downloadClientRPCURL("aria2", a.cfg.Host)
	if err != nil {
		return nil, err
	}

	if params == nil {
		params = []interface{}{}
	}

	// 如果 secret 不在 params 中，添加到第一位
	if len(params) > 0 {
		if secret, ok := params[0].(string); ok && strings.HasPrefix(secret, "token:") {
			// 已经有 secret
		} else {
			newParams := make([]interface{}, 0, len(params)+1)
			newParams = append(newParams, "token:"+a.cfg.Password)
			newParams = append(newParams, params...)
			params = newParams
		}
	} else {
		params = []interface{}{"token:" + a.cfg.Password}
	}

	req := &aria2Request{
		JSONRPC: "2.0",
		Method:  method,
		ID:      a.nextID(),
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := newDownloadClientHTTPRequest(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.cfg.Username != "" {
		httpReq.SetBasicAuth(a.cfg.Username, a.cfg.Password)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp aria2Response
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("aria2 rpc error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// AddTorrent 通过 URL 添加种子或磁力链接。
func (a *Aria2Adapter) AddTorrent(ctx context.Context, torrentURL, savePath string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Aria2 addUri 的参数: [secret, [uris], options]
	uris := []string{torrentURL}
	options := map[string]string{}
	if savePath != "" {
		options["dir"] = savePath
	}

	result, err := a.rpcLocked(ctx, "aria2.addUri", []interface{}{uris, options})
	if err != nil {
		return "", err
	}
	var gid string
	if err := json.Unmarshal(result, &gid); err != nil {
		return "", err
	}
	return gid, nil
}

// AddMagnet 通过磁力链接添加下载。
func (a *Aria2Adapter) AddMagnet(ctx context.Context, magnet, savePath string) (string, error) {
	return a.AddTorrent(ctx, magnet, savePath)
}

// Pause 暂停下载任务（通过 GID）。
func (a *Aria2Adapter) Pause(ctx context.Context, hash string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.rpcLocked(ctx, "aria2.pause", []interface{}{hash})
	return err
}

// Resume 恢复下载任务（通过 GID）。
func (a *Aria2Adapter) Resume(ctx context.Context, hash string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.rpcLocked(ctx, "aria2.unpause", []interface{}{hash})
	return err
}

// Remove 移除下载任务。
func (a *Aria2Adapter) Remove(ctx context.Context, hash string, deleteFiles bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if deleteFiles {
		_, err := a.rpcLocked(ctx, "aria2.removeDownloadResult", []interface{}{hash})
		return err
	}
	_, err := a.rpcLocked(ctx, "aria2.remove", []interface{}{hash})
	return err
}

// List 列出所有活动/等待/已停止的任务。
func (a *Aria2Adapter) List(ctx context.Context, filter string) ([]TorrentInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var allResults []TorrentInfo

	// 获取活动任务
	active, err := a.rpcLocked(ctx, "aria2.tellActive", []interface{}{
		[]string{"gid", "bittorrent", "totalLength", "completedLength", "downloadSpeed", "uploadSpeed", "status", "dir", "numSeeders", "connections", "errorCode"},
	})
	if err == nil && active != nil {
		items := a.parseAria2Items(active)
		allResults = append(allResults, items...)
	}

	// 获取等待中的任务
	waiting, err := a.rpcLocked(ctx, "aria2.tellWaiting", []interface{}{
		0, 100,
		[]string{"gid", "bittorrent", "totalLength", "completedLength", "downloadSpeed", "uploadSpeed", "status", "dir", "numSeeders", "connections", "errorCode"},
	})
	if err == nil && waiting != nil {
		items := a.parseAria2Items(waiting)
		allResults = append(allResults, items...)
	}

	// 获取已停止的任务
	stopped, err := a.rpcLocked(ctx, "aria2.tellStopped", []interface{}{
		0, 100,
		[]string{"gid", "bittorrent", "totalLength", "completedLength", "downloadSpeed", "uploadSpeed", "status", "dir", "numSeeders", "connections", "errorCode"},
	})
	if err == nil && stopped != nil {
		items := a.parseAria2Items(stopped)
		allResults = append(allResults, items...)
	}

	if filter != "" {
		filtered := make([]TorrentInfo, 0, len(allResults))
		for _, item := range allResults {
			if strings.EqualFold(item.State, filter) {
				filtered = append(filtered, item)
			}
		}
		return filtered, nil
	}

	return allResults, nil
}

// GetInfo 获取单个任务信息。
func (a *Aria2Adapter) GetInfo(ctx context.Context, hash string) (*TorrentInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	result, err := a.rpcLocked(ctx, "aria2.tellStatus", []interface{}{
		hash,
		[]string{"gid", "bittorrent", "totalLength", "completedLength", "downloadSpeed", "uploadSpeed", "status", "dir", "numSeeders", "connections"},
	})
	if err != nil {
		return nil, err
	}

	var item map[string]interface{}
	if err := json.Unmarshal(result, &item); err != nil {
		return nil, err
	}

	info := a.parseSingleItem(item)
	if info == nil {
		return nil, fmt.Errorf("task %s not found", hash)
	}
	return info, nil
}

// parseAria2Items 解析 Aria2 返回的任务列表。
func (a *Aria2Adapter) parseAria2Items(raw json.RawMessage) []TorrentInfo {
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}

	result := make([]TorrentInfo, 0, len(items))
	for _, item := range items {
		info := a.parseSingleItem(item)
		if info != nil {
			result = append(result, *info)
		}
	}
	return result
}

// parseSingleItem 解析单个 Aria2 任务项。
func (a *Aria2Adapter) parseSingleItem(item map[string]interface{}) *TorrentInfo {
	gid := strVal(item["gid"])
	totalLength := toInt64(item["totalLength"])
	completedLength := toInt64(item["completedLength"])
	dlSpeed := toInt64(item["downloadSpeed"])
	upSpeed := toInt64(item["uploadSpeed"])
	status := strVal(item["status"])
	dir := strVal(item["dir"])
	numSeeders := int(toInt64(item["numSeeders"]))
	connections := int(toInt64(item["connections"]))

	var name string
	var hash string

	// 尝试从 bittorrent info 获取名称和 hash
	if bt, ok := item["bittorrent"].(map[string]interface{}); ok {
		if info, ok := bt["info"].(map[string]interface{}); ok {
			name = strVal(info["name"])
		}
		hash = strVal(bt["infoHash"])
	}

	// 如果没有 bittorrent 信息，使用 GID 作为 hash
	if hash == "" {
		hash = gid
	}
	if name == "" {
		// 尝试从 files 获取文件名
		if files, ok := item["files"].([]interface{}); ok && len(files) > 0 {
			if f, ok := files[0].(map[string]interface{}); ok {
				paths, ok := f["path"].([]interface{})
				if ok && len(paths) > 0 {
					name = strVal(paths[len(paths)-1])
				}
				if name == "" {
					name = strVal(f["uris"])
				}
			}
		}
	}
	if name == "" {
		name = gid
	}

	var progress float64
	if totalLength > 0 {
		progress = float64(completedLength) / float64(totalLength) * 100
	}

	// Aria2 状态映射
	state := aria2StatusStr(status)

	return &TorrentInfo{
		Hash:      hash,
		Name:      name,
		Size:      totalLength,
		Progress:  progress,
		DLSpeed:   dlSpeed,
		UPSpeed:   upSpeed,
		State:     state,
		SavePath:  dir,
		NumSeeds:  numSeeders,
		NumLeechs: max(connections-numSeeders, 0),
		AddedOn:   time.Now(),
	}
}

// aria2StatusStr 将 Aria2 状态转为可读字符串。
func aria2StatusStr(status string) string {
	switch status {
	case "active":
		return "downloading"
	case "waiting":
		return "queued"
	case "paused":
		return "paused"
	case "error":
		return "error"
	case "complete":
		return "seeding"
	case "removed":
		return "removed"
	default:
		return status
	}
}

// nextID 生成递增的请求 ID。
func (a *Aria2Adapter) nextID() string {
	a.idSeq++
	return fmt.Sprintf("msg-%d", a.idSeq)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
