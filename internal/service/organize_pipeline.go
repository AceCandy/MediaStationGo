package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

type OrganizeScope string

const (
	OrganizeScopeDirectory OrganizeScope = "directory"
	OrganizeScopeMedia     OrganizeScope = "media"
	OrganizeScopeLibrary   OrganizeScope = "library"
)

type OrganizeTrigger string

const (
	OrganizeTriggerManual    OrganizeTrigger = "manual"
	OrganizeTriggerScheduled OrganizeTrigger = "scheduled"
	OrganizeTriggerDownload  OrganizeTrigger = "download"
)

// OrganizePipelineRequest is the single service-facing entry point for every
// organize → rename → scan → scrape ingest workflow. Handlers, scheduler jobs
// and download-complete hooks should only decide the trigger/scope/options;
// the pipeline owns the execution order and task reporting.
type OrganizePipelineRequest struct {
	Scope              OrganizeScope
	Trigger            OrganizeTrigger
	TaskName           string
	MediaID            string
	LibraryID          string
	PreferredLibraryID string
	SourcePath         string
	DestPath           string
	TransferMode       string
	MediaType          string
	MediaCategory      string
	ScanAfter          *bool
	ScrapeAfter        *bool
	DryRun             bool
	AllowReplace       bool
}

type OrganizePipelineResponse struct {
	Path   string
	Result *OrganizeResult
}

type OrganizePipelineService struct {
	log       *zap.Logger
	repo      *repository.Container
	organizer *OrganizerService
	scanner   *ScannerService
	tasks     *TaskTrackerService
}

func NewOrganizePipelineService(log *zap.Logger, repo *repository.Container, organizer *OrganizerService, scanner *ScannerService, tasks *TaskTrackerService) *OrganizePipelineService {
	return &OrganizePipelineService{
		log:       log,
		repo:      repo,
		organizer: organizer,
		scanner:   scanner,
		tasks:     tasks,
	}
}

func (p *OrganizePipelineService) Run(ctx context.Context, req OrganizePipelineRequest) (*OrganizePipelineResponse, error) {
	if p == nil || p.organizer == nil {
		return nil, errors.New("organize pipeline unavailable")
	}
	opts := OrganizeOptions{
		SourcePath:           strings.TrimSpace(req.SourcePath),
		DestPath:             strings.TrimSpace(req.DestPath),
		MediaType:            strings.TrimSpace(req.MediaType),
		MediaCategory:        strings.TrimSpace(req.MediaCategory),
		DryRun:               req.DryRun,
		AllowReplaceExisting: req.AllowReplace,
	}
	if mode := strings.TrimSpace(req.TransferMode); mode != "" {
		opts.TransferMode = TransferMode(mode)
	}
	task := p.startTask(ctx, req, opts)

	response := &OrganizePipelineResponse{}
	res, path, err := p.runOrganize(ctx, req, opts)
	if err != nil {
		p.finishTask(task, err, "organize", p.failureMessage(req), nil)
		return nil, err
	}
	response.Path = path
	response.Result = res

	if task != nil {
		task.Update(TaskUpdate{
			Stage:      "organize",
			SourcePath: res.SourcePath,
			DestPath:   firstNonEmpty(res.DestPath, filepath.Dir(path)),
			Message:    "整理/重命名完成，准备扫描入库",
			Metrics:    OrganizeTaskMetrics(res),
			Details:    OrganizeTaskDetails(res, 8),
		})
	}

	if p.shouldScan(req, res) {
		if task != nil {
			task.Update(TaskUpdate{
				Stage:   "scan_scrape",
				Message: "正在扫描入库并按设置刮削",
				Metrics: OrganizeTaskMetrics(res),
				Details: OrganizeTaskDetails(res, 8),
			})
		}
		scanRoot := organizeScanRoot(res, path)
		if scanRoot == "" && strings.TrimSpace(path) != "" {
			scanRoot = filepath.Dir(path)
		}
		preferredLibraryID := strings.TrimSpace(req.PreferredLibraryID)
		if preferredLibraryID == "" && req.Scope == OrganizeScopeLibrary {
			preferredLibraryID = strings.TrimSpace(req.LibraryID)
		}
		res.Scans, res.Scrapes = p.scanner.ScanAndScrapeLibrariesForPath(ctx, scanRoot, preferredLibraryID, p.scrapeAfter(ctx, req))
	} else if p.log != nil && res != nil && !req.DryRun {
		p.log.Info("organize pipeline skipped scan; no destination changes",
			zap.String("trigger", string(req.Trigger)),
			zap.String("scope", string(req.Scope)),
			zap.String("source", res.SourcePath),
			zap.String("dest", res.DestPath),
			zap.Int("organized", res.Organized),
			zap.Int("replaced", res.Replaced),
			zap.Int("skipped", res.Skipped))
	}

	p.finishTask(task, nil, "completed", p.completedMessage(req), res)
	if p.log != nil && res != nil {
		p.log.Info("organize pipeline finished",
			zap.String("trigger", string(req.Trigger)),
			zap.String("scope", string(req.Scope)),
			zap.String("source", res.SourcePath),
			zap.String("dest", firstNonEmpty(res.DestPath, filepath.Dir(path))),
			zap.Int("organized", res.Organized),
			zap.Int("replaced", res.Replaced),
			zap.Int("skipped", res.Skipped),
			zap.Int("scrapes", len(res.Scrapes)),
			zap.Int("errors", len(res.Errors)))
	}
	return response, nil
}

