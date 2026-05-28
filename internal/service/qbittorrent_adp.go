// Package service — qBittorrent 下载适配器。
//
// QBitAdapter 实现了 DownloadAdapter 接口，通过 qBittorrent WebUI API
// 管理下载任务。底层使用与 QBitClient 相同的 HTTP API 调用逻辑。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// QBitAdapter 是 qBittorrent 的 DownloadAdapter 实现。
type QBitAdapter struct {
	mu       sync.Mutex
	cfg      DownloadClientConfig
	client   *http.Client
	LoggedIn bool
}

// NewQBitAdapter 创建新的 qBittorrent 适配器。
func NewQBitAdapter() *QBitAdapter {
	jar, _ := cookiejar.New(nil)
	return &QBitAdapter{
		client: &http.Client{Jar: jar, Timeout: 20 * time.Second},
	}
}

// Initialize 配置并初始化 qBittorrent 连接。
func (a *QBitAdapter) Initialize(ctx context.Context, cfg DownloadClientConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg = cfg
	a.LoggedIn = false
	jar, _ := cookiejar.New(nil)
	a.client.Jar = jar
	return a.loginLocked(ctx)
}

// Ping 测试连接。
func (a *QBitAdapter) Ping(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loginLocked(ctx)
}

// AddTorrent 通过 URL 添加种子。
func (a *QBitAdapter) AddTorrent(ctx context.Context, torrentURL, savePath string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ensureAuthLocked(ctx); err != nil {
		return "", err
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("urls", torrentURL)
	if savePath != "" {
		_ = w.WriteField("savepath", savePath)
	}
	_ = w.Close()

	baseURL := strings.TrimRight(a.cfg.Host, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v2/torrents/add", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Referer", baseURL)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("qbittorrent add torrent: %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return "", nil
}

// AddMagnet 通过磁力链接添加种子。
func (a *QBitAdapter) AddMagnet(ctx context.Context, magnet, savePath string) (string, error) {
	return a.AddTorrent(ctx, magnet, savePath)
}

// Pause 暂停种子。
func (a *QBitAdapter) Pause(ctx context.Context, hash string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ensureAuthLocked(ctx); err != nil {
		return err
	}
	baseURL := strings.TrimRight(a.cfg.Host, "/")
	form := url.Values{}
	form.Set("hashes", hash)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v2/torrents/pause", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", baseURL)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("qbittorrent pause: %d", resp.StatusCode)
	}
	return nil
}

// Resume 恢复种子。
func (a *QBitAdapter) Resume(ctx context.Context, hash string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ensureAuthLocked(ctx); err != nil {
		return err
	}
	baseURL := strings.TrimRight(a.cfg.Host, "/")
	form := url.Values{}
	form.Set("hashes", hash)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v2/torrents/resume", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", baseURL)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("qbittorrent resume: %d", resp.StatusCode)
	}
	return nil
}

// Remove 删除种子。
func (a *QBitAdapter) Remove(ctx context.Context, hash string, deleteFiles bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ensureAuthLocked(ctx); err != nil {
		return err
	}
	baseURL := strings.TrimRight(a.cfg.Host, "/")
	form := url.Values{}
	form.Set("hashes", hash)
	if deleteFiles {
		form.Set("deleteFiles", "true")
	} else {
		form.Set("deleteFiles", "false")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v2/torrents/delete", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", baseURL)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("qbittorrent delete: %d", resp.StatusCode)
	}
	return nil
}

