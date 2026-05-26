// Package service — download manager.
//
// DownloadService persists user-initiated downloads, dispatches them to
// the configured client (currently qBittorrent) and pushes live progress
// to the WS hub so the React UI can render a live table.
//
// Settings consumed (system Setting table):
//
//   qbittorrent.url       e.g. http://127.0.0.1:8080
//   qbittorrent.username  qBittorrent WebUI user
//   qbittorrent.password  qBittorrent WebUI password
//   qbittorrent.savepath  optional default save dir
//
// Settings can be updated at runtime via the admin UI; ReloadConfig()
// re-reads them and re-authenticates.
package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// DownloadService is the single download orchestrator.
type DownloadService struct {
	log          *zap.Logger
	repo         *repository.Container
	hub          *Hub
	qb           *QBitClient
	organizer    *OrganizerService

	mu           sync.Mutex
	stopCh       chan struct{}
	pollOnce     sync.Once
	prevStates   map[string]bool // hash -> wasCompleted
}

// NewDownloadService is the constructor.
func NewDownloadService(log *zap.Logger, repo *repository.Container, hub *Hub, organizer *OrganizerService) *DownloadService {
	return &DownloadService{
		log:       log,
		repo:      repo,
		hub:       hub,
		qb:        NewQBitClient(log, QBitConfig{}),
		organizer: organizer,
		prevStates: make(map[string]bool),
		stopCh:    make(chan struct{}),
	}
}

// Start kicks off the background poller (idempotent).
func (d *DownloadService) Start(ctx context.Context) {
	d.pollOnce.Do(func() {
		_ = d.ReloadConfig(ctx)
		go d.poll(ctx)
	})
}

// Stop terminates the poller.
func (d *DownloadService) Stop() {
	close(d.stopCh)
}

// ReloadConfig rebuilds the qBittorrent client from the configured
// download clients (preferred) or the legacy Setting table (fallback).
//
// 配置来源优先级：
//
//  1. download_clients 表中 type=qbittorrent 且 is_default=true 且 enabled=true
//     的行（侧边栏「下载器」页面写入的数据）。
//  2. system Setting 表中的 qbittorrent.url / username / password
//     （旧版「系统设置」表单写入的数据；保留作向后兼容）。
//
// 这避免了两套配置各跑各的：之前操作员明明已经在「下载器」页面填好
// 默认 qb，但实际下载链路读的还是 Setting 表，导致一直连不上。
func (d *DownloadService) ReloadConfig(ctx context.Context) error {
	cfg := QBitConfig{}

	// Path 1: download_clients 表
	if d.repo.DownloadClient != nil {
		if c, err := d.repo.DownloadClient.FindDefault(ctx); err == nil && c != nil && c.Type == "qbittorrent" {
			cfg.BaseURL = strings.TrimRight(c.Host, "/")
			cfg.Username = c.Username
			cfg.Password = c.Password
		}
	}

	// Path 2: legacy Setting 表（仅在 client 表未配置时回退）
	if cfg.BaseURL == "" {
		get := func(k string) string {
			v, _ := d.repo.Setting.Get(ctx, k)
			return v
		}
		cfg.BaseURL = get("qbittorrent.url")
		cfg.Username = get("qbittorrent.username")
		cfg.Password = get("qbittorrent.password")
	}

	d.qb.Configure(cfg)
	return nil
}

// AddDownload accepts a magnet URL / HTTP URL and persists a tracking row.
func (d *DownloadService) AddDownload(ctx context.Context, userID, urlStr, savePath string) (*model.DownloadTask, error) {
	if urlStr == "" {
		return nil, errors.New("empty url")
	}
	if savePath == "" {
		savePath, _ = d.repo.Setting.Get(ctx, "qbittorrent.savepath")
	}
	if err := d.qb.AddTorrent(ctx, urlStr, savePath); err != nil {
		return nil, err
	}
	t := &model.DownloadTask{
		UserID:   userID,
		Source:   "qbittorrent",
		URL:      urlStr,
		SavePath: savePath,
		Status:   "queued",
	}
	if err := d.repo.Download.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// List returns every persisted download task augmented with live data
// from qBittorrent when available.
func (d *DownloadService) List(ctx context.Context) ([]model.DownloadTask, []QBitTorrent, error) {
	rows, err := d.repo.Download.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	live, err := d.qb.List(ctx, "")
	if err != nil {
		// Network failure shouldn't break the page — return rows with no
		// live data and let the UI render the persisted snapshot.
		d.log.Debug("qbittorrent list failed", zap.Error(err))
		return rows, nil, nil
	}
	return rows, live, nil
}

// Delete removes a torrent (and optionally its files) from qBittorrent.
func (d *DownloadService) Delete(ctx context.Context, hash string, withFiles bool) error {
	return d.qb.Delete(ctx, hash, withFiles)
}

// poll fans out qBittorrent /torrents/info every 5 s as WS events. The
// payload is opaque to the client; the React store merges by hash.
func (d *DownloadService) poll(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	// prevStates tracks previous completion states to detect "just finished"
	if d.prevStates == nil {
		d.prevStates = make(map[string]bool)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-t.C:
		}
		live, err := d.qb.List(ctx, "")
		if err != nil {
			continue
		}
		// Detect completed downloads and trigger organize
		for _, t := range live {
			hash := t.Hash
			complete := t.Progress >= 1.0
			if complete && !d.prevStates[hash] {
				// Just completed: trigger organize
				go d.onTorrentComplete(ctx, hash, t.SavePath)
			}
			d.prevStates[hash] = complete
		}
		d.hub.Publish("download", map[string]any{"torrents": live})
	}
}

// onTorrentComplete handles a torrent that just finished downloading.
// It tries to find the associated Media record and trigger organize.
func (d *DownloadService) onTorrentComplete(ctx context.Context, hash string, savePath string) {
	if d.organizer == nil || savePath == "" {
		return
	}
	// 仅当显式开启 organizer.auto_after_download 时才在下载完成后整理。
	// 之前的代码错误地把 organizer.smart_classify 也当成"自动整理"开关，
	// 让操作员只想启用"分类子目录"就被动触发了文件 move。
	autoOrganize := false
	if v, err := d.repo.Setting.Get(ctx, "organizer.auto_after_download"); err == nil {
		autoOrganize = v == "true" || v == "1" || v == "on"
	}
	if !autoOrganize {
		d.log.Info("download completed, auto-organize disabled", zap.String("hash", hash))
		return
	}
	d.log.Info("download completed, triggering organize", zap.String("hash", hash), zap.String("save_path", savePath))
	// Find Media record by path prefix
	var medias []model.Media
	if err := d.repo.DB.WithContext(ctx).Where("path LIKE ?", savePath+"%").Find(&medias).Error; err != nil {
		d.log.Error("find media by path", zap.Error(err))
		return
	}
	for i := range medias {
		if _, err := d.organizer.OrganizeMedia(ctx, medias[i].ID); err != nil {
			d.log.Error("organize media", zap.String("media_id", medias[i].ID), zap.Error(err))
		}
	}
}
