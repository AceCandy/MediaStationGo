// Package service — API 配置管理服务。
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ApiConfigService 负责第三方 API 配置的 CRUD 和加密管理。
type ApiConfigService struct {
	cfg    *config.Config
	log    *zap.Logger
	repo   *repository.Container
	crypto *CryptoService
}

// NewApiConfigService 创建 API 配置服务实例。
func NewApiConfigService(cfg *config.Config, log *zap.Logger, repo *repository.Container, crypto *CryptoService) *ApiConfigService {
	return &ApiConfigService{cfg: cfg, log: log, repo: repo, crypto: crypto}
}

// ApiConfigService 错误定义。
var (
	ErrApiConfigNotFound = errors.New("API configuration not found")
	ErrInvalidProvider   = errors.New("invalid provider")
	ErrTestFailed        = errors.New("connection test failed")
)

// GetByProvider 获取指定提供者的 API 配置。
func (s *ApiConfigService) GetByProvider(ctx context.Context, provider string) (*model.ApiConfig, error) {
	cfg, err := s.repo.ApiConfig.FindByProvider(ctx, provider)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, ErrApiConfigNotFound
	}
	// 解密敏感字段
	if cfg.APIKey != "" && s.crypto.IsEncrypted(cfg.APIKey) {
		cfg.APIKey = s.crypto.Decrypt(cfg.APIKey)
	}
	return cfg, nil
}

// List 返回所有 API 配置。
func (s *ApiConfigService) List(ctx context.Context) ([]model.ApiConfig, error) {
	configs, err := s.repo.ApiConfig.List(ctx)
	if err != nil {
		return nil, err
	}
	// 解密敏感字段
	for i := range configs {
		if configs[i].APIKey != "" && s.crypto.IsEncrypted(configs[i].APIKey) {
			configs[i].APIKey = s.crypto.Decrypt(configs[i].APIKey)
		}
	}
	return configs, nil
}

// GetProviders 返回预定义的提供者列表。
func (s *ApiConfigService) GetProviders() []model.ApiProvider {
	return model.PredefinedProviders()
}

// Upsert 创建或更新 API 配置，自动加密敏感字段。
func (s *ApiConfigService) Upsert(ctx context.Context, provider string, apiKey, baseURL, extra string, enabled bool) (*model.ApiConfig, error) {
	// 验证提供者是否有效
	if !s.isValidProvider(provider) {
		return nil, ErrInvalidProvider
	}

	// 加密 API Key
	encryptedKey := apiKey
	if apiKey != "" && !s.crypto.IsEncrypted(apiKey) {
		encryptedKey = s.crypto.Encrypt(apiKey)
	}

	cfg := &model.ApiConfig{
		Provider:    provider,
		APIKey:      encryptedKey,
		BaseURL:     baseURL,
		Extra:       extra,
		Enabled:     enabled,
		Description: s.getProviderDescription(provider),
	}

	if err := s.repo.ApiConfig.Upsert(ctx, cfg); err != nil {
		return nil, err
	}

	// 返回解密后的配置
	cfg.APIKey = apiKey
	return cfg, nil
}

// Delete 删除 API 配置。
func (s *ApiConfigService) Delete(ctx context.Context, provider string) error {
	return s.repo.ApiConfig.Delete(ctx, provider)
}

// Update 更新 API 配置。
func (s *ApiConfigService) Update(ctx context.Context, provider string, apiKey, baseURL, extra string, enabled bool) error {
	// 加密 API Key
	encryptedKey := apiKey
	if apiKey != "" && !s.crypto.IsEncrypted(apiKey) {
		encryptedKey = s.crypto.Encrypt(apiKey)
	}

	cfg := &model.ApiConfig{
		Provider: provider,
		APIKey:   encryptedKey,
		BaseURL:  baseURL,
		Extra:    extra,
		Enabled:  enabled,
	}

	return s.repo.ApiConfig.Update(ctx, cfg)
}

// TestConnection 测试 API 连接。
func (s *ApiConfigService) TestConnection(ctx context.Context, provider string) (string, error) {
	cfg, err := s.GetByProvider(ctx, provider)
	if err != nil {
		return "error", err
	}

	// 根据不同提供者执行不同的测试逻辑
	switch provider {
	case "tmdb":
		return s.testTMDb(cfg)
	case "openai":
		return s.testOpenAI(cfg)
	case "deepseek":
		return s.testDeepSeek(cfg)
	case "siliconflow":
		return s.testSiliconFlow(cfg)
	default:
		return "unknown", fmt.Errorf("no test implemented for provider: %s", provider)
	}
}

// testTMDb 测试 TMDb API 连接。
func (s *ApiConfigService) testTMDb(cfg *model.ApiConfig) (string, error) {
	if cfg.APIKey == "" {
		return "error", errors.New("API key is required")
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(s.cfg.Secrets.TMDbAPIProxy, "/")
	}
	if baseURL == "" {
		baseURL = "https://api.themoviedb.org/3"
	}
	testURL := baseURL + "/configuration?api_key=" + url.QueryEscape(cfg.APIKey)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, testURL, nil)
	if err != nil {
		return "error", err
	}
	client := NewExternalHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "error", fmt.Errorf("TMDb connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return "success", nil
	}
	if resp.StatusCode == 401 {
		return "invalid", errors.New("invalid API key")
	}
	return "error", fmt.Errorf("TMDb API returned status %d", resp.StatusCode)
}