// List 列出种子。
func (a *QBitAdapter) List(ctx context.Context, filter string) ([]TorrentInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ensureAuthLocked(ctx); err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(a.cfg.Host, "/")
	u := baseURL + "/api/v2/torrents/info"
	if filter != "" {
		u += "?filter=" + url.QueryEscape(filter)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", baseURL)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("qbittorrent list: %d", resp.StatusCode)
	}

	// qBittorrent 返回的字段名与 TorrentInfo 不同，需要转换
	type qbTorrent struct {
		Hash      string  `json:"hash"`
		Name      string  `json:"name"`
		State     string  `json:"state"`
		Progress  float32 `json:"progress"`
		DLSpeed   int64   `json:"dlspeed"`
		UPSpeed   int64   `json:"upspeed"`
		NumSeeds  int     `json:"num_seeds"`
		NumLeechs int     `json:"num_leechs"`
		Size      int64   `json:"size"`
		SavePath  string  `json:"save_path"`
		AddedOn   int64   `json:"added_on"`
		Category  string  `json:"category"`
		Tags      string  `json:"tags"`
	}

	var qbList []qbTorrent
	if err := json.NewDecoder(resp.Body).Decode(&qbList); err != nil {
		return nil, err
	}

	result := make([]TorrentInfo, 0, len(qbList))
	for _, t := range qbList {
		result = append(result, TorrentInfo{
			Hash:      t.Hash,
			Name:      t.Name,
			Size:      t.Size,
			Progress:  float64(t.Progress),
			DLSpeed:   t.DLSpeed,
			UPSpeed:   t.UPSpeed,
			State:     t.State,
			SavePath:  t.SavePath,
			NumSeeds:  t.NumSeeds,
			NumLeechs: t.NumLeechs,
			AddedOn:   time.Unix(t.AddedOn, 0),
			Category:  t.Category,
			Tags:      t.Tags,
		})
	}
	return result, nil
}

// GetInfo 获取单个种子信息。
func (a *QBitAdapter) GetInfo(ctx context.Context, hash string) (*TorrentInfo, error) {
	list, err := a.List(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if t.Hash == hash {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("torrent %s not found", hash)
}

// loginLocked 执行登录（调用者必须持有锁）。
func (a *QBitAdapter) loginLocked(ctx context.Context) error {
	if a.cfg.Host == "" {
		return fmt.Errorf("qbittorrent host not configured")
	}
	form := url.Values{}
	form.Set("username", a.cfg.Username)
	form.Set("password", a.cfg.Password)
	baseURL := strings.TrimRight(a.cfg.Host, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", baseURL)
	req.Header.Set("Origin", baseURL)
	req.Header.Set("User-Agent", "MediaStationGo/0.1")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := strings.TrimSpace(string(body))
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("qbittorrent login forbidden: check username/password, WebUI IP ban, CSRF/Host header validation, and allowed subnets")
	}
	if resp.StatusCode >= 400 || text != "Ok." {
		return fmt.Errorf("qbittorrent login failed: status=%d body=%s", resp.StatusCode, text)
	}
	a.LoggedIn = true
	return nil
}

// ensureAuthLocked 确保已认证（调用者必须持有锁）。
func (a *QBitAdapter) ensureAuthLocked(ctx context.Context) error {
	if a.LoggedIn {
		return nil
	}
	return a.loginLocked(ctx)
}

// --- 为了与现有的 QBitClient 兼容，添加转换辅助函数 ---

// QBitTorrentToInfo 将旧的 QBitTorrent 转换为新的 TorrentInfo。
func QBitTorrentToInfo(q QBitTorrent) TorrentInfo {
	return TorrentInfo{
		Hash:      q.Hash,
		Name:      q.Name,
		Size:      q.Size,
		Progress:  float64(q.Progress),
		DLSpeed:   q.DLSpeed,
		UPSpeed:   q.UpSpeed,
		State:     q.State,
		SavePath:  q.SavePath,
		NumSeeds:  q.NumSeeds,
		NumLeechs: q.NumLeech,
	}
}

// TorrentInfoToQBit 将 TorrentInfo 转换回旧的 QBitTorrent 格式（兼容性）。
func TorrentInfoToQBit(t TorrentInfo) QBitTorrent {
	return QBitTorrent{
		Hash:     t.Hash,
		Name:     t.Name,
		State:    t.State,
		Progress: float32(t.Progress),
		DLSpeed:  t.DLSpeed,
		UpSpeed:  t.UPSpeed,
		NumSeeds: t.NumSeeds,
		NumLeech: t.NumLeechs,
		Size:     t.Size,
		SavePath: t.SavePath,
	}
}

// unused import guard
var _ = strconv.Itoa
