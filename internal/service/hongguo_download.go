package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const TaskKindHongGuoDownload = "hongguo_download"
const hongGuoDownloadRootKey = "hongguo.download_root"
const hongGuoDownloadConcurrencyKey = "hongguo.download_concurrency"
const hongGuoVerificationConcurrencyKey = "hongguo.verification_concurrency"
const hongGuoHardwareVerificationKey = "hongguo.hardware_verification"
const hongGuoDownloadPriorityKey = "hongguo.download_priority"
const defaultHongGuoDownloadConcurrency = 3
const maxHongGuoDownloadConcurrency = 5
const defaultHongGuoVerificationConcurrency = 2
const maxHongGuoVerificationConcurrency = 5

// HongGuoDownloadService 有界并发消费持久化分集队列；跨进程租约由数据库维护。
type HongGuoDownloadService struct {
	repo         *repository.Container
	catalog      *HongGuoService
	tasks        *TaskTrackerService
	client       *hongguo.Client
	http         *http.Client
	wake         chan struct{}
	wg           sync.WaitGroup
	supplementMu sync.Mutex
}

type HongGuoDownloadConfig struct {
	Root                    string `json:"root"`
	TemporaryDir            string `json:"temporary_dir"`
	OutputDir               string `json:"output_dir"`
	Concurrency             int    `json:"concurrency"`
	VerificationConcurrency int    `json:"verification_concurrency"`
	HardwareVerification    bool   `json:"hardware_verification"`
	Priority                string `json:"priority"`
}

// HongGuoDownloadConfigPatch 省略字段时保留原设置，兼容仅更新目录的客户端。
type HongGuoDownloadConfigPatch struct {
	Concurrency             *int    `json:"concurrency"`
	VerificationConcurrency *int    `json:"verification_concurrency"`
	HardwareVerification    *bool   `json:"hardware_verification"`
	Priority                *string `json:"priority"`
}

func NewHongGuoDownloadService(repo *repository.Container, catalog *HongGuoService, tasks *TaskTrackerService) *HongGuoDownloadService {
	client := hongguo.DownloadHTTPClient()
	return &HongGuoDownloadService{repo: repo, catalog: catalog, tasks: tasks, client: hongguo.NewClient(client), http: client, wake: make(chan struct{}, 1)}
}

func (s *HongGuoDownloadService) Config(ctx context.Context) (HongGuoDownloadConfig, error) {
	cfg := HongGuoDownloadConfig{Concurrency: defaultHongGuoDownloadConcurrency, VerificationConcurrency: defaultHongGuoVerificationConcurrency, Priority: hongguo.DownloadApp}
	var settings []model.Setting
	err := s.repo.DB.WithContext(ctx).Where("key IN ?", []string{hongGuoDownloadRootKey, hongGuoDownloadConcurrencyKey, hongGuoVerificationConcurrencyKey, hongGuoHardwareVerificationKey, hongGuoDownloadPriorityKey}).Find(&settings).Error
	if err != nil {
		return cfg, err
	}
	for _, setting := range settings {
		switch setting.Key {
		case hongGuoDownloadRootKey:
			cfg.Root = setting.Value
		case hongGuoDownloadConcurrencyKey:
			cfg.Concurrency, err = strconv.Atoi(setting.Value)
			if err != nil || cfg.Concurrency < 1 || cfg.Concurrency > maxHongGuoDownloadConcurrency {
				return cfg, errors.New("下载并发配置无效")
			}
		case hongGuoVerificationConcurrencyKey:
			cfg.VerificationConcurrency, err = strconv.Atoi(setting.Value)
			if err != nil || cfg.VerificationConcurrency < 1 || cfg.VerificationConcurrency > maxHongGuoVerificationConcurrency {
				return cfg, errors.New("校验并发配置无效")
			}
		case hongGuoHardwareVerificationKey:
			cfg.HardwareVerification, err = strconv.ParseBool(setting.Value)
			if err != nil {
				return cfg, errors.New("硬件校验配置无效")
			}
		case hongGuoDownloadPriorityKey:
			cfg.Priority = setting.Value
			if cfg.Priority != hongguo.DownloadApp && cfg.Priority != hongguo.DownloadOfficial && cfg.Priority != hongguo.DownloadFallback {
				return cfg, errors.New("下载接口优先级无效")
			}
		}
	}
	if cfg.Root != "" {
		cfg.TemporaryDir = filepath.Join(cfg.Root, "downloading")
		cfg.OutputDir = filepath.Join(cfg.Root, "completed")
	}
	return cfg, err
}

