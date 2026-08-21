package service

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"
)

func (s *ScannerService) localMediaProbeWorker() {
	for task := range s.localMediaProbeQueue {
		s.probeLocalMediaAsync(task)
	}
}

func (s *ScannerService) queueLocalMediaProbe(ctx context.Context, path, probePath string) bool {
	task, ok := s.newLocalMediaProbeTask(path, probePath)
	if !ok {
		return false
	}
	s.startLocalMediaProbeWorkers()
	if !s.reserveLocalMediaProbe(task.path) {
		return false
	}
	if s.enqueueLocalMediaProbe(ctx, task) {
		return true
	}
	s.releaseLocalMediaProbe(task.path)
	s.logLocalMediaProbeQueueFull(task)
	return false
}

func (s *ScannerService) newLocalMediaProbeTask(path, probePath string) (localMediaProbeTask, bool) {
	if s == nil || (s.probe == nil && s.mediaProbe == nil) {
		return localMediaProbeTask{}, false
	}
	task := localMediaProbeTask{path: strings.TrimSpace(path), probePath: strings.TrimSpace(probePath)}
	return task, task.path != "" && task.probePath != ""
}

func (s *ScannerService) startLocalMediaProbeWorkers() {
	s.localMediaProbeOnce.Do(func() {
		workers := s.ffprobeWorkerCount()
		for i := 0; i < workers; i++ {
			go s.localMediaProbeWorker()
		}
	})
}

func (s *ScannerService) ffprobeWorkerCount() int {
	if s == nil || s.cfg == nil {
		return 1
	}
	return normalizeFFprobeMaxConcurrent(s.cfg.App.FFprobeMaxConcurrent)
}

func (s *ScannerService) reserveLocalMediaProbe(path string) bool {
	s.localMediaProbeMu.Lock()
	defer s.localMediaProbeMu.Unlock()
	if s.localMediaProbing == nil {
		s.localMediaProbing = make(map[string]struct{})
	}
	if _, ok := s.localMediaProbing[path]; ok {
		return false
	}
	s.localMediaProbing[path] = struct{}{}
	return true
}

func (s *ScannerService) releaseLocalMediaProbe(path string) {
	s.localMediaProbeMu.Lock()
	delete(s.localMediaProbing, path)
	s.localMediaProbeMu.Unlock()
}

func (s *ScannerService) enqueueLocalMediaProbe(ctx context.Context, task localMediaProbeTask) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case s.localMediaProbeQueue <- task:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *ScannerService) logLocalMediaProbeQueueFull(task localMediaProbeTask) {
	if s != nil && s.log != nil {
		s.log.Debug("local media probe queue full", zap.String("path", task.path))
	}
}

func (s *ScannerService) probeLocalMediaAsync(task localMediaProbeTask) {
	defer s.releaseLocalMediaProbe(task.path)
	if s == nil || s.mediaProbe == nil || strings.TrimSpace(task.path) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	media, err := s.repo.Media.FindByPath(ctx, task.path)
	if err != nil || media == nil {
		return
	}
	probe, err := s.mediaProbe.ProbeMedia(ctx, media.ID)
	if err != nil {
		if s.log != nil {
			s.log.Debug("local media async probe failed", zap.String("path", task.path), zap.Error(err))
		}
		return
	}
	s.publishLocalProbeResult(task.path, probe)
}

func (s *ScannerService) publishLocalProbeResult(path string, probe *ProbeResult) {
	if s.hub != nil && probe != nil {
		s.hub.Publish("scan", map[string]any{
			"path":          path,
			"track_probed":  true,
			"duration_sec":  probe.DurationSec,
			"video_codec":   probe.VideoCodec,
			"audio_codec":   probe.AudioCodec,
			"width":         probe.Width,
			"height":        probe.Height,
			"probe_message": "本地媒体轨道元数据已后台补齐",
		})
	}
}