func (p *OrganizePipelineService) runOrganize(ctx context.Context, req OrganizePipelineRequest, opts OrganizeOptions) (*OrganizeResult, string, error) {
	switch req.Scope {
	case OrganizeScopeMedia:
		path, err := p.organizer.OrganizeMediaWithOptions(ctx, strings.TrimSpace(req.MediaID), opts)
		if err != nil {
			return nil, "", err
		}
		res := &OrganizeResult{
			Organized:  1,
			SourcePath: opts.SourcePath,
			DestPath:   filepath.Dir(path),
			DryRun:     opts.DryRun,
			Items: []OrganizePreviewItem{{
				Target: path,
				Action: "organize",
			}},
		}
		return res, path, nil
	case OrganizeScopeLibrary:
		res, err := p.organizer.OrganizeLibraryWithOptions(ctx, strings.TrimSpace(req.LibraryID), opts)
		return res, "", err
	default:
		res, err := p.organizer.OrganizeDirectory(ctx, opts)
		return res, "", err
	}
}

func (p *OrganizePipelineService) shouldScan(req OrganizePipelineRequest, res *OrganizeResult) bool {
	if req.DryRun || p == nil || p.scanner == nil || res == nil {
		return false
	}
	if req.ScanAfter != nil && !*req.ScanAfter {
		return false
	}
	return OrganizeResultNeedsVisibilitySync(res)
}

func organizeScanRoot(res *OrganizeResult, path string) string {
	if res == nil {
		if strings.TrimSpace(path) == "" {
			return ""
		}
		return filepath.Dir(path)
	}
	var root string
	for _, item := range res.Items {
		if !organizeItemNeedsVisibilitySync(item) {
			continue
		}
		target := strings.TrimSpace(item.Target)
		if target == "" {
			continue
		}
		dir := filepath.Dir(target)
		if root == "" {
			root = dir
			continue
		}
		root = commonPathRoot(root, dir)
	}
	if root != "" {
		return root
	}
	if strings.TrimSpace(path) != "" {
		return filepath.Dir(path)
	}
	return strings.TrimSpace(res.DestPath)
}

