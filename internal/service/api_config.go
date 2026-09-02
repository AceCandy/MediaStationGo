// Package service — third-party API key store.
//
// APIConfigService is a small CRUD layer over the api_configs table. It
// transparently encrypts the api_key column on write and decrypts it on
// read so values stored on disk are useless without the JWT secret.
//
// On first read it seeds the table with the providers supported by
// MediaStationGo today (TMDb / Bangumi / TheTVDB / Fanart / OpenAI / Douban).
package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	ProxyPoolTypeNormal = "normal"
	ProxyPoolTypeResin  = "resin"
)

// APIConfigService coordinates third-party API key storage.
type APIConfigService struct {
	log      *zap.Logger
	repo     *repository.Container
	crypto   *CryptoService
	revision atomic.Uint64
}

// NewAPIConfigService is the constructor.
func NewAPIConfigService(log *zap.Logger, repo *repository.Container, crypto *CryptoService) *APIConfigService {
	return &APIConfigService{log: log, repo: repo, crypto: crypto}
}

// SeedDefaults inserts a row for every well-known provider on first run.
func (s *APIConfigService) SeedDefaults(ctx context.Context) error {
	defaults := []model.APIConfig{
		{Provider: "tmdb", BaseURL: "https://api.themoviedb.org/3", Description: "TMDb (movies + tv)", Enabled: true},
		{Provider: "bangumi", BaseURL: "https://api.bgm.tv", Description: "Bangumi (anime)", Enabled: true},
		{Provider: "thetvdb", BaseURL: "https://api4.thetvdb.com/v4", Description: "TheTVDB (tv)", Enabled: true},
		{Provider: "fanart", BaseURL: "https://webservice.fanart.tv/v3", Description: "Fanart.tv (artwork)", Enabled: true},
		{Provider: "douban", Description: "Douban cookie (zh metadata)", Enabled: true},
		{Provider: "adult", BaseURL: "https://javdb.com", Extra: "https://javbus.sbs,https://www.javbus.com,https://www.cdnbus.cyou,https://www.javsee.cyou,https://www.busjav.cyou", Description: "Adult / 番号元数据（JavDB/JavBus）", Enabled: true},
		{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini", Description: "OpenAI-compatible (smart search)", Enabled: true},
	}
	for i := range defaults {
		var existing model.APIConfig
		err := s.repo.DB.WithContext(ctx).
			Where("provider = ?", defaults[i].Provider).
			First(&existing).Error
		if err == nil {
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := s.repo.DB.WithContext(ctx).Create(&defaults[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

// PublicView is the safe-to-display projection of an API config row.
// The plaintext key is never returned — only a mask.
type PublicView struct {
	ID                 string    `json:"id"`
	Provider           string    `json:"provider"`
	BaseURL            string    `json:"base_url,omitempty"`
	Model              string    `json:"model,omitempty"`
	Extra              string    `json:"extra,omitempty"`
	Enabled            bool      `json:"enabled"`
	ImageDirect        bool      `json:"image_direct"`
	UseProxyPool       bool      `json:"use_proxy_pool"`
	ProxyPoolType      string    `json:"proxy_pool_type"`
	ResinProxyURL      string    `json:"resin_proxy_url,omitempty"`
	ResinAccount       string    `json:"resin_account,omitempty"`
	HasResinProxyToken bool      `json:"has_resin_proxy_token"`
	WebSearchEnabled   bool      `json:"web_search_enabled"`
	Description        string    `json:"description,omitempty"`
	HasKey             bool      `json:"has_key"`
	MaskedKey          string    `json:"masked_key,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// List returns every API config row (with masked keys).
func (s *APIConfigService) List(ctx context.Context) ([]PublicView, error) {
	var rows []model.APIConfig
	if err := s.repo.DB.WithContext(ctx).Order("provider asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]PublicView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.toPublic(&r))
	}
	return out, nil
}

// Get returns the public view for a single provider, or nil.
func (s *APIConfigService) Get(ctx context.Context, provider string) (*PublicView, error) {
	row, err := s.findByProvider(ctx, provider)
	if err != nil || row == nil {
		return nil, err
	}
	v := s.toPublic(row)
	return &v, nil
}

// Resolve returns the decrypted key + base url ready for use by an HTTP
// client. Empty struct (with no error) when the provider is unknown or
// the API key is empty.
type Resolved struct {
	APIKey           string
	BaseURL          string
	Model            string
	Extra            string
	Enabled          bool
	ImageDirect      bool
	UseProxyPool     bool
	ProxyPoolType    string
	ResinProxyURL    string
	ResinProxyToken  string
	ResinAccount     string
	WebSearchEnabled bool
	Revision         uint64
}

// Resolve fetches the live configuration for a provider, decrypting the
// API key. Callers can use Resolved.APIKey != "" as the "configured" check.
func (s *APIConfigService) Resolve(ctx context.Context, provider string) (Resolved, error) {
	row, err := s.findByProvider(ctx, provider)
	if err != nil {
		s.log.Warn("api_config.resolve: query failed", zap.String("provider", provider), zap.Error(err))
		return Resolved{}, err
	}
	if row == nil {
		s.log.Warn("api_config.resolve: provider not found", zap.String("provider", provider))
		return Resolved{}, nil
	}
	resolved := Resolved{
		APIKey:           s.crypto.Decrypt(row.APIKey),
		BaseURL:          row.BaseURL,
		Model:            row.Model,
		Extra:            row.Extra,
		Enabled:          row.Enabled,
		ImageDirect:      row.ImageDirect,
		UseProxyPool:     row.UseProxyPool,
		ProxyPoolType:    effectiveProxyPoolType(row.ProxyPoolType),
		ResinProxyURL:    row.ResinProxyURL,
		ResinProxyToken:  s.crypto.Decrypt(row.ResinProxyToken),
		ResinAccount:     row.ResinAccount,
		WebSearchEnabled: row.WebSearchEnabled,
		Revision:         s.revision.Load(),
	}
	return resolved, nil
}

// Update upserts a single provider's config. An empty patch.APIKey leaves
// the existing key untouched; pass "<clear>" sentinel to wipe it.
type APIConfigPatch struct {
	APIKey           *string `json:"api_key,omitempty"`
	BaseURL          *string `json:"base_url,omitempty"`
	Model            *string `json:"model,omitempty"`
	Extra            *string `json:"extra,omitempty"`
	Enabled          *bool   `json:"enabled,omitempty"`
	ImageDirect      *bool   `json:"image_direct,omitempty"`
	UseProxyPool     *bool   `json:"use_proxy_pool,omitempty"`
	ProxyPoolType    *string `json:"proxy_pool_type,omitempty"`
	ResinProxyURL    *string `json:"resin_proxy_url,omitempty"`
	ResinProxyToken  *string `json:"resin_proxy_token,omitempty"`
	ResinAccount     *string `json:"resin_account,omitempty"`
	WebSearchEnabled *bool   `json:"web_search_enabled,omitempty"`
	Description      *string `json:"description,omitempty"`
}

// Update applies the patch and returns the new public view.
func (s *APIConfigService) Update(ctx context.Context, provider string, patch APIConfigPatch) (*PublicView, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return nil, errors.New("provider required")
	}

	row, err := s.findByProvider(ctx, provider)
	if err != nil {
		return nil, err
	}
	if row == nil {
		row = &model.APIConfig{Provider: provider, Enabled: true}
		if err := s.repo.DB.WithContext(ctx).Create(row).Error; err != nil {
			return nil, err
		}
	}

	updates := map[string]any{}
	proxyPoolType, err := normalizeProxyPoolType(row.ProxyPoolType)
	if err != nil {
		return nil, err
	}
	resinProxyURL := row.ResinProxyURL
	useProxyPool := row.UseProxyPool
	if patch.APIKey != nil {
		v := strings.TrimSpace(*patch.APIKey)
		if v == "" || v == "<clear>" {
			updates["api_key"] = ""
		} else {
			updates["api_key"] = s.crypto.Encrypt(v)
		}
	}
	if patch.BaseURL != nil {
		baseURL := strings.TrimSpace(*patch.BaseURL)
		if provider == "douban" {
			var err error
			baseURL, err = normalizeDoubanImageOrigin(baseURL)
			if err != nil {
				return nil, err
			}
		}
		updates["base_url"] = baseURL
	}
	if patch.Model != nil {
		updates["model"] = strings.TrimSpace(*patch.Model)
	}
	if patch.Extra != nil {
		updates["extra"] = *patch.Extra
	}
	if patch.Enabled != nil {
		updates["enabled"] = *patch.Enabled
	}
	if patch.ImageDirect != nil {
		updates["image_direct"] = *patch.ImageDirect
	}
	if patch.UseProxyPool != nil {
		updates["use_proxy_pool"] = *patch.UseProxyPool
		useProxyPool = *patch.UseProxyPool
	}
	if patch.ProxyPoolType != nil {
		proxyPoolType, err = normalizeProxyPoolType(*patch.ProxyPoolType)
		if err != nil {
			return nil, err
		}
		updates["proxy_pool_type"] = proxyPoolType
	}
	if patch.ResinProxyURL != nil {
		resinProxyURL, err = normalizeResinProxyOrigin(*patch.ResinProxyURL)
		if err != nil {
			return nil, err
		}
		updates["resin_proxy_url"] = resinProxyURL
	}
	if patch.ResinProxyToken != nil {
		v := strings.TrimSpace(*patch.ResinProxyToken)
		if v == "" || v == "<clear>" {
			updates["resin_proxy_token"] = ""
		} else {
			updates["resin_proxy_token"] = s.crypto.Encrypt(v)
		}
	}
	if patch.ResinAccount != nil {
		v := strings.TrimSpace(*patch.ResinAccount)
		if utf8.RuneCountInString(v) > 128 {
			return nil, errors.New("resin account must be at most 128 characters")
		}
		updates["resin_account"] = v
	}
	if patch.WebSearchEnabled != nil {
		updates["web_search_enabled"] = *patch.WebSearchEnabled
	}
	if patch.Description != nil {
		updates["description"] = *patch.Description
	}
	if provider == "douban" && useProxyPool && proxyPoolType == ProxyPoolTypeResin && resinProxyURL == "" {
		return nil, errors.New("resin proxy address is required")
	}
	if len(updates) > 0 {
		if err := s.repo.DB.WithContext(ctx).
			Model(&model.APIConfig{}).
			Where("id = ?", row.ID).
			Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	if provider == "douban" {
		s.revision.Add(1)
	}
	row, err = s.findByProvider(ctx, provider)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("updated API config not found")
	}
	v := s.toPublic(row)
	return &v, nil
}

func normalizeDoubanImageOrigin(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("douban image domain must be an HTTP(S) origin")
	}
	return u.Scheme + "://" + u.Host, nil
}

func effectiveProxyPoolType(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ProxyPoolTypeNormal
	}
	return value
}

func normalizeProxyPoolType(raw string) (string, error) {
	value := effectiveProxyPoolType(raw)
	if value != ProxyPoolTypeNormal && value != ProxyPoolTypeResin {
		return "", errors.New("proxy pool type must be normal or resin")
	}
	return value, nil
}

func normalizeResinProxyOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("resin proxy address must be an HTTP(S) origin")
	}
	return u.Scheme + "://" + u.Host, nil
}

// Delete clears a provider's API key (the row stays so the masked
// description is still useful). Non-existent providers are a no-op.
func (s *APIConfigService) Delete(ctx context.Context, provider string) error {
	provider = strings.TrimSpace(strings.ToLower(provider))
	row, err := s.findByProvider(ctx, provider)
	if err != nil || row == nil {
		return err
	}
	err = s.repo.DB.WithContext(ctx).
		Model(&model.APIConfig{}).
		Where("id = ?", row.ID).
		Update("api_key", "").Error
	if err == nil && provider == "douban" {
		s.revision.Add(1)
	}
	return err
}

func (s *APIConfigService) findByProvider(ctx context.Context, provider string) (*model.APIConfig, error) {
	var row model.APIConfig
	err := s.repo.DB.WithContext(ctx).Where("provider = ?", provider).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *APIConfigService) toPublic(r *model.APIConfig) PublicView {
	plain := s.crypto.Decrypt(r.APIKey)
	pv := PublicView{
		ID:                 r.ID,
		Provider:           r.Provider,
		BaseURL:            r.BaseURL,
		Model:              r.Model,
		Extra:              r.Extra,
		Enabled:            r.Enabled,
		ImageDirect:        r.ImageDirect,
		UseProxyPool:       r.UseProxyPool,
		ProxyPoolType:      effectiveProxyPoolType(r.ProxyPoolType),
		ResinProxyURL:      r.ResinProxyURL,
		ResinAccount:       r.ResinAccount,
		HasResinProxyToken: s.crypto.Decrypt(r.ResinProxyToken) != "",
		WebSearchEnabled:   r.WebSearchEnabled,
		Description:        r.Description,
		HasKey:             plain != "",
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
	if pv.HasKey {
		pv.MaskedKey = MaskAPIKey(plain)
	}
	return pv
}
