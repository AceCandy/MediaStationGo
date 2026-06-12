// Package service — 下载管理器，管理多个下载客户端适配器。
//
// DownloadManager 提供多客户端分发能力，支持运行时热插拔。
// 调用方通过 GetDefault() 或 GetClient(id) 获取适配器来执行下载操作。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// DownloadManager 管理多个下载客户端适配器实例。
type DownloadManager struct {
	log    *zap.Logger
	repo   *repository.Container
	crypto *CryptoService

	mu      sync.RWMutex
	clients map[string]DownloadAdapter // clientID -> adapter
	configs map[string]DownloadClientConfig
}

// NewDownloadManager 创建新的下载管理器。
func NewDownloadManager(log *zap.Logger, repo *repository.Container, crypto *CryptoService) *DownloadManager {
	return &DownloadManager{
		log:     log,
		repo:    repo,
		crypto:  crypto,
		clients: make(map[string]DownloadAdapter),
		configs: make(map[string]DownloadClientConfig),
	}
}

// LoadAll 从数据库加载所有已启用的客户端并初始化适配器。
func (m *DownloadManager) LoadAll(ctx context.Context) error {
	dbClients, err := m.repo.DownloadClient.ListEnabled(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 清空现有
	m.clients = make(map[string]DownloadAdapter, len(dbClients))
	m.configs = make(map[string]DownloadClientConfig, len(dbClients))

	for _, dc := range dbClients {
		cfg, err := m.buildConfig(&dc)
		if err != nil {
			m.log.Warn("failed to build config for download client",
				zap.String("id", dc.ID),
				zap.String("name", dc.Name),
				zap.Error(err),
			)
			continue
		}

		adapter := AdapterFactory(dc.Type)
		if adapter == nil {
			m.log.Warn("unknown download client type",
				zap.String("type", dc.Type),
				zap.String("id", dc.ID),
			)
			continue
		}

		if initErr := adapter.Initialize(ctx, cfg); initErr != nil {
			// 初始化失败通常是 Docker 启动顺序问题（qBittorrent 还没就绪）。
			// 适配器内部支持按需重新登录（403/未登录时透明重试），所以
			// 这里仍然注册适配器，等下载器上线后自动恢复；此前直接 continue
			// 会让该客户端在应用重启前永久不可用，下载完成也无法整理入库。
			m.log.Warn("download client init failed; registered for lazy reconnect",
				zap.String("id", dc.ID),
				zap.String("name", dc.Name),
				zap.Error(initErr),
			)
		}

		m.clients[dc.ID] = adapter
		m.configs[dc.ID] = cfg
		m.log.Info("download client registered",
			zap.String("id", dc.ID),
			zap.String("name", dc.Name),
			zap.String("type", dc.Type),
		)
	}
	return nil
}

// GetDefault 返回默认下载客户端适配器。
// 如果没有设置默认客户端，返回第一个可用的客户端。
func (m *DownloadManager) GetDefault() (string, DownloadAdapter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 首先找默认的
	defaultClient, err := m.repo.DownloadClient.FindDefault(context.Background())
	if err != nil {
		return "", nil, err
	}
	if defaultClient != nil {
		if adapter, ok := m.clients[defaultClient.ID]; ok {
			return defaultClient.ID, adapter, nil
		}
	}

	// 返回第一个可用的
	for id, adapter := range m.clients {
		return id, adapter, nil
	}

	return "", nil, errors.New("no download client available")
}

// GetClient 返回指定 ID 的下载客户端适配器。
func (m *DownloadManager) GetClient(id string) (DownloadAdapter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	adapter, ok := m.clients[id]
	if !ok {
		return nil, errors.New("download client not found or not initialized")
	}
	return adapter, nil
}

// AddClient 动态添加并初始化一个下载客户端。
func (m *DownloadManager) AddClient(ctx context.Context, dc *model.DownloadClient) error {
	cfg, err := m.buildConfig(dc)
	if err != nil {
		return err
	}

	adapter := AdapterFactory(dc.Type)
	if adapter == nil {
		return errors.New("unknown download client type: " + dc.Type)
	}

	if err := adapter.Initialize(ctx, cfg); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[dc.ID] = adapter
	m.configs[dc.ID] = cfg
	return nil
}

// RemoveClient 移除一个下载客户端（停止适配器，不删除数据库记录）。
func (m *DownloadManager) RemoveClient(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, id)
	delete(m.configs, id)
}

// UpdateClient 更新已有客户端的配置并重新初始化。
func (m *DownloadManager) UpdateClient(ctx context.Context, dc *model.DownloadClient) error {
	m.RemoveClient(dc.ID)
	return m.AddClient(ctx, dc)
}

// TestConnection 测试客户端连接。
func (m *DownloadManager) TestConnection(ctx context.Context, dc *model.DownloadClient) error {
	cfg, err := m.buildConfig(dc)
	if err != nil {
		return err
	}

	adapter := AdapterFactory(dc.Type)
	if adapter == nil {
		return errors.New("unknown download client type: " + dc.Type)
	}

	return adapter.Initialize(ctx, cfg)
}

// ListAll 获取所有已加载客户端的种子列表。
func (m *DownloadManager) ListAll(ctx context.Context, filter string) (map[string][]TorrentInfo, error) {
	m.mu.RLock()
	ids := make([]string, 0, len(m.clients))
	for id := range m.clients {
		ids = append(ids, id)
	}
	adapters := make([]DownloadAdapter, 0, len(m.clients))
	for _, id := range ids {
		adapters = append(adapters, m.clients[id])
	}
	m.mu.RUnlock()

	result := make(map[string][]TorrentInfo)
	for i, id := range ids {
		list, err := adapters[i].List(ctx, filter)
		if err != nil {
			m.log.Warn("failed to list torrents from client",
				zap.String("id", id),
				zap.Error(err),
			)
			continue
		}
		result[id] = list
	}
	return result, nil
}

// GetAdapterTypes 返回支持的下载客户端类型列表。
func (m *DownloadManager) GetAdapterTypes() []AdapterTypeInfo {
	return []AdapterTypeInfo{
		{Type: "qbittorrent", Name: "qBittorrent", Description: "qBittorrent WebUI API (v2)"},
		{Type: "transmission", Name: "Transmission", Description: "Transmission RPC API"},
		{Type: "aria2", Name: "Aria2", Description: "Aria2 JSON-RPC API"},
	}
}

// AdapterTypeInfo 描述下载客户端类型信息。
type AdapterTypeInfo struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// buildConfig 从数据库模型构建适配器配置。
func (m *DownloadManager) buildConfig(dc *model.DownloadClient) (DownloadClientConfig, error) {
	password := dc.Password
	if m.crypto != nil && password != "" {
		password = m.crypto.Decrypt(password)
	}
	host, err := normalizeDownloadClientEndpoint(dc.Type, dc.Host)
	if err != nil {
		return DownloadClientConfig{}, err
	}

	cfg := DownloadClientConfig{
		Host:     host,
		Username: dc.Username,
		Password: password,
	}

	// 解析 Extra JSON 配置
	if dc.Extra != "" {
		extraStr := dc.Extra
		if m.crypto != nil {
			extraStr = m.crypto.Decrypt(extraStr)
		}
		var extra map[string]string
		if err := json.Unmarshal([]byte(extraStr), &extra); err == nil {
			cfg.Extra = extra
		}
	}

	return cfg, nil
}
