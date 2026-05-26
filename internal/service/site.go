// Package service — site management (PT/BT tracker CRUD + connection test).
//
// SiteService owns the lifecycle of Site rows and exposes a cross-site
// search dispatcher that fans out a keyword query to every enabled site's
// adapter, collects results and returns them merged + sorted.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/helper"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// SiteService manages PT/BT site configurations.
type SiteService struct {
	log            *zap.Logger
	repo           *repository.Container
	flareSolverrURL string
}

// NewSiteService is the constructor.
func NewSiteService(log *zap.Logger, repo *repository.Container, flareSolverrURL string) *SiteService {
	return &SiteService{log: log, repo: repo, flareSolverrURL: flareSolverrURL}
}

// Create persists a new site.
func (s *SiteService) Create(ctx context.Context, site *model.Site) error {
	if strings.TrimSpace(site.Name) == "" || strings.TrimSpace(site.URL) == "" {
		return errors.New("name and url required")
	}
	site.URL = strings.TrimRight(site.URL, "/")
	if site.Type == "" {
		site.Type = "nexusphp"
	}
	if site.AuthType == "" {
		site.AuthType = "cookie"
	}
	return s.repo.DB.WithContext(ctx).Create(site).Error
}

// List returns every site ordered by created_at.
func (s *SiteService) List(ctx context.Context) ([]model.Site, error) {
	var sites []model.Site
	err := s.repo.DB.WithContext(ctx).Order("created_at asc").Find(&sites).Error
	if sites == nil {
		sites = []model.Site{}
	}
	return sites, err
}

// FindByID returns a single site or nil.
func (s *SiteService) FindByID(ctx context.Context, id string) (*model.Site, error) {
	var site model.Site
	err := s.repo.DB.WithContext(ctx).Where("id = ?", id).First(&site).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &site, err
}

// Update applies a partial patch to an existing site.
func (s *SiteService) Update(ctx context.Context, id string, updates map[string]any) error {
	return s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).Updates(updates).Error
}

// Delete removes a site.
func (s *SiteService) Delete(ctx context.Context, id string) error {
	return s.repo.DB.WithContext(ctx).Where("id = ?", id).Delete(&model.Site{}).Error
}

// TestConnection tries to reach the site's base URL with the configured
// credentials and reports success/failure.
//
// 测试逻辑（与参考项目 ShukeBta/MediaStation 对齐）：
//
//  1. 优先调用对应站点适配器的 Authenticate()，让 PT 站点（M-Team / UNIT3D /
//     Gazelle 等）使用各自的开放 API 验证，而不是去拉首页 HTML——后者通常
//     被 Cloudflare 直接 403 但 API 能正常访问。
//  2. 适配器不可用或站点类型未知时，回退到 helper.TestSiteConnectivity 的
//     通用浏览器头 GET 方案。
//  3. helper.TestSiteConnectivity 在全局 FlareSolverr 启用且站点开启了
//     BrowserEmulation 时，会自动走 FlareSolverr。
func (s *SiteService) TestConnection(ctx context.Context, id string) (bool, string, error) {
	site, err := s.FindByID(ctx, id)
	if err != nil || site == nil {
		return false, "site not found", err
	}

	// Get timeout from site config (default 15 seconds)
	timeout := site.Timeout
	if timeout <= 0 {
		timeout = 15
	}
	flareSolverrURL := s.flareSolverrURL

	// ── Path 1: site-aware adapter Authenticate ────────────────────────
	// custom_rss 没有真适配器，跳过；其它类型先尝试针对性认证端点。
	if adapter := NewSiteAdapter(site); adapter != nil && site.Type != "" && site.Type != "custom_rss" {
		cfg := s.siteModelToConfig(site)
		actx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
		if authErr := adapter.Authenticate(actx, cfg); authErr == nil {
			now := time.Now()
			_ = s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).
				Updates(map[string]any{
					"login_status":  "ok",
					"last_error":    "",
					"last_check_at": &now,
				}).Error
			return true, "连接成功", nil
		} else {
			s.log.Warn("site adapter authenticate failed, falling back to generic test",
				zap.String("site", site.Name),
				zap.String("type", site.Type),
				zap.Error(authErr))
			// 回退到通用 GET 测试 — 给 Cookie/RSS 类站点一个机会
		}
	}

	// ── Path 2: generic GET with browser headers / FlareSolverr ───────
	ok, msg, err := helper.TestSiteConnectivity(site, flareSolverrURL, timeout, s.log)
	if err != nil {
		now := time.Now()
		_ = s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).
			Updates(map[string]any{
				"login_status":  "fail",
				"last_error":    err.Error(),
				"last_check_at": &now,
			}).Error
		return false, err.Error(), nil
	}

	loginStatus := "ok"
	storedError := ""
	if !ok {
		loginStatus = "fail"
		storedError = msg
	}
	now := time.Now()
	_ = s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).
		Updates(map[string]any{
			"login_status":  loginStatus,
			"last_error":    storedError,
			"last_check_at": &now,
		}).Error
	return ok, msg, nil
}

