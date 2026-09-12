package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func (s *ScraperService) queueCatalogArtwork(ctx context.Context, id string) error {
	if err := s.repo.Metadata.QueueCatalogArtwork(ctx, id); err != nil {
		return err
	}
	select {
	case s.catalogArtworkWake <- struct{}{}:
	default:
	}
	return nil
}

// StartCatalogArtworkWorker 恢复并消费首次图片待办；巡检开关不影响已提交的下载。
func (s *ScraperService) StartCatalogArtworkWorker(ctx context.Context) {
	s.catalogArtworkOnce.Do(func() {
		if s.catalogArtworkWake == nil {
			s.catalogArtworkWake = make(chan struct{}, 1)
		}
		s.catalogHydrationWG.Add(1)
		go func() {
			defer s.catalogHydrationWG.Done()
			for ctx.Err() == nil {
				next, err := s.repo.Metadata.NextCatalogArtworkAt(ctx)
				if err == nil && next != nil && !next.After(time.Now()) {
					err = s.runTMDbArtworkLocalRepair(ctx, TaskTriggerEvent)
					if err == nil {
						continue
					}
				}
				var timer *time.Timer
				var timerC <-chan time.Time
				delay := time.Duration(0)
				if next != nil {
					delay = max(time.Until(*next), time.Second)
				}
				if err != nil {
					delay = time.Minute
					if s.log != nil && ctx.Err() == nil && !errors.Is(err, ErrSchedulerJobAlreadyRunning) {
						s.log.Warn("catalog artwork pass failed", zap.Error(sanitizeTaskLogError(err)))
					}
				}
				if delay > 0 {
					timer = time.NewTimer(delay)
					timerC = timer.C
				}
				select {
				case <-ctx.Done():
				case <-s.catalogArtworkWake:
				case <-timerC:
				}
				if timer != nil {
					timer.Stop()
				}
			}
		}()
	})
}

// downloadPendingCatalogArtwork 固定本轮截止时间，失败条目留到退避后再处理。
func (s *ScraperService) downloadPendingCatalogArtwork(ctx context.Context, task *TaskHandle, metrics map[string]int64) error {
	cutoff := time.Now().UTC()
	for {
		items, err := s.repo.Metadata.ListDueCatalogArtwork(ctx, cutoff, artworkRepairPageLimit)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		for _, item := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			metrics["pending_scanned"]++
			err := s.downloadCatalogArtwork(ctx, item, metrics)
			s.invalidateMediaCache(ctx)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			detail := fmt.Sprintf("✅ %s，kind=%s，动作=首次图片下载，结果=已处理", item.Title, item.Kind)
			if err != nil {
				metrics["failed"]++
				if retryErr := s.repo.Metadata.RetryCatalogArtwork(ctx, item, time.Now().UTC().Add(catalogRetryDelay(item.CatalogArtworkAttempts+1))); retryErr != nil {
					return retryErr
				}
				detail = fmt.Sprintf("❌ %s，kind=%s，动作=首次图片下载，结果=留待重试：%v", item.Title, item.Kind, sanitizeTaskLogError(err))
			}
			if task != nil {
				task.Update(TaskUpdate{Stage: "download", Message: "正在下载已入库资料的图片", Metrics: metrics, Details: []string{detail}})
			}
		}
	}
}

