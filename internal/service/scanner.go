// Package service — filesystem scanner.
//
// ScannerService walks the configured library roots looking for video
// files, then upserts a model.Media row per file. Each upsert also runs
// ffprobe (when available) and queues a metadata lookup for newly added
// rows.
//
// When a filename exposes season + episode numbers we store them on the
// Media row for every library type, so variety shows and other episodic
// collections get the same grouping experience as TV/anime.
package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// videoExtensions lists the file extensions treated as media. Matches the
// MediaStation Python defaults.
var videoExtensions = map[string]struct{}{
	".mkv":  {},
	".mp4":  {},
	".m4v":  {},
	".avi":  {},
	".mov":  {},
	".webm": {},
	".ts":   {},
	".rmvb": {},
	".rm":   {},
	".3gp":  {},
	".mpg":  {},
	".mpeg": {},
	".strm": {},
}

// ScannerService walks libraries on disk and upserts model.Media rows.
type ScannerService struct {
	cfg     *config.Config
	log     *zap.Logger
	repo    *repository.Container
	hub     *Hub
	probe   *FFprobeService
	scraper *ScraperService
}

// NewScannerService is the constructor.
func NewScannerService(
	cfg *config.Config,
	log *zap.Logger,
	repo *repository.Container,
	hub *Hub,
	probe *FFprobeService,
	scraper *ScraperService,
) *ScannerService {
	return &ScannerService{
		cfg: cfg, log: log, repo: repo, hub: hub,
		probe: probe, scraper: scraper,
	}
}

// ScanResult summarises a scan run.
type ScanResult struct {
	LibraryID     string `json:"library_id"`
	Visited       int    `json:"visited"`
	Added         int    `json:"added"`
	Updated       int    `json:"updated"`
	Probed        int    `json:"probed"`
	LocalMetadata int    `json:"local_metadata"`
	Removed       int64  `json:"removed"`
}

// ScanLibrary walks the library root and persists discovered media files.
func (s *ScannerService) ScanLibrary(ctx context.Context, libraryID string) (*ScanResult, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil {
		return nil, err
	}
	res := &ScanResult{LibraryID: lib.ID}
	seen := make(map[string]struct{})

	walkFn := func(path string, info walkInfo) error {
		if info.isDir {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := videoExtensions[ext]; !ok {
			return nil
		}
		res.Visited++
		seen[filepath.Clean(path)] = struct{}{}
		isNewMedia := !s.mediaPathExists(ctx, path)

		title, year := CleanQuery(path)
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(path), ext)
		}

		m := &model.Media{
			LibraryID: lib.ID,
			Title:     title,
			Year:      year,
			Path:      path,
			SizeBytes: info.size,
			Container: strings.TrimPrefix(ext, "."),
		}

		parsedSeason, parsedEpisode := ParseEpisode(path)
		m.SeasonNum = parsedSeason
		m.EpisodeNum = parsedEpisode

		if local, err := ReadLocalMetadata(path, lib.Path, librarySupportsSeasons(lib) || parsedSeason > 0 || parsedEpisode > 0); err == nil && local != nil {
			applyLocalMetadata(m, local)
			res.LocalMetadata++
		} else if err != nil {
			s.log.Warn("read local metadata failed", zap.String("path", path), zap.Error(err))
		}

		// Best-effort ffprobe; failure does not abort the file.
		if s.probe != nil {
			if probe, err := s.probe.Probe(ctx, path); err == nil && probe != nil {
				m.DurationSec = probe.DurationSec
				m.Width = probe.Width
				m.Height = probe.Height
				m.VideoCodec = probe.VideoCodec
				m.AudioCodec = probe.AudioCodec
				if probe.Container != "" {
					m.Container = probe.Container
				}
				res.Probed++
			} else if err != nil {
				s.log.Debug("ffprobe failed", zap.String("path", path), zap.Error(err))
			}
		}

		if err := s.repo.Media.Upsert(ctx, m); err != nil {
			s.log.Warn("upsert media failed", zap.String("path", path), zap.Error(err))
			return nil
		}
		if isNewMedia {
			res.Added++
		} else {
			res.Updated++
		}
		s.hub.Publish("scan", map[string]any{
			"library_id": lib.ID,
			"path":       path,
			"visited":    res.Visited,
			"added":      res.Added,
			"updated":    res.Updated,
			"probed":     res.Probed,
			"local_meta": res.LocalMetadata,
		})
		return nil
	}

	if err := walk(lib.Path, walkFn); err != nil {
		return res, err
	}
	removed, err := s.pruneMissingMedia(ctx, lib.ID, seen)
	if err != nil {
		s.log.Warn("prune missing media failed", zap.String("library_id", lib.ID), zap.Error(err))
	} else {
		res.Removed = removed
	}

	s.hub.Publish("scan", map[string]any{
		"library_id": lib.ID,
		"finished":   true,
		"visited":    res.Visited,
		"added":      res.Added,
		"updated":    res.Updated,
		"probed":     res.Probed,
		"local_meta": res.LocalMetadata,
		"removed":    res.Removed,
	})

	// Online enrichment is opt-in. Local NFO is always consumed first during
	// the scan, and matched rows are excluded from EnrichLibrary's pending set.
	if s.scraper != nil && s.scraper.AnyEnabled() && s.autoScrapeEnabled(ctx) {
		go func(libID string) {
			if _, err := s.scraper.EnrichLibrary(context.Background(), libID); err != nil {
				s.log.Warn("scraper enrich failed", zap.Error(err))
			}
		}(lib.ID)
	}
	return res, nil
}

