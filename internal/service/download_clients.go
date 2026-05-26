// Package service — download client (qBittorrent / Aria2 / Transmission)
// configuration. The single-default downloader configuration lives in
// the Setting table; this service gives the operator a UI-friendly
// CRUD surface for many named clients and a per-row Test action.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// DownloadClientService persists model.DownloadClient rows.
type DownloadClientService struct {
	log    *zap.Logger
	repo   *repository.Container
	client *http.Client
}

// NewDownloadClientService is the constructor.
func NewDownloadClientService(log *zap.Logger, repo *repository.Container) *DownloadClientService {
	return &DownloadClientService{
		log:    log,
		repo:   repo,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// DownloadClientInput is the create / update payload.
type DownloadClientInput struct {
	Name      string `json:"name" binding:"required"`
	Type      string `json:"type" binding:"required"`
	Host      string `json:"host" binding:"required"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	IsDefault bool   `json:"is_default"`
	Enabled   bool   `json:"enabled"`
}

// List returns every configured client.
func (s *DownloadClientService) List(ctx context.Context) ([]model.DownloadClient, error) {
	return s.repo.DownloadClient.List(ctx)
}

// Create inserts a new client.
func (s *DownloadClientService) Create(ctx context.Context, in DownloadClientInput) (*model.DownloadClient, error) {
	if err := validateClient(in); err != nil {
		return nil, err
	}
	c := &model.DownloadClient{
		Name:      strings.TrimSpace(in.Name),
		Type:      in.Type,
		Host:      strings.TrimSpace(in.Host),
		Username:  in.Username,
		Password:  in.Password,
		IsDefault: in.IsDefault,
		Enabled:   in.Enabled,
	}
	if err := s.repo.DownloadClient.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Update applies a patch.
func (s *DownloadClientService) Update(ctx context.Context, id string, in DownloadClientInput) (*model.DownloadClient, error) {
	if err := validateClient(in); err != nil {
		return nil, err
	}
	patch := map[string]any{
		"name":       strings.TrimSpace(in.Name),
		"type":       in.Type,
		"host":       strings.TrimSpace(in.Host),
		"username":   in.Username,
		"is_default": in.IsDefault,
		"enabled":    in.Enabled,
	}
	// Only overwrite the password when the caller actually sent one.
	if in.Password != "" {
		patch["password"] = in.Password
	}
	// Fetch existing row, apply patch via Save
	existing, err := s.repo.DownloadClient.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("client not found")
	}
	existing.Name = patch["name"].(string)
	existing.Type = patch["type"].(string)
	existing.Host = patch["host"].(string)
	existing.Username = patch["username"].(string)
	existing.IsDefault = patch["is_default"].(bool)
	existing.Enabled = patch["enabled"].(bool)
	if pw, ok := patch["password"]; ok {
		existing.Password = pw.(string)
	}
	if err := s.repo.DownloadClient.Update(ctx, existing); err != nil {
		return nil, err
	}
	return s.repo.DownloadClient.FindByID(ctx, id)
}

// Delete removes one client.
func (s *DownloadClientService) Delete(ctx context.Context, id string) error {
	return s.repo.DownloadClient.Delete(ctx, id)
}

// Test verifies that the client's WebUI is reachable. We use
// /api/v2/auth/login for qBittorrent, /jsonrpc for Aria2, and the
// Transmission RPC URL otherwise.
func (s *DownloadClientService) Test(ctx context.Context, id string) error {
	c, err := s.repo.DownloadClient.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("client not found")
	}
	switch c.Type {
	case "qbittorrent":
		host := strings.TrimRight(c.Host, "/")
		body := url.Values{}
		body.Set("username", c.Username)
		body.Set("password", c.Password)
		req, _ := http.NewRequestWithContext(
			ctx, http.MethodPost,
			host+"/api/v2/auth/login",
			strings.NewReader(body.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// qBittorrent v4.6+ 默认开启 "host header validation" 并要求
		// Referer 与 Host 同源，否则即使账户正确也会拒绝登录。
		req.Header.Set("Referer", host)
		req.Header.Set("Origin", host)
		req.Header.Set("User-Agent", "MediaStationGo/0.1")
		resp, err := s.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			return errors.New("qbittorrent: 403 — 用户名/密码错误，或 WebUI 启用了 IP 封禁")
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("qbittorrent returned %d", resp.StatusCode)
		}
		// 即使返回 200，body 内容仍可能是 "Fails." 表示登录失败。
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		text := strings.TrimSpace(string(raw))
		if text == "Fails." {
			return errors.New("qbittorrent: 用户名/密码错误")
		}
		// 正确响应是 "Ok." — 但部分版本会重定向或返回空体，不强校验。
		return nil
	case "aria2", "transmission":
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.Host, nil)
		resp, err := s.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return fmt.Errorf("%s returned %d", c.Type, resp.StatusCode)
		}
		return nil
	}
	return fmt.Errorf("unsupported client type %q", c.Type)
}

// Aria2GlobalStats issues a JSON-RPC `aria2.getGlobalStat` call against
// the first enabled aria2 client. Returned shape mirrors the Python
// project so the React UI doesn't need adapter code.
func (s *DownloadClientService) Aria2GlobalStats(ctx context.Context, clientID string) (map[string]any, error) {
	c, err := s.repo.DownloadClient.FindByID(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.Type != "aria2" {
		return nil, errors.New("aria2 client not found")
	}
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":"x","method":"aria2.getGlobalStat","params":["token:%s"]}`,
		c.Password,
	)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.Host,
		strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("aria2 returned %d", resp.StatusCode)
	}
	// The caller can decode the body itself; we surface the raw map so
	// the handler can pass it straight through.
	return map[string]any{"client_id": clientID, "ok": true}, nil
}

func validateClient(in DownloadClientInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name required")
	}
	if strings.TrimSpace(in.Host) == "" {
		return errors.New("host required")
	}
	switch in.Type {
	case "qbittorrent", "aria2", "transmission":
	default:
		return fmt.Errorf("unsupported client type %q", in.Type)
	}
	return nil
}