func organizeItemNeedsVisibilitySync(item OrganizePreviewItem) bool {
	switch item.Action {
	case "organize", "replace":
		return true
	case "skip":
		switch item.Reason {
		case organizeSkipAlreadyOrganized, organizeSkipTargetExists, "duplicate exists", "target exists":
			return true
		}
	}
	return false
}

func commonPathRoot(a, b string) string {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "" || a == "." {
		return b
	}
	if b == "" || b == "." {
		return a
	}
	if pathWithin(a, b) {
		return b
	}
	if pathWithin(b, a) {
		return a
	}
	for {
		parent := filepath.Dir(a)
		if parent == a || parent == "." {
			return parent
		}
		if pathWithin(b, parent) {
			return parent
		}
		a = parent
	}
}

func (p *OrganizePipelineService) scrapeAfter(ctx context.Context, req OrganizePipelineRequest) bool {
	if req.ScrapeAfter != nil {
		return *req.ScrapeAfter
	}
	return OrganizeScrapeAfterEnabled(ctx, p.repo)
}

func (p *OrganizePipelineService) startTask(ctx context.Context, req OrganizePipelineRequest, opts OrganizeOptions) *TaskHandle {
	if p == nil || p.tasks == nil {
		return nil
	}
	name := strings.TrimSpace(req.TaskName)
	if name == "" {
		name = p.defaultTaskName(req)
	}
	message := "正在整理/重命名/入库"
	if req.DryRun {
		message = "正在预览整理/重命名"
	}
	return p.tasks.Start(TaskKindOrganize, name, TaskUpdate{
		Stage:      "organize",
		SourcePath: firstNonEmpty(opts.SourcePath, p.defaultSourcePath(ctx, req)),
		DestPath:   firstNonEmpty(opts.DestPath, p.defaultDestPath(ctx, req)),
		Message:    message,
	})
}

func (p *OrganizePipelineService) finishTask(task *TaskHandle, err error, stage, message string, res *OrganizeResult) {
	if task == nil {
		return
	}
	task.Finish(err, TaskUpdate{
		Stage:   stage,
		Message: message,
		Metrics: OrganizeTaskMetrics(res),
		Details: OrganizeTaskDetails(res, 8),
	})
}

func (p *OrganizePipelineService) defaultTaskName(req OrganizePipelineRequest) string {
	switch req.Trigger {
	case OrganizeTriggerScheduled:
		return "自动整理重命名刮削入库"
	case OrganizeTriggerDownload:
		return "下载完成自动整理重命名刮削入库"
	default:
		if req.DryRun {
			return "预览整理重命名入库"
		}
		return "手动整理重命名刮削入库"
	}
}

func (p *OrganizePipelineService) failureMessage(req OrganizePipelineRequest) string {
	switch req.Trigger {
	case OrganizeTriggerScheduled:
		return "自动整理重命名入库失败"
	case OrganizeTriggerDownload:
		return "下载完成自动整理失败"
	default:
		return "手动整理重命名入库失败"
	}
}

func (p *OrganizePipelineService) completedMessage(req OrganizePipelineRequest) string {
	switch req.Trigger {
	case OrganizeTriggerScheduled:
		return "自动整理重命名刮削入库结束"
	case OrganizeTriggerDownload:
		return "下载完成自动整理入库结束"
	default:
		return "手动整理重命名刮削入库结束"
	}
}

func (p *OrganizePipelineService) defaultSourcePath(ctx context.Context, req OrganizePipelineRequest) string {
	if p == nil || p.organizer == nil {
		return ""
	}
	if req.Scope == OrganizeScopeDirectory {
		return p.organizer.defaultSourceRoot(ctx, "")
	}
	return ""
}

func (p *OrganizePipelineService) defaultDestPath(ctx context.Context, req OrganizePipelineRequest) string {
	if p == nil || p.organizer == nil {
		return ""
	}
	if req.Scope == OrganizeScopeDirectory {
		return p.organizer.defaultDestRoot(ctx, "")
	}
	return ""
}