func (s *HongGuoDownloadService) SaveConfig(ctx context.Context, root string, patch HongGuoDownloadConfigPatch) (HongGuoDownloadConfig, error) {
	if patch.VerificationConcurrency != nil && (*patch.VerificationConcurrency < 1 || *patch.VerificationConcurrency > maxHongGuoVerificationConcurrency) {
		return HongGuoDownloadConfig{}, errors.New("并发校验数量须为 1–5")
	}
	if patch.Concurrency != nil && (*patch.Concurrency < 1 || *patch.Concurrency > maxHongGuoDownloadConcurrency) {
		return HongGuoDownloadConfig{}, errors.New("并发下载数量须为 1–5")
	}
	if patch.Priority != nil && *patch.Priority != hongguo.DownloadApp && *patch.Priority != hongguo.DownloadOfficial && *patch.Priority != hongguo.DownloadFallback {
		return HongGuoDownloadConfig{}, errors.New("下载接口优先级无效")
	}
	root = strings.TrimSpace(root)
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) || strings.ContainsRune(root, 0) {
		return HongGuoDownloadConfig{}, errors.New("请设置非文件系统根目录的绝对存储路径")
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o750); err != nil {
		return HongGuoDownloadConfig{}, errors.New("无法创建下载存储目录")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return HongGuoDownloadConfig{}, errors.New("下载存储目录不可访问")
	}
	root = canonical
	// 下载目录不能落在已有媒体库内，避免监听器把临时或完成视频自动入库。
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		return HongGuoDownloadConfig{}, err
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
				return HongGuoDownloadConfig{}, errors.New("下载存储目录不能与媒体库目录重叠")
			}
		}
	}
	for _, name := range []string{"downloading", "completed"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o750); err != nil {
			return HongGuoDownloadConfig{}, errors.New("下载子目录不可写")
		}
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return HongGuoDownloadConfig{}, errors.New("下载子目录必须是实际目录")
		}
	}
	// 实际测试原子发布所需的同文件系统链接，不根据路径字符串推断挂载关系。
	f, err := os.CreateTemp(filepath.Join(root, "downloading"), ".check-")
	if err != nil {
		return HongGuoDownloadConfig{}, errors.New("临时目录不可写")
	}
	f.Close()
	defer os.Remove(f.Name())
	target := filepath.Join(root, "completed", filepath.Base(f.Name()))
	if err := os.Link(f.Name(), target); err != nil {
		return HongGuoDownloadConfig{}, errors.New("临时目录与完成目录必须支持同文件系统原子发布")
	}
	defer os.Remove(target)
	settings := []model.Setting{{Key: hongGuoDownloadRootKey, Value: root, UpdatedAt: time.Now()}}
	if patch.HardwareVerification != nil {
		settings = append(settings, model.Setting{Key: hongGuoHardwareVerificationKey, Value: strconv.FormatBool(*patch.HardwareVerification), UpdatedAt: time.Now()})
	}
	if patch.VerificationConcurrency != nil {
		settings = append(settings, model.Setting{Key: hongGuoVerificationConcurrencyKey, Value: strconv.Itoa(*patch.VerificationConcurrency), UpdatedAt: time.Now()})
	}
	if patch.Concurrency != nil {
		settings = append(settings, model.Setting{Key: hongGuoDownloadConcurrencyKey, Value: strconv.Itoa(*patch.Concurrency), UpdatedAt: time.Now()})
	}
	if patch.Priority != nil {
		settings = append(settings, model.Setting{Key: hongGuoDownloadPriorityKey, Value: *patch.Priority, UpdatedAt: time.Now()})
	}
	if err := s.repo.DB.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&settings).Error; err != nil {
		return HongGuoDownloadConfig{}, err
	}
	s.Wake()
	return s.Config(ctx)
}