func (s *ScannerService) mediaPathExists(ctx context.Context, path string) bool {
	var count int64
	err := s.repo.DB.WithContext(ctx).Unscoped().Model(&model.Media{}).
		Where("path = ?", path).Count(&count).Error
	return err == nil && count > 0
}

func (s *ScannerService) pruneMissingMedia(ctx context.Context, libraryID string, seen map[string]struct{}) (int64, error) {
	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).
		Where("library_id = ?", libraryID).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	var removed int64
	for _, row := range rows {
		if row.Path == "" {
			continue
		}
		if _, ok := seen[filepath.Clean(row.Path)]; ok {
			continue
		}
		if _, err := os.Stat(row.Path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			continue
		}
		res := s.repo.DB.WithContext(ctx).
			Where("id = ?", row.ID).
			Delete(&model.Media{})
		if res.Error != nil {
			return removed, res.Error
		}
		removed += res.RowsAffected
	}
	return removed, nil
}

func applyLocalMetadata(m *model.Media, local *LocalMetadata) {
	if local.Title != "" {
		m.Title = local.Title
	}
	if local.OriginalName != "" {
		m.OriginalName = local.OriginalName
	}
	if local.AdultCode != "" {
		m.OriginalName = local.AdultCode
	}
	if local.Year > 0 {
		m.Year = local.Year
	}
	if local.Overview != "" {
		m.Overview = local.Overview
	}
	if local.Rating > 0 {
		m.Rating = local.Rating
	}
	if local.PosterURL != "" {
		m.PosterURL = local.PosterURL
	}
	if local.BackdropURL != "" {
		m.BackdropURL = local.BackdropURL
	}
	if local.TMDbID > 0 {
		m.TMDbID = local.TMDbID
	}
	if local.SeasonNum > 0 {
		m.SeasonNum = local.SeasonNum
	}
	if local.EpisodeNum > 0 {
		m.EpisodeNum = local.EpisodeNum
	}
	if local.Genres != "" {
		m.Genres = local.Genres
	}
	if local.Countries != "" {
		m.Countries = local.Countries
	}
	if local.Languages != "" {
		m.Languages = local.Languages
	}
	if local.NSFW {
		m.NSFW = true
	}
	if local.HasNFO || localHasDescriptiveMetadata(local) {
		m.ScrapeStatus = "matched"
	}
}

func localHasDescriptiveMetadata(local *LocalMetadata) bool {
	if local == nil {
		return false
	}
	return local.Title != "" ||
		local.OriginalName != "" ||
		local.AdultCode != "" ||
		local.Year > 0 ||
		local.Overview != "" ||
		local.Rating > 0 ||
		local.TMDbID > 0 ||
		local.Genres != "" ||
		local.Countries != "" ||
		local.Languages != ""
}

func (s *ScannerService) autoScrapeEnabled(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	value, err := s.repo.Setting.Get(ctx, "scrape.auto_on_scan")
	if err != nil {
		s.log.Warn("read scrape.auto_on_scan failed", zap.Error(err))
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}
