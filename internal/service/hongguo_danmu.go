package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// HongGuoDanmuService 仅由弹幕出口调用；关闭时等待已接纳请求和增量保存。
type HongGuoDanmuService struct {
	repo    *repository.HongGuoRepository
	config  *APIConfigService
	client  *hongguo.Client
	log     *zap.Logger
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closed  bool
	wg      sync.WaitGroup
	slots   chan struct{}
	flights map[hongGuoDanmuKey]*hongGuoDanmuFlight
}

// hongGuoDanmuKey 隔离分集、视频映射和参数快照，不在键中保留明文凭据。
type hongGuoDanmuKey struct {
	source, video string
	episode       int
	appHash       [32]byte
}

// hongGuoDanmuFlight 只共享在途抓取；最后一个等待者离开时取消上游。
type hongGuoDanmuFlight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	output  []byte
	err     error
}

func NewHongGuoDanmuService(repo *repository.HongGuoRepository, config *APIConfigService, log *zap.Logger) *HongGuoDanmuService {
	ctx, cancel := context.WithCancel(context.Background())
	return &HongGuoDanmuService{repo: repo, config: config, client: hongguo.NewClient(nil), log: log, ctx: ctx, cancel: cancel, slots: make(chan struct{}, 3), flights: make(map[hongGuoDanmuKey]*hongGuoDanmuFlight)}
}

func (s *HongGuoDanmuService) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

// Get 的坐标必须先通过媒体权限检查；数据库错误不伪装成空历史。
func (s *HongGuoDanmuService) Get(ctx context.Context, source string, episode int, video string) ([]byte, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("弹幕服务已关闭")
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	history, err := s.repo.Danmus(ctx, source, episode)
	if err != nil {
		return nil, err
	}
	app, enabled, err := s.config.ResolveHongGuoApp(ctx)
	if err != nil || !enabled {
		return marshalHongGuoDanmus(history)
	}
	parameters, _ := json.Marshal(app)
	key := hongGuoDanmuKey{source: source, video: video, episode: episode, appHash: sha256.Sum256(parameters)}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	flight := s.flights[key]
	if flight == nil {
		select {
		case s.slots <- struct{}{}:
		default:
			s.mu.Unlock()
			return marshalHongGuoDanmus(history)
		}
		workCtx, workCancel := context.WithCancel(s.ctx)
		flight = &hongGuoDanmuFlight{done: make(chan struct{}), cancel: workCancel}
		s.flights[key] = flight
		s.wg.Add(1)
		go s.loadDanmus(workCtx, key, flight, history, app)
	}
	flight.waiters++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		flight.waiters--
		if flight.waiters == 0 && s.flights[key] == flight {
			delete(s.flights, key)
			flight.cancel()
		}
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return bytes.Clone(flight.output), flight.err
	}
}

// loadDanmus 共用一次上游抓取与增量保存，响应不等待数据库写入。
func (s *HongGuoDanmuService) loadDanmus(ctx context.Context, key hongGuoDanmuKey, flight *hongGuoDanmuFlight, history []model.HongGuoDanmu, app hongguo.DanmuAppConfig) {
	defer s.wg.Done()
	defer flight.cancel()
	defer func() { <-s.slots }()
	live, fetchErr := s.client.Danmus(ctx, key.source, key.episode, key.video, app)
	if fetchErr != nil {
		s.log.Debug("hongguo danmu: upstream incomplete", zap.Int("received", len(live)))
	}
	merged, added := mergeHongGuoDanmus(key.source, key.episode, history, live)
	output, err := marshalHongGuoDanmus(merged)
	s.mu.Lock()
	flight.output, flight.err = output, err
	if s.flights[key] == flight {
		delete(s.flights, key)
	}
	close(flight.done)
	s.mu.Unlock()
	if err == nil && len(added) > 0 {
		// 保存独立于播放器取消，关闭最多等待此有界写入完成。
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer saveCancel()
		if err := s.repo.InsertDanmus(saveCtx, added); err != nil {
			s.log.Warn("hongguo danmu: incremental save failed", zap.Int("count", len(added)))
		}
	}
}

