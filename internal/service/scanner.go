// Package service — filesystem scanner.
//
// ScannerService walks the configured library roots looking for video files,
// then upserts a model.Media row per file. Track probing runs separately after
// the scan so slow ffprobe work cannot block library ingestion.
//
// When a filename exposes season + episode numbers we store them on the
// Media row for every library type, so variety shows and other episodic
// collections get the same grouping experience as TV/anime.
package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// videoExtensions lists the file extensions treated as media. Matches the
// legacy Python defaults.
var videoExtensions = map[string]struct{}{
	".mkv":  {},
	".mp4":  {},
	".m4v":  {},
	".avi":  {},
	".mov":  {},
	".webm": {},
	".flv":  {},
	".wmv":  {},
	".ts":   {},
	".m2ts": {},
	".mts":  {},
	".vob":  {},
	".rmvb": {},
	".rm":   {},
	".3gp":  {},
	".mpg":  {},
	".mpeg": {},
	".iso":  {},
	".strm": {},
}

// ScannerService walks libraries on disk and upserts model.Media rows.
type ScannerService struct {
	cfg        *config.Config
	log        *zap.Logger
	repo       *repository.Container
	hub        *Hub
	mediaProbe *MediaProbeService
	scraper    *ScraperService
	cache      *RuntimeCacheService
	notify     *NotifyChannelService

	imageProxy *ImageProxy

	localScanMu sync.Mutex
	localScans  map[string]struct{}
}

func (s *ScannerService) SetMediaProbe(mediaProbe *MediaProbeService) {
	if s != nil {
		s.mediaProbe = mediaProbe
	}
}

// NewScannerService is the constructor.
func NewScannerService(
	cfg *config.Config,
	log *zap.Logger,
	repo *repository.Container,
	hub *Hub,
	_ *FFprobeService,
	scraper *ScraperService,
) *ScannerService {
	return &ScannerService{
		cfg: cfg, log: log, repo: repo, hub: hub,
		scraper:    scraper,
		localScans: make(map[string]struct{}),
	}
}

func (s *ScannerService) SetRuntimeCache(cache *RuntimeCacheService) {
	if s != nil {
		s.cache = cache
	}
}

func (s *ScannerService) SetNotifyChannels(notify *NotifyChannelService) {
	if s != nil {
		s.notify = notify
	}
}

func (s *ScannerService) SetImageProxy(imageProxy *ImageProxy) {
	s.imageProxy = imageProxy
}

// ScanResult summarises a scan run.
type ScanResult struct {
	LibraryID      string       `json:"library_id"`
	Visited        int          `json:"visited"`
	Added          int          `json:"added"`
	Updated        int          `json:"updated"`
	Skipped        int          `json:"skipped"`
	Probed         int          `json:"probed"`
	LocalMetadata  int          `json:"local_metadata"`
	Reconciled     int          `json:"reconciled"`
	Removed        int64        `json:"removed"`
	ErrorCount     int          `json:"error_count,omitempty"`
	Errors         []string     `json:"errors,omitempty"`
	Changes        []ScanChange `json:"changes,omitempty"`
	OmittedChanges int          `json:"omitted_changes,omitempty"`
}

type ScanProgress struct {
	Phase          string
	RootID         string
	RootPath       string
	RootIndex      int
	RootTotal      int
	RootsCompleted int
	Visited        int
	Added          int
	Updated        int
	Skipped        int
	LocalMetadata  int
	Reconciled     int
	Removed        int64
	Errors         int
}

type ScanProgressFunc func(ScanProgress)

const (
	ScanProgressRootStarted  = "root_started"
	ScanProgressRunning      = "running"
	ScanProgressRootFinished = "root_finished"
	ScanProgressRootFailed   = "root_failed"
	maxScanChangeDetails     = 200
	scanProgressEvery        = 100
)

func (p ScanProgress) Metrics() map[string]int64 {
	return map[string]int64{
		"roots_total":     int64(p.RootTotal),
		"roots_completed": int64(p.RootsCompleted),
		"visited":         int64(p.Visited),
		"added":           int64(p.Added),
		"updated":         int64(p.Updated),
		"skipped":         int64(p.Skipped),
		"local_metadata":  int64(p.LocalMetadata),
		"reconciled":      int64(p.Reconciled),
		"removed":         p.Removed,
		"errors":          int64(p.Errors),
	}
}

type ScanChangeAction string

const (
	ScanChangeAdded   ScanChangeAction = "added"
	ScanChangeUpdated ScanChangeAction = "updated"
	ScanChangeRemoved ScanChangeAction = "removed"
)

// ScanChange records one persisted media change for administrator task logs.
type ScanChange struct {
	Action ScanChangeAction `json:"action"`
	Path   string           `json:"path"`
	Reason string           `json:"reason,omitempty"`
}

func (res *ScanResult) addChange(action ScanChangeAction, path, reason string) {
	if res == nil || strings.TrimSpace(path) == "" {
		return
	}
	if action == ScanChangeUpdated && strings.TrimSpace(reason) == "" {
		reason = "已有记录重新入库"
	}
	if len(res.Changes) >= maxScanChangeDetails {
		res.OmittedChanges++
		return
	}
	res.Changes = append(res.Changes, ScanChange{Action: action, Path: path, Reason: reason})
}

func (res *ScanResult) ChangeDetails() []string {
	if res == nil {
		return nil
	}
	out := make([]string, 0, len(res.Changes)+1)
	if res.Reconciled > 0 {
		out = append(out, fmt.Sprintf("🧹 纠正 %d 条电影库季集脏数据", res.Reconciled))
	}
	for _, change := range res.Changes {
		switch change.Action {
		case ScanChangeAdded:
			out = append(out, "➕ 新增 "+change.Path)
		case ScanChangeUpdated:
			out = append(out, "🔄 更新 "+change.Path+"（"+change.Reason+"）")
		case ScanChangeRemoved:
			out = append(out, "🗑️ 删除 "+change.Path)
		}
	}
	if res.OmittedChanges > 0 {
		out = append(out, fmt.Sprintf("ℹ️ 另有 %d 条媒体变化未展开", res.OmittedChanges))
	}
	return out
}

// ErrorDetails 返回任务日志使用的脱敏错误明细，不展开成功项。
func (res *ScanResult) ErrorDetails(limit int) []string {
	if res == nil || limit <= 0 {
		return nil
	}
	var details []string
	for _, line := range res.Errors {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		details = append(details, "❌ "+sanitizeTaskLogError(errors.New(line)).Error())
		if len(details) >= limit {
			break
		}
	}
	if omitted := res.ErrorCount - len(details); omitted > 0 {
		details = append(details, fmt.Sprintf("⚠️ 另有 %d 条扫描错误未展开", omitted))
	}
	return details
}

var ErrLocalScanAlreadyRunning = errors.New("local scan already running")

const maxScanErrorDetails = 20

func addScanError(res *ScanResult, path string, err error) {
	if res == nil || err == nil {
		return
	}
	res.ErrorCount++
	if len(res.Errors) >= maxScanErrorDetails {
		return
	}
	path = strings.TrimSpace(path)
	msg := strings.TrimSpace(err.Error())
	if path != "" {
		msg = path + ": " + msg
	}
	res.Errors = append(res.Errors, msg)
}

type existingLocalMedia struct {
	SeasonNum         int
	ScanFileSizeBytes int64
	ScanFileMTimeNS   int64
	FileID            string
}
