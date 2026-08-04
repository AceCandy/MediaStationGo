package service

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
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
	if s == nil || s.probe == nil {
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
	if s == nil || s.probe == nil || strings.TrimSpace(task.path) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	if s.mediaProbe != nil {
		media, err := s.repo.Media.FindByPath(ctx, task.path)
		if err == nil && media != nil {
			probe, probeErr := s.mediaProbe.ProbeMedia(ctx, media.ID)
			cancel()
			if probeErr != nil {
				if s.log != nil {
					s.log.Debug("local media async probe failed", zap.String("path", task.path), zap.Error(probeErr))
				}
				return
			}
			s.publishLocalProbeResult(task.path, probe)
			return
		}
	}
	probe, err := s.probe.Probe(ctx, task.probePath)
	cancel()
	if err != nil {
		if s.log != nil {
			s.log.Debug("local media async probe failed", zap.String("path", task.path), zap.Error(err))
		}
		return
	}
	updates := localProbeResultUpdates(probe, task.probePath)
	if len(updates) == 0 {
		return
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	if task.path != task.probePath {
		var current model.Media
		if err := s.repo.DB.WithContext(writeCtx).Where("path = ?", task.path).First(&current).Error; err != nil || localSTRMFileTarget(&current) != task.probePath {
			return
		}
	}
	if err := s.repo.DB.WithContext(writeCtx).Model(&model.Media{}).Where("path = ?", task.path).Updates(updates).Error; err != nil {
		if s.log != nil {
			s.log.Debug("update local media track metadata failed", zap.String("path", task.path), zap.Error(err))
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
