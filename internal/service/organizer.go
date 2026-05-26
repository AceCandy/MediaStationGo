// Package service — media file organizer (auto-rename + move).
//
// OrganizerService takes a source file (typically from a download
// completion) and moves/renames it into the configured library directory
// using a Jinja2-like template. The default templates:
//
//   movie: {Title} ({Year})/{Title} ({Year}).{Ext}
//   tv:    {Title}/Season {Season:02d}/{Title} - S{Season:02d}E{Episode:02d}.{Ext}
//
// We do NOT delete the source after move — the operator can turn that on
// via a config flag. This mirrors MediaStation's "organize after download"
// workflow.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// OrganizerService moves/renames files into library structures.
type OrganizerService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

// NewOrganizerService is the constructor.
func NewOrganizerService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *OrganizerService {
	return &OrganizerService{cfg: cfg, log: log, repo: repo}
}

// OrganizeResult reports what happened.
type OrganizeResult struct {
	Organized int      `json:"organized"`
	Skipped   int      `json:"skipped"`
	Errors    []string `json:"errors,omitempty"`
}

// OrganizeMedia moves a single media file into the target library directory.
// It auto-detects whether the media is a movie or TV episode based on the
// parsed season/episode numbers and builds the destination path accordingly.
// When smart classify is enabled, it adds a category subfolder (e.g., "华语电影").
func (o *OrganizerService) OrganizeMedia(ctx context.Context, mediaID string) (string, error) {
	m, err := o.repo.Media.FindByID(ctx, mediaID)
	if err != nil || m == nil {
		return "", errors.New("media not found")
	}
	lib, err := o.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil || lib == nil {
		return "", errors.New("library not found")
	}
	ext := filepath.Ext(m.Path)
	title := sanitizeFilename(m.Title)
	if title == "" {
		title = "Unknown"
	}

	// Determine category folder (if smart classify is enabled)
	category := o.SmartClassify(ctx, m)

	var dst string
	if lib.Type == "tv" || lib.Type == "anime" {
		// TV: {lib.Path}/[分类]/{Title}/Season XX/{Title} - SxxExx.ext
		season := fmt.Sprintf("Season %02d", m.SeasonNum)
		epTag := fmt.Sprintf("S%02dE%02d", m.SeasonNum, m.EpisodeNum)
		dir := filepath.Join(lib.Path, category, title, season)
		if category == "" {
			dir = filepath.Join(lib.Path, title, season)
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s - %s%s", title, epTag, ext))
	} else {
		// Movie: {lib.Path}/[分类]/{Title} ({Year})/{Title} ({Year}).ext
		folder := title
		if m.Year > 0 {
			folder = fmt.Sprintf("%s (%d)", title, m.Year)
		}
		dir := filepath.Join(lib.Path, category, folder)
		if category == "" {
			dir = filepath.Join(lib.Path, folder)
		}
		dst = filepath.Join(dir, folder+ext)
	}

	// Skip if already in place.
	if m.Path == dst {
		return dst, nil
	}

	// Refuse to overwrite an existing different file. 当多个 release（如
	// 不同字幕组、不同源）刮削后被统一改名，原本不重复的文件会被映射到
	// 同一个目标路径，盲目 move 会导致后者覆盖前者，造成数据丢失。
	if _, err := os.Stat(dst); err == nil {
		o.log.Warn("organize skipped: destination already exists",
			zap.String("media", m.ID),
			zap.String("from", m.Path),
			zap.String("to", dst))
		return dst, nil
	}

	// Create directories.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}

	// Move the file (same filesystem = rename; cross-device = copy+delete).
	if err := moveFile(m.Path, dst); err != nil {
		return "", err
	}

	// Update the database row.
	if err := o.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Where("id = ?", m.ID).
		Update("path", dst).Error; err != nil {
		return dst, err
	}
	o.log.Info("organized",
		zap.String("media", m.ID),
		zap.String("from", m.Path),
		zap.String("to", dst),
		zap.String("category", category),
	)
	return dst, nil
}

// OrganizeLibrary organizes every media row in a library whose file is
// not already in the expected path structure.
func (o *OrganizerService) OrganizeLibrary(ctx context.Context, libraryID string) (*OrganizeResult, error) {
	var rows []model.Media
	if err := o.repo.DB.WithContext(ctx).
		Where("library_id = ? AND deleted_at IS NULL", libraryID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	res := &OrganizeResult{}
	for i := range rows {
		dst, err := o.OrganizeMedia(ctx, rows[i].ID)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %s", rows[i].Title, err.Error()))
			continue
		}
		if dst == rows[i].Path {
			res.Skipped++
		} else {
			res.Organized++
		}
	}
	return res, nil
}

// moveFile tries os.Rename first (instant on same fs), then falls back
// to copy + remove for cross-device moves.
//
// 重要：如果 dst 已经存在，moveFile 会直接报错而不是覆盖。OrganizeMedia
// 已经在调用前做过 stat 检查，这里是第二道防线。
func moveFile(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("destination already exists: %s", dst)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device: read → write → remove.
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	// O_EXCL 保证不会覆盖已存在的目标。
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, werr := f.Write(data); werr != nil {
		f.Close()
		os.Remove(dst)
		return werr
	}
	if cerr := f.Close(); cerr != nil {
		return cerr
	}
	return os.Remove(src)
}

// sanitizeFilename removes characters not safe for filesystem names.
func sanitizeFilename(s string) string {
	r := strings.NewReplacer(
		"/", " ", "\\", " ", ":", " ", "*", "", "?", "",
		"\"", "", "<", "", ">", "", "|", "",
	)
	return strings.TrimSpace(r.Replace(s))
}

// isSmartClassifyEnabled checks if smart classify is enabled.
// It first checks the database setting, then falls back to config.yaml.
func (o *OrganizerService) isSmartClassifyEnabled(ctx context.Context) bool {
	// Try database first
	if o.repo != nil && o.repo.Setting != nil {
		val, err := o.repo.Setting.Get(ctx, "organizer.smart_classify")
		if err == nil && val != "" {
			return val == "true" || val == "1" || val == "on"
		}
	}
	// Fallback to config.yaml
	return o.cfg.Organizer.SmartClassify
}

// SmartClassify determines the subcategory folder based on media metadata.
// It returns the category folder name (e.g., "华语电影", "欧美剧", "日番").
// Returns empty string if smart classify is disabled or metadata is insufficient.
func (o *OrganizerService) SmartClassify(ctx context.Context, m *model.Media) string {
	// Check if smart classify is enabled (from database first, then config)
	smartClassify := o.isSmartClassifyEnabled(ctx)
	if !smartClassify {
		return ""
	}

	// Fetch fresh metadata from DB (languages, countries, genres may have been updated by scraper)
	if m.Languages == "" && m.Countries == "" && m.Genres == "" {
		// Try to reload from DB
		fresh, err := o.repo.Media.FindByID(ctx, m.ID)
		if err == nil && fresh != nil {
			m = fresh
		}
	}

	// Parse metadata fields (comma-separated)
	languages := parseCommaList(m.Languages)
	countries := parseCommaList(m.Countries)
	genres := parseCommaList(m.Genres)

	// Determine media type from library
	lib, err := o.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil || lib == nil {
		return ""
	}

	categories := o.cfg.Organizer.Categories
	if categories == nil {
		categories = make(map[string]string)
	}

	// Helper closures
	isChinese := func() bool {
		for _, lang := range languages {
			if lang == "zh" || lang == "zh-CN" || lang == "zh-TW" {
				return true
			}
		}
		for _, c := range countries {
			if c == "CN" || c == "TW" || c == "HK" {
				return true
			}
		}
		return false
	}

	isEastAsian := func() bool {
		for _, c := range countries {
			if c == "JP" || c == "KR" {
				return true
			}
		}
		for _, lang := range languages {
			if lang == "ja" || lang == "ko" {
				return true
			}
		}
		return false
	}

	isWestern := func() bool {
		westernCountries := []string{"US", "GB", "FR", "DE", "CA", "AU", "NZ", "IE", "NL", "SE", "NO", "DK", "FI", "ES", "IT", "PT", "AT", "CH", "BE"}
		for _, c := range countries {
			for _, wc := range westernCountries {
				if c == wc {
					return true
				}
			}
		}
		return false
	}

	// Classification logic
	switch lib.Type {
	case "movie":
		// Use genres to help classify (e.g., Animation might be anime)
		isAnimation := false
		for _, g := range genres {
			if g == "Animation" {
				isAnimation = true
				break
			}
		}

		if isChinese() {
			if name, ok := categories["chinese_movie"]; ok && name != "" {
				return name
			}
			return "华语电影"
		}
		if isEastAsian() || (isAnimation && isEastAsian()) {
			if name, ok := categories["jk_movie"]; ok && name != "" {
				return name
			}
			return "日韩电影"
		}
		if isWestern() {
			if name, ok := categories["euus_movie"]; ok && name != "" {
				return name
			}
			return "欧美电影"
		}
		// Fallback: foreign movie
		if name, ok := categories["foreign_movie"]; ok && name != "" {
			return name
		}
		return "外语电影"

	case "tv", "anime":
		if lib.Type == "anime" || contains(genres, "Animation") {
			// Anime classification
			if isEastAsian() {
				// Check if it's Japanese
				for _, c := range countries {
					if c == "JP" {
						if name, ok := categories["jp_anime"]; ok && name != "" {
							return name
						}
						return "日番"
					}
				}
			}
			// Chinese anime
			if isChinese() {
				if name, ok := categories["cn_anime"]; ok && name != "" {
					return name
				}
				return "国漫"
			}
		}
		// TV classification
		if isChinese() {
			if name, ok := categories["domestic_tv"]; ok && name != "" {
				return name
			}
			return "国产剧"
		}
		if isEastAsian() {
			if name, ok := categories["jk_tv"]; ok && name != "" {
				return name
			}
			return "日韩剧"
		}
		if isWestern() {
			if name, ok := categories["euus_tv"]; ok && name != "" {
				return name
			}
			return "欧美剧"
		}
		return "剧集"
	}

	return ""
}

// parseCommaList splits a comma-separated string into a slice of trimmed strings.
func parseCommaList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// contains checks if a string slice contains a specific string.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