func (s *ScraperService) downloadCatalogArtwork(ctx context.Context, item model.MetadataItem, metrics map[string]int64) error {
	snapshot, err := s.repo.Metadata.FindProviderSnapshot(ctx, item.ID, "tmdb")
	if err != nil {
		return err
	}
	if snapshot == nil || s.tmdb == nil {
		return errors.New("catalog TMDb snapshot unavailable")
	}
	var fields struct {
		Poster   string `json:"poster_path"`
		Backdrop string `json:"backdrop_path"`
		Still    string `json:"still_path"`
	}
	if err := json.Unmarshal([]byte(snapshot.Payload), &fields); err != nil {
		return err
	}
	sources := map[string]string{}
	switch item.Kind {
	case model.MetadataKindMovie, model.MetadataKindSeries:
		sources[model.ArtworkTypePoster], sources[model.ArtworkTypeBackdrop] = fields.Poster, fields.Backdrop
	case model.MetadataKindSeason:
		sources[model.ArtworkTypePoster] = fields.Poster
	case model.MetadataKindEpisode:
		sources[model.ArtworkTypeStill] = fields.Still
	default:
		return errors.New("unsupported catalog artwork kind")
	}
	var missing []string
	var failures []error
	for _, kind := range []string{model.ArtworkTypePoster, model.ArtworkTypeBackdrop, model.ArtworkTypeStill} {
		path, ok := sources[kind]
		if !ok {
			continue
		}
		if strings.TrimSpace(path) == "" {
			missing = append(missing, kind)
			metrics["source_missing"]++
			continue
		}
		if err := s.downloadCatalogImage(ctx, item, snapshot, kind, tmdbOriginalImageURL(s.tmdb.imgCDN, path), metrics); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", kind, err))
		}
	}
	if item.Kind != model.MetadataKindEpisode {
		if err := s.downloadCatalogProfiles(ctx, item.ID, metrics); err != nil {
			failures = append(failures, err)
		}
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	return s.repo.Metadata.CompleteCatalogArtwork(ctx, item, snapshot, missing)
}

func (s *ScraperService) downloadCatalogImage(ctx context.Context, item model.MetadataItem, snapshot *model.MetadataProviderSnapshot, kind, source string, metrics map[string]int64) error {
	selected, err := s.repo.Artwork.FindSelection(ctx, item.ID, kind)
	if err != nil {
		return err
	}
	if selected != nil {
		metrics["existing"]++
		return nil
	}
	if s.artwork.imageProxy == nil {
		return errors.New("image proxy unavailable")
	}
	if err := s.artwork.imageProxy.RemoveFailed(source); err != nil {
		return err
	}
	data, _, err := s.artwork.imageProxy.Fetch(ctx, source)
	if err != nil {
		return err
	}
	asset, err := s.artwork.prepareAsset(item.ID, kind, data)
	if err != nil {
		return err
	}
	saved, err := s.repo.Metadata.SaveCatalogArtworkAsset(ctx, item, snapshot, kind, source, asset)
	if saved {
		metrics["downloaded"]++
	}
	return err
}

func (s *ScraperService) downloadCatalogProfiles(ctx context.Context, metadataID string, metrics map[string]int64) error {
	credits, err := s.repo.Person.ListCreditsWithPeople(ctx, metadataID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var failures []error
	for _, credit := range credits {
		person := credit.Person
		if seen[person.ID] || person.Source != "tmdb" || strings.TrimSpace(person.ProfileURL) == "" {
			continue
		}
		seen[person.ID] = true
		if s.people == nil {
			return errors.New("people image store unavailable")
		}
		if person.ProfileImageSourceURL == person.ProfileURL && s.people.hasUsableImage(person.ProfileImageKey) {
			continue
		}
		key, err := s.people.ImportCached(ctx, person.ProfileURL)
		if err != nil {
			failures = append(failures, fmt.Errorf("person %s: %w", person.ID, err))
			continue
		}
		res := s.repo.DB.WithContext(ctx).Model(&model.Person{}).
			Where("id = ? AND profile_url = ? AND COALESCE(profile_image_key, '') = ?", person.ID, person.ProfileURL, person.ProfileImageKey).
			Updates(map[string]any{"profile_image_key": key, "profile_image_source_url": person.ProfileURL})
		if res.Error != nil {
			failures = append(failures, res.Error)
		} else {
			metrics["profiles_saved"] += res.RowsAffected
		}
	}
	return errors.Join(failures...)
}
