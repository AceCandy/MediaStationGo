// Package service — YemaPT site adapter.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type YemaPTAdapter struct {
	client *http.Client
}

func NewYemaPTAdapter() *YemaPTAdapter {
	return &YemaPTAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *YemaPTAdapter) Authenticate(ctx context.Context, cfg SiteConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("YemaPT 需要填写个人详情页创建的第三方对接专用 API Auth Key")
	}
	u := strings.TrimRight(cfg.URL, "/") + "/openApi/user/fetchBasicInfo.json"
	data, status, err := doRequestJSON(ctx, a.client, http.MethodGet, u, cfg, nil)
	if err != nil {
		return fmt.Errorf("yemapt authenticate: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("yemapt authenticate failed: status %d", status)
	}
	var resp yemaPTAPIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("yemapt authenticate parse: %w", err)
	}
	if resp.Success {
		return nil
	}
	if resp.ErrorMessage != "" {
		return fmt.Errorf("yemapt authenticate failed: %s", resp.ErrorMessage)
	}
	if resp.ErrorCode != 0 {
		return fmt.Errorf("yemapt authenticate failed: errorCode=%d", resp.ErrorCode)
	}
	return errors.New("yemapt authenticate failed")
}

func (a *YemaPTAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	return nil, errYemaPTTorrentOpenAPIUnsupported()
}

func (a *YemaPTAdapter) Browse(ctx context.Context, cfg SiteConfig, category string, page int) (*SiteSearchResult, error) {
	return nil, errYemaPTTorrentOpenAPIUnsupported()
}

func (a *YemaPTAdapter) GetDetail(ctx context.Context, cfg SiteConfig, id string) (*TorrentDetail, error) {
	return nil, errYemaPTTorrentOpenAPIUnsupported()
}

type yemaPTAPIResponse struct {
	Success      bool            `json:"success"`
	ShowType     int             `json:"showType"`
	ErrorCode    int             `json:"errorCode"`
	ErrorMessage string          `json:"errorMessage"`
	Data         json.RawMessage `json:"data"`
}

func errYemaPTTorrentOpenAPIUnsupported() error {
	return errors.New("YemaPT 当前公开 OpenAPI 未提供种子搜索/详情接口")
}

func isYemaPTConfig(cfg SiteConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Type), "yemapt") || isYemaPTURL(cfg.URL)
}

func isYemaPTURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "yemapt.org" || strings.HasSuffix(host, ".yemapt.org")
}