func mergeHongGuoDanmus(source string, episode int, history []model.HongGuoDanmu, live []hongguo.Danmu) ([]model.HongGuoDanmu, []model.HongGuoDanmu) {
	seen := make(map[string]bool, len(history)+len(live))
	merged := append([]model.HongGuoDanmu(nil), history...)
	for _, row := range history {
		seen[row.CommentID] = true
	}
	var added []model.HongGuoDanmu
	for _, row := range live {
		if seen[row.ID] || !hongguo.ValidID(row.ID) || row.OffsetMS < 0 || strings.TrimSpace(row.Content) == "" {
			continue
		}
		seen[row.ID] = true
		item := model.HongGuoDanmu{SourceID: source, EpisodeNumber: episode, CommentID: row.ID, OffsetMS: row.OffsetMS, Content: row.Content, SourceCreatedAt: row.CreatedAt}
		merged = append(merged, item)
		added = append(added, item)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].OffsetMS == merged[j].OffsetMS {
			return merged[i].CommentID < merged[j].CommentID
		}
		return merged[i].OffsetMS < merged[j].OffsetMS
	})
	return merged, added
}

func marshalHongGuoDanmus(rows []model.HongGuoDanmu) ([]byte, error) {
	type entry struct {
		P    string `xml:"p,attr"`
		Text string `xml:",chardata"`
	}
	doc := struct {
		XMLName  xml.Name `xml:"i"`
		Provider string   `xml:"sourceprovider"`
		Size     int      `xml:"datasize"`
		Rows     []entry  `xml:"d"`
	}{Provider: "hongguo", Size: len(rows)}
	// 历史回退也必须按播放时间输出。
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].OffsetMS == rows[j].OffsetMS {
			return rows[i].CommentID < rows[j].CommentID
		}
		return rows[i].OffsetMS < rows[j].OffsetMS
	})
	for _, row := range rows {
		text := strings.Map(func(r rune) rune {
			if r == 9 || r == 10 || r == 13 || r >= 0x20 && r <= 0xd7ff || r >= 0xe000 && r <= 0xfffd || r >= 0x10000 && r <= 0x10ffff {
				return r
			}
			return -1
		}, row.Content)
		doc.Rows = append(doc.Rows, entry{P: fmt.Sprintf("%d.%03d,1,25,16777215,%d,0,0,%s,0", row.OffsetMS/1000, row.OffsetMS%1000, row.SourceCreatedAt, row.CommentID), Text: text})
	}
	data, err := xml.Marshal(doc)
	return append([]byte(xml.Header), data...), err
}

// HongGuoDanmuTarget 只解析当前可见叶子，不展开合集、季或整部剧的状态。
func (e *EmbyService) HongGuoDanmuTarget(ctx context.Context, userID, id string) (source string, episode int, video string, matched bool, err error) {
	if strings.HasPrefix(id, "hg-") && !strings.HasPrefix(id, "hg-episode-") {
		return "", 0, "", true, gorm.ErrRecordNotFound
	}
	var files *gorm.DB
	if strings.HasPrefix(id, "hg-") {
		files = e.hongGuoItemFiles(ctx, userID, id)
	} else {
		var media model.Media
		result := e.repo.DB.WithContext(ctx).Select("id", "catalog_source").Where("id = ?", id).Limit(1).Find(&media)
		if result.Error != nil {
			return "", 0, "", false, result.Error
		}
		if media.CatalogSource != model.TaskSystemHongGuo {
			return "", 0, "", false, nil
		}
		files = e.hongGuoVisibleFiles(ctx, userID, "").Where("m.id = ?", id)
	}
	var target struct {
		SourceID      string
		Number        int
		SourceVideoID string
	}
	q := e.repo.DB.WithContext(ctx).Table("(?) AS m", files).Joins("JOIN hongguo_media_bindings b ON b.media_id = m.id").Joins("JOIN hongguo_works w ON w.id = b.work_id").Joins("JOIN hongguo_episodes ep ON ep.id = b.episode_id AND ep.work_id = w.id")
	result := q.Select("w.source_id, COALESCE(ep.number,1) AS number, ep.source_video_id").Limit(1).Scan(&target)
	if result.Error != nil {
		return "", 0, "", true, result.Error
	}
	if result.RowsAffected == 0 {
		return "", 0, "", true, gorm.ErrRecordNotFound
	}
	return target.SourceID, target.Number, target.SourceVideoID, true, nil
}