// testOpenAI 测试 OpenAI API 连接。
func (s *ApiConfigService) testOpenAI(cfg *model.ApiConfig) (string, error) {
	if cfg.APIKey == "" {
		return "error", errors.New("API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	testURL := baseURL + "/models"
	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		return "error", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req.WithContext(context.Background()))
	if err != nil {
		return "error", fmt.Errorf("OpenAI connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return "success", nil
	}
	if resp.StatusCode == 401 {
		return "invalid", errors.New("invalid API key")
	}
	return "error", fmt.Errorf("OpenAI API returned status %d", resp.StatusCode)
}

// testDeepSeek 测试 DeepSeek API 连接。
func (s *ApiConfigService) testDeepSeek(cfg *model.ApiConfig) (string, error) {
	if cfg.APIKey == "" {
		return "error", errors.New("API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}

	testURL := baseURL + "/models"
	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		return "error", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req.WithContext(context.Background()))
	if err != nil {
		return "error", fmt.Errorf("DeepSeek connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return "success", nil
	}
	if resp.StatusCode == 401 {
		return "invalid", errors.New("invalid API key")
	}
	return "error", fmt.Errorf("DeepSeek API returned status %d", resp.StatusCode)
}

// testSiliconFlow 测试 SiliconFlow API 连接。
func (s *ApiConfigService) testSiliconFlow(cfg *model.ApiConfig) (string, error) {
	if cfg.APIKey == "" {
		return "error", errors.New("API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.siliconflow.cn/v1"
	}

	testURL := baseURL + "/models"
	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		return "error", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req.WithContext(context.Background()))
	if err != nil {
		return "error", fmt.Errorf("SiliconFlow connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return "success", nil
	}
	if resp.StatusCode == 401 {
		return "invalid", errors.New("invalid API key")
	}
	return "error", fmt.Errorf("SiliconFlow API returned status %d", resp.StatusCode)
}

// GetEffectiveConfig 获取生效的 API 配置（数据库配置优先于配置文件）。
func (s *ApiConfigService) GetEffectiveConfig(ctx context.Context, provider string) (*model.ApiConfig, error) {
	// 首先尝试从数据库获取
	cfg, err := s.GetByProvider(ctx, provider)
	if err == nil && cfg != nil {
		return cfg, nil
	}

	// 如果数据库没有，尝试从配置文件获取
	return s.getConfigFromFile(provider)
}

// getConfigFromFile 从配置文件获取 API 配置。
func (s *ApiConfigService) getConfigFromFile(provider string) (*model.ApiConfig, error) {
	var apiKey string
	var hasKey bool

	switch provider {
	case "tmdb":
		apiKey = s.cfg.Secrets.TMDbAPIKey
		hasKey = apiKey != ""
	case "bangumi":
		apiKey = s.cfg.Secrets.BangumiToken
		hasKey = apiKey != ""
	case "thetvdb":
		apiKey = s.cfg.Secrets.TheTVDBAPIKey
		hasKey = apiKey != ""
	case "fanart":
		apiKey = s.cfg.Secrets.FanartAPIKey
		hasKey = apiKey != ""
	}

	if !hasKey {
		return nil, ErrApiConfigNotFound
	}

	return &model.ApiConfig{
		Provider: provider,
		APIKey:   apiKey,
		Enabled:  true,
	}, nil
}

// isValidProvider 检查提供者是否有效。
func (s *ApiConfigService) isValidProvider(provider string) bool {
	providers := model.PredefinedProviders()
	for _, p := range providers {
		if p.ID == provider {
			return true
		}
	}
	return false
}

// getProviderDescription 获取提供者描述。
func (s *ApiConfigService) getProviderDescription(provider string) string {
	providers := model.PredefinedProviders()
	for _, p := range providers {
		if p.ID == provider {
			return p.Description
		}
	}
	return ""
}

// UpdateTestResult 更新测试结果。
func (s *ApiConfigService) UpdateTestResult(ctx context.Context, provider, result string) error {
	return s.repo.ApiConfig.UpdateTestResult(ctx, provider, result)
}

// MaskAPIKey 遮蔽 API Key 的中间部分。
func (s *ApiConfigService) MaskAPIKey(apiKey string) string {
	if len(apiKey) <= 8 {
		return "***"
	}
	return apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
}

// ExtractBaseURL 从 URL 中提取域名。
func ExtractBaseURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Scheme + "://" + u.Host
}

// ProviderMatches 检查请求的提供者是否与配置的提供者匹配。
func ProviderMatches(requested, configured string) bool {
	return strings.EqualFold(requested, configured)
}