// SearchResult is one torrent returned by a site adapter search.
type SearchResult struct {
	SiteName    string `json:"site_name"`
	SiteID      string `json:"site_id"`
	Title       string `json:"title"`
	TorrentURL  string `json:"torrent_url"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	Free        bool   `json:"free"`
}

// Search fans out a keyword query to every enabled site and returns
// merged results sorted by seeders descending.
// Uses concurrent search with sync.WaitGroup for performance.
func (s *SiteService) Search(ctx context.Context, keyword string) ([]SearchResult, error) {
	if strings.TrimSpace(keyword) == "" {
		return []SearchResult{}, nil
	}
	sites, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results []SearchResult
	)

	for i := range sites {
		if !sites[i].Enabled {
			continue
		}
		wg.Add(1)
		go func(site model.Site) {
			defer wg.Done()

			adapter := NewSiteAdapter(&site)
			if adapter == nil {
				return
			}

			cfg := s.siteModelToConfig(&site)

			// Use site timeout or default 30s
			timeout := time.Duration(site.Timeout) * time.Second
			if timeout <= 0 {
				timeout = 30 * time.Second
			}
			ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			result, err := adapter.Search(ctxWithTimeout, cfg, keyword, 1)
			if err != nil {
				s.log.Warn("site search failed",
					zap.String("site", site.Name),
					zap.String("type", site.Type),
					zap.String("url", site.URL),
					zap.Error(err))
				return
			}
			if result == nil {
				return
			}
			items := result.Items
			if items == nil {
				items = []TorrentItem{}
			}
			for _, item := range items {
				mu.Lock()
				results = append(results, SearchResult{
					SiteName:    site.Name,
					SiteID:      site.ID,
					Title:       item.Title,
					TorrentURL:  item.DetailURL,
					DownloadURL: item.DownloadURL,
					Size:        item.Size,
					Seeders:     item.Seeders,
					Leechers:    item.Leechers,
					Free:        item.Free,
				})
				mu.Unlock()
			}
		}(sites[i])
	}
	wg.Wait()

	// Ensure results is never nil (return [] instead of null in JSON)
	if results == nil {
		results = []SearchResult{}
	}

	// Sort by seeders desc.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Seeders > results[j].Seeders
	})
	return results, nil
}

// siteModelToConfig 将 model.Site 转换为适配器使用的 SiteConfig。
// 当全局 FlareSolverr 已启用且此站点开启了 BrowserEmulation 时，填充 FlareSolverrURL。
func (svc *SiteService) siteModelToConfig(s *model.Site) SiteConfig {
	timeout := time.Duration(s.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	userAgent := s.UserAgent
	if userAgent == "" {
		userAgent = model.DefaultUserAgent
	}
	var extra map[string]string
	if s.Extra != "" {
		_ = json.Unmarshal([]byte(s.Extra), &extra)
	}

	// Per-site FlareSolverr opt-in: only when global FlareSolverr is enabled
	// AND this site has BrowserEmulation turned on.
	flareSolverrURL := ""
	if svc.flareSolverrURL != "" && s.BrowserEmulation {
		flareSolverrURL = svc.flareSolverrURL
	}

	return SiteConfig{
		Name:            s.Name,
		Type:            s.Type,
		URL:             s.URL,
		AuthType:        s.AuthType,
		Cookie:          s.Cookie,
		APIKey:          s.APIKey,
		AuthHeader:      s.AuthHeader,
		UserAgent:       userAgent,
		Timeout:         timeout,
		Extra:           extra,
		FlareSolverrURL: flareSolverrURL,
		UseProxy:        s.UseProxy,
	}
}
