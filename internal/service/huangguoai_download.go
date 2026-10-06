package service

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const TaskKindHuangGuoAIDownload = "huangguoai_download"

// HuangGuoAIDownloadService owns two independent pools: network transfer and full local verification.
type HuangGuoAIDownloadService struct {
	repo    *repository.Container
	catalog *HuangGuoAIService
	tasks   *TaskTrackerService
	mu      sync.Mutex
	cancel  context.CancelFunc
	closed  bool
	wg      sync.WaitGroup
	wake    chan struct{}
}
type HuangGuoAIDownloadConfig struct {
	Root                    string `json:"root"`
	TemporaryDir            string `json:"temporary_dir"`
	OutputDir               string `json:"output_dir"`
	Concurrency             int    `json:"concurrency"`
	VerificationConcurrency int    `json:"verification_concurrency"`
}

func NewHuangGuoAIDownloadService(repo *repository.Container, catalog *HuangGuoAIService, tasks *TaskTrackerService) *HuangGuoAIDownloadService {
	return &HuangGuoAIDownloadService{repo: repo, catalog: catalog, tasks: tasks, wake: make(chan struct{}, 1)}
}
func (s *HuangGuoAIDownloadService) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *HuangGuoAIDownloadService) Config(ctx context.Context) (HuangGuoAIDownloadConfig, error) {
	cfg := HuangGuoAIDownloadConfig{Concurrency: 2, VerificationConcurrency: 2}
	var settings []model.Setting
	err := s.repo.DB.WithContext(ctx).Where("key IN ?", []string{"huangguoai.download_root", "huangguoai.download_concurrency", "huangguoai.verification_concurrency"}).Find(&settings).Error
	if err != nil {
		return cfg, err
	}
	for _, x := range settings {
		switch x.Key {
		case "huangguoai.download_root":
			cfg.Root = x.Value
		case "huangguoai.download_concurrency":
			cfg.Concurrency, err = strconv.Atoi(x.Value)
		case "huangguoai.verification_concurrency":
			cfg.VerificationConcurrency, err = strconv.Atoi(x.Value)
		}
		if err != nil {
			return cfg, errors.New("黄果 AI 下载配置无效")
		}
	}
	if cfg.Concurrency < 1 || cfg.Concurrency > 10 || cfg.VerificationConcurrency < 1 || cfg.VerificationConcurrency > 20 {
		return cfg, errors.New("黄果 AI 下载并发配置无效")
	}
	if cfg.Root != "" {
		cfg.TemporaryDir = filepath.Join(cfg.Root, "downloading")
		cfg.OutputDir = filepath.Join(cfg.Root, "completed")
	}
	return cfg, nil
}
func (s *HuangGuoAIDownloadService) SaveConfig(ctx context.Context, cfg HuangGuoAIDownloadConfig) (HuangGuoAIDownloadConfig, error) {
	if cfg.Concurrency < 1 || cfg.Concurrency > 10 || cfg.VerificationConcurrency < 1 || cfg.VerificationConcurrency > 20 {
		return cfg, errors.New("下载并发须为 1–10，校验并发须为 1–20")
	}
	root := strings.TrimSpace(cfg.Root)
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) || strings.ContainsRune(root, 0) {
		return cfg, errors.New("请设置非文件系统根目录的绝对路径")
	}
	if err := os.MkdirAll(root, 0750); err != nil {
		return cfg, errors.New("下载目录无法创建")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return cfg, errors.New("下载目录不可访问")
	}
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		return cfg, err
	}
	for _, lib := range libs {
		paths := []string{lib.Path}
		for _, r := range lib.Roots {
			paths = append(paths, r.Path)
		}
		for _, p := range paths {
			if p == "" {
				continue
			}
			p = resolveMappedDestinationPath(p)
			if real, e := filepath.EvalSymlinks(p); e == nil {
				p = real
			}
			if downloadPathContains(p, root) || downloadPathContains(root, p) {
				return cfg, errors.New("下载目录不能与媒体库重叠")
			}
		}
	}
	// Separate queues must not share output or attempt directories.
	other, err := s.repo.Setting.Get(ctx, "hongguo.download_root")
	if err != nil {
		return cfg, err
	}
	if other != "" && (downloadPathContains(other, root) || downloadPathContains(root, other)) {
		return cfg, errors.New("黄果 AI 下载目录不能与红果下载目录重叠")
	}
	for _, name := range []string{"downloading", "completed"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0750); err != nil {
			return cfg, errors.New("下载子目录不可写")
		}
		info, e := os.Lstat(p)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return cfg, errors.New("下载子目录必须是实际目录")
		}
	}
	f, err := os.CreateTemp(filepath.Join(root, "downloading"), ".check-")
	if err != nil {
		return cfg, errors.New("暂存目录不可写")
	}
	f.Close()
	defer os.Remove(f.Name())
	target := filepath.Join(root, "completed", filepath.Base(f.Name()))
	if err = os.Link(f.Name(), target); err != nil {
		return cfg, errors.New("下载和完成目录必须支持同文件系统原子发布")
	}
	defer os.Remove(target)
	settings := []model.Setting{{Key: "huangguoai.download_root", Value: root}, {Key: "huangguoai.download_concurrency", Value: strconv.Itoa(cfg.Concurrency)}, {Key: "huangguoai.verification_concurrency", Value: strconv.Itoa(cfg.VerificationConcurrency)}}
	if err = s.repo.DB.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&settings).Error; err != nil {
		return cfg, err
	}
	s.Wake()
	return s.Config(ctx)
}

