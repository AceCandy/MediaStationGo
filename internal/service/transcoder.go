// Package service — HLS on-demand transcoder.
//
// TranscoderService spawns ffmpeg processes that segment a source media file
// into HLS (.m3u8 + .ts). The output lives under cache.cache_dir/hls/<id>.
// The HTTP layer serves these files directly with a normal http.FileServer.
//
// Encoder selection (read once at startup from the config):
//
//	transcoder.encoder = "" | "nvenc" | "qsv" | "vaapi"
//
//	""      software libx264 (default; runs anywhere)
//	nvenc   h264_nvenc      (NVIDIA GPU, requires --gpus all on Docker)
//	qsv     h264_qsv        (Intel iGPU, requires /dev/dri:/dev/dri)
//	vaapi   h264_vaapi      (Mesa/Intel VAAPI, requires /dev/dri:/dev/dri
//	                         plus the kernel module loaded)
//
// Concurrency model:
//   - Each media/audio selection has at most one active ffmpeg job.
//   - jobs[key] tracks the running goroutine + cancel func.
//   - Calling Start while a job already exists is a no-op.
//   - When the playlist file appears on disk we consider the job "ready"
//     and unblock the HTTP handler that was waiting on it.
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// TranscoderService orchestrates background ffmpeg transcodes.
type TranscoderService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
	hub  *Hub

	mu   sync.Mutex
	jobs map[string]*hlsJob
}

// hlsJob holds the live state of one ffmpeg run.
type hlsJob struct {
	key              TranscodeKey
	mediaID          string
	audioStreamIndex int
	outputDir        string
	cancel           context.CancelFunc
	startedAt        time.Time
	lastAccess       time.Time
	playlistOK       bool
	encoder          string
}

var (
	// ErrTranscodeDisabled is returned when HLS transcoding is globally disabled.
	ErrTranscodeDisabled = errors.New("transcode disabled")
	// ErrTranscodeBusy is returned when the server has reached its configured
	// ffmpeg concurrency limit.
	ErrTranscodeBusy = errors.New("transcode concurrency limit reached")
)

// NewTranscoderService is the constructor.
func NewTranscoderService(cfg *config.Config, log *zap.Logger, repo *repository.Container, hub *Hub) *TranscoderService {
	return &TranscoderService{
		cfg:  cfg,
		log:  log,
		repo: repo,
		hub:  hub,
		jobs: make(map[string]*hlsJob),
	}
}

// HLSDir is the per-media directory that holds index.m3u8 + segment files.
func (t *TranscoderService) HLSDir(key TranscodeKey) string {
	return filepath.Join(t.cfg.Cache.CacheDir, "hls", key.String())
}

// PlaylistPath returns the absolute path of the m3u8 playlist for a media.
func (t *TranscoderService) PlaylistPath(key TranscodeKey) string {
	return filepath.Join(t.HLSDir(key), "index.m3u8")
}

// EnsureJob makes sure a transcode is running for mediaID. The function is
// non-blocking: it returns the playlist path immediately. The caller is
// expected to poll until WaitReady reports true.
func (t *TranscoderService) EnsureJob(ctx context.Context, key TranscodeKey) (string, error) {
	if !t.cfg.Transcoder.Enabled {
		return "", ErrTranscodeDisabled
	}
	m, err := t.repo.Media.FindByID(ctx, key.MediaID)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", ErrMediaNotFound
	}
	source := m.Path
	if target := localSTRMFileTarget(m); target != "" {
		source = target
	}
	if _, err := os.Stat(source); err != nil {
		return "", ErrMediaNotFound
	}
	if _, err := t.resolveFFmpegPath(); err != nil {
		return "", err
	}

	t.mu.Lock()
	jobID := key.String()
	if _, ok := t.jobs[jobID]; ok {
		t.touchJobLocked(jobID)
		t.mu.Unlock()
		return t.PlaylistPath(key), nil
	}
	if max := t.maxConcurrent(); max > 0 && len(t.jobs) >= max {
		t.mu.Unlock()
		return "", ErrTranscodeBusy
	}

	outDir := t.HLSDir(key)
	if err := os.RemoveAll(outDir); err != nil {
		t.mu.Unlock()
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.mu.Unlock()
		return "", err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	job := &hlsJob{
		key: key, mediaID: key.MediaID, audioStreamIndex: key.AudioStreamIndex,
		outputDir:  outDir,
		cancel:     cancel,
		startedAt:  time.Now(),
		lastAccess: time.Now(),
		encoder:    t.effectiveEncoder(),
	}
	t.jobs[jobID] = job
	t.mu.Unlock()

	go t.monitorIdle(jobCtx, job)
	go t.runFFmpeg(jobCtx, job, source)
	return t.PlaylistPath(key), nil
}