func downloadPathContains(parent, child string) bool {
	if !filepath.IsAbs(parent) || !filepath.IsAbs(child) {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// hongGuoDownloadDirectory 缺失月份不跳层，缺失年份下直接存放作品。
func hongGuoDownloadDirectory(year, month int, title, id string) string {
	date := "未知年份"
	if year > 0 {
		date = fmt.Sprintf("%04d", year)
		if month >= 1 && month <= 12 {
			date = filepath.Join(date, fmt.Sprintf("%02d", month))
		} else {
			date = filepath.Join(date, "未知月份")
		}
	}
	name := strings.Trim(strings.Map(func(r rune) rune {
		// 标题中的来源标签不能与追加的权威作品 ID 产生歧义。
		if r == '[' {
			return '［'
		}
		if r == ']' {
			return '］'
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, sanitizeFilename(title)), " .")
	runes := []rune(name)
	if len(runes) > 60 {
		name = string(runes[:60])
	}
	if name == "" {
		name = "未命名作品"
	}
	return filepath.Join(date, name+" [hongguo-"+id+"]")
}

func (s *HongGuoDownloadService) Enqueue(ctx context.Context, id string) (int, error) {
	return s.enqueue(ctx, id, false)
}

func (s *HongGuoDownloadService) enqueue(ctx context.Context, id string, onlyNew bool) (int, error) {
	if !hongguo.ValidID(id) {
		return 0, errors.New("作品 ID 无效")
	}
	enabled, err := s.catalog.Enabled(ctx)
	if err != nil {
		return 0, err
	}
	if !enabled {
		return 0, ErrHongGuoDisabled
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	if cfg.Root == "" {
		return 0, errors.New("请先在下载空间设置存储目录")
	}
	var work model.HongGuoWork
	if err := s.repo.DB.WithContext(ctx).First(&work, "source_id = ?", id).Error; err != nil {
		return 0, errors.New("请先补齐该作品的红果资料")
	}
	var episodes []model.HongGuoEpisode
	if err := s.repo.DB.WithContext(ctx).Where("work_id = ?", work.ID).Order("number").Limit(10001).Find(&episodes).Error; err != nil {
		return 0, err
	}
	if len(episodes) == 0 || len(episodes) > 10000 {
		return 0, errors.New("分集资料为空或超过单次下载上限")
	}
	if onlyNew {
		for _, episode := range episodes {
			if !hongguo.ValidID(episode.SourceVideoID) {
				return 0, errors.New("分集资料尚未补齐")
			}
		}
	}
	year, month := 0, 0
	if work.FirstVisibleAt != nil {
		at := work.FirstVisibleAt.In(time.FixedZone("CST", 8*60*60))
		year, month = at.Year(), int(at.Month())
	}
	placement := model.HongGuoDownloadWork{SourceID: id, Title: work.Title, Root: cfg.Root, Directory: hongGuoDownloadDirectory(year, month, work.Title, id)}
	added := 0
	err = s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&placement)
		if created.Error != nil {
			return created.Error
		}
		if onlyNew && created.RowsAffected == 0 {
			return nil
		}
		if err := tx.First(&placement, "source_id = ?", id).Error; err != nil {
			return err
		}
		for _, episode := range episodes {
			status, message := "queued", ""
			if !hongguo.ValidID(episode.SourceVideoID) {
				status, message = "failed", "该集缺少源视频 ID，请刷新作品资料后重试"
			}
			filename := fmt.Sprintf("S01E%03d.mp4", episode.Number)
			row := model.HongGuoDownload{SourceID: id, Episode: episode.Number, VideoID: episode.SourceVideoID, Title: placement.Title, Root: placement.Root, RelativePath: filepath.Join(placement.Directory, "Season 01", filename), Status: status, Error: message}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			if result.Error != nil {
				return result.Error
			}
			added += int(result.RowsAffected)
		}
		return nil
	})
	if err == nil {
		s.Wake()
	}
	return added, err
}

func (s *HongGuoDownloadService) List(ctx context.Context, page int) ([]model.HongGuoDownload, int64, error) {
	rows := []model.HongGuoDownload{}
	var total int64
	if err := s.repo.DB.WithContext(ctx).Model(&model.HongGuoDownload{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := s.repo.DB.WithContext(ctx).Order("created_at DESC, episode, id").Limit(50).Offset((page - 1) * 50).Find(&rows).Error
	return rows, total, err
}

func (s *HongGuoDownloadService) Action(ctx context.Context, id, action string) error {
	if action != "cancel" && action != "retry" {
		return errors.New("下载操作无效")
	}
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.HongGuoDownload
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if action == "cancel" {
			if row.Status == "completed" {
				return errors.New("已完成文件不能取消或删除")
			}
			return tx.Model(&row).Updates(map[string]any{"status": "cancelled", "lease_token": "", "lease_until": nil}).Error
		}
		if row.Status != "failed" && row.Status != "cancelled" {
			return errors.New("只能重试失败或已取消分集")
		}
		return retryHongGuoDownload(tx, row)
	})
	if err == nil {
		s.Wake()
	}
	return err
}

var errDownloadEpisodeMissing = errors.New("该集仍缺少视频 ID，请先刷新作品资料")

// 调用方须先锁定任务并校验状态，批量和单集重试使用同一来源校验。
func retryHongGuoDownload(tx *gorm.DB, row model.HongGuoDownload) error {
	var episode model.HongGuoEpisode
	err := tx.Joins("JOIN hongguo_works w ON w.id = hongguo_episodes.work_id").Where("w.source_id = ? AND hongguo_episodes.number = ?", row.SourceID, row.Episode).First(&episode).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errDownloadEpisodeMissing
	}
	if err != nil {
		return err
	}
	if !hongguo.ValidID(episode.SourceVideoID) {
		return errDownloadEpisodeMissing
	}
	return tx.Model(&row).Updates(map[string]any{"status": "queued", "error": "", "source_errors": "{}", "video_id": episode.SourceVideoID, "source_tries": 0, "lease_token": "", "lease_until": nil}).Error
}

func (s *HongGuoDownloadService) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *HongGuoDownloadService) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTicker(5 * time.Second)
		defer timer.Stop()
		finished := make(chan bool, maxHongGuoDownloadConcurrency+maxHongGuoVerificationConcurrency)
		active, verifying := 0, 0
		defer func() {
			for active+verifying > 0 {
				<-finished
				if active > 0 {
					active--
				} else {
					verifying--
				}
			}
		}()
		for {
			if ctx.Err() != nil {
				return
			}
			enabled, err := s.catalog.Enabled(ctx)
			cfg, configErr := s.Config(ctx)
			if err == nil && enabled && configErr == nil {
				launched := false
				for _, verification := range []bool{true, false} {
					if verification && verifying >= cfg.VerificationConcurrency || !verification && active >= cfg.Concurrency {
						continue
					}
					claim := s.repo.HongGuo.ClaimHongGuoDownload
					if verification {
						claim = s.repo.HongGuo.ClaimHongGuoVerification
					}
					row, e := claim(ctx)
					if e != nil || row == nil {
						continue
					}
					if verification {
						verifying++
					} else {
						active++
					}
					go func() { defer func() { finished <- verification }(); s.run(ctx, *row) }()
					launched = true
				}
				if launched {
					continue
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			case verification := <-finished:
				if verification {
					verifying--
				} else {
					active--
				}
			case <-timer.C:
			}
		}
	}()
}
func (s *HongGuoDownloadService) Wait() { s.wg.Wait() }