// huangGuoAIDownloadDirectory 按来源分类及作品 ID 分入 aa～hh 共 64 个桶。
func huangGuoAIDownloadDirectory(category, title, id string) (string, error) {
	categoryName := map[string]string{"ai-duanju": "AI短剧", "ai-manju": "AI漫剧", "ai-huanlian": "AI换脸", "ai-mogai": "AI魔改"}[category]
	if categoryName == "" {
		return "", errors.New("黄果 AI 来源分类未补齐")
	}
	// 复用作品标题清洗与截断规则，来源标签只保留在作品目录中。
	name := strings.Replace(filepath.Base(hongGuoDownloadDirectory(0, 0, title, id)), "[hongguo-", "[huangguoai-", 1)
	bucket := crc32.ChecksumIEEE([]byte(id)) % 64
	return filepath.Join(categoryName, fmt.Sprintf("%c%c", 'a'+bucket/8, 'a'+bucket%8), name), nil
}

func (s *HuangGuoAIDownloadService) Enqueue(ctx context.Context, id string) (int, error) {
	if !huangguoai.ValidID(id) {
		return 0, errors.New("作品 ID 无效")
	}
	enabled, err := s.catalog.Enabled(ctx)
	if err != nil {
		return 0, err
	}
	if !enabled {
		return 0, ErrHuangGuoAIDisabled
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	if cfg.Root == "" {
		return 0, errors.New("请先设置黄果 AI 下载目录")
	}
	added := 0
	err = s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var work model.HuangGuoAIWork
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", id).Take(&work).Error; e != nil {
			return errors.New("请先补齐黄果 AI 作品资料")
		}
		if work.ProjectionError != "" {
			return errors.New("该作品存在分类冲突，暂不下载")
		}
		var episodes []model.HuangGuoAIEpisode
		if e := tx.Where("work_id = ?", work.ID).Order("number").Limit(10001).Find(&episodes).Error; e != nil {
			return e
		}
		if len(episodes) == 0 || len(episodes) > 10000 {
			return errors.New("分集资料为空或超出上限")
		}
		if work.Kind == model.MetadataKindMovie && (len(episodes) != 1 || episodes[0].Number != 1) {
			return errors.New("电影存在未确认的分集结构，暂不下载")
		}
		dir, err := huangGuoAIDownloadDirectory(work.SourceCategory, work.Title, id)
		if err != nil {
			return err
		}
		placement := model.HuangGuoAIDownloadWork{SourceID: id, Title: work.Title, Root: cfg.Root, Directory: dir}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&placement).Error; e != nil {
			return e
		}
		if e := tx.Where("source_id = ?", id).Take(&placement).Error; e != nil {
			return e
		}
		rows := make([]model.HuangGuoAIDownload, 0, len(episodes))
		for _, ep := range episodes {
			rows = append(rows, model.HuangGuoAIDownload{SourceID: id, Episode: ep.Number, Title: placement.Title, Root: placement.Root, RelativePath: filepath.Join(placement.Directory, "Season 01", fmt.Sprintf("S01E%03d.mp4", ep.Number)), Status: "queued"})
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, 100)
		added = int(result.RowsAffected)
		return result.Error
	})
	if err == nil {
		s.refreshWorkTask(ctx, id)
		s.Wake()
	}
	return added, err
}
func (s *HuangGuoAIDownloadService) List(ctx context.Context, page int) ([]model.HuangGuoAIDownload, int64, error) {
	if page < 1 || page > 1000000 {
		return nil, 0, errors.New("分页无效")
	}
	rows := []model.HuangGuoAIDownload{}
	var count int64
	q := s.repo.DB.WithContext(ctx).Model(&model.HuangGuoAIDownload{})
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC,id").Offset((page - 1) * 50).Limit(50).Find(&rows).Error
	return rows, count, err
}
func (s *HuangGuoAIDownloadService) Action(ctx context.Context, id, action string) error {
	var sourceID string
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.HuangGuoAIDownload
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&row).Error; e != nil {
			return e
		}
		sourceID = row.SourceID
		switch action {
		case "cancel":
			if row.Status == "completed" {
				return errors.New("已完成任务不能取消")
			}
			return tx.Model(&row).Updates(map[string]any{"status": "cancelled"}).Error
		case "retry":
			if row.Status != "cancelled" && row.Status != "failed" {
				return errors.New("仅失败或取消任务可重试")
			}
			if row.LeaseUntil != nil && row.LeaseUntil.After(time.Now()) {
				return errors.New("等待旧执行退出")
			}
			return tx.Model(&row).Updates(map[string]any{"status": "queued", "error": "", "lease_token": "", "lease_until": nil}).Error
		default:
			return errors.New("下载操作无效")
		}
	})
	if err == nil {
		s.refreshWorkTask(ctx, sourceID)
		s.Wake()
	}
	return err
}
func (s *HuangGuoAIDownloadService) Start(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.recoverWorkTasks(ctx)
	}()
	for _, verify := range []bool{false, true} {
		s.wg.Add(1)
		go func() { defer s.wg.Done(); s.pool(ctx, verify) }()
	}
}
func (s *HuangGuoAIDownloadService) Wait() {
	s.mu.Lock()
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *HuangGuoAIDownloadService) pool(ctx context.Context, verify bool) {
	var jobs sync.WaitGroup
	defer jobs.Wait()
	done := make(chan struct{}, 20)
	active := 0
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		cfg, e := s.Config(ctx)
		enabled, ee := s.catalog.Enabled(ctx)
		limit := cfg.Concurrency
		if verify {
			limit = cfg.VerificationConcurrency
		}
		if e == nil && ee == nil && enabled && cfg.Root != "" {
			for active < limit {
				var row *model.HuangGuoAIDownload
				if verify {
					row, e = s.repo.HuangGuoAI.ClaimHuangGuoAIVerification(ctx)
				} else {
					row, e = s.repo.HuangGuoAI.ClaimHuangGuoAIDownload(ctx)
				}
				if e != nil || row == nil {
					break
				}
				active++
				jobs.Add(1)
				go func(row model.HuangGuoAIDownload) {
					defer jobs.Done()
					defer func() { done <- struct{}{} }()
					s.run(ctx, row)
				}(*row)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-done:
			active--
		case <-tick.C:
		case <-s.wake:
		}
	}
}
