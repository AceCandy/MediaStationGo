package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// MediaCategoryReclassifyOptions controls the metadata-based category audit.
// Empty LibraryIDs means all enabled local libraries.
type MediaCategoryReclassifyOptions struct {
	LibraryIDs []string
	MediaIDs   []string
	DryRun     bool
}

// ReclassifyMisclassifiedMedia corrects already-scanned local media whose
// stored metadata clearly disagrees with the current library/category.
func (o *OrganizerService) ReclassifyMisclassifiedMedia(ctx context.Context, opts MediaCategoryReclassifyOptions) (*OrganizeResult, error) {
	res := &OrganizeResult{DryRun: opts.DryRun}
	if o == nil || o.repo == nil || o.repo.DB == nil || o.repo.Library == nil {
		return res, nil
	}
	libraries, err := o.repo.Library.List(ctx)
	if err != nil {
		return res, err
	}
	filterIDs := compactLibraryIDs(opts.LibraryIDs...)
	mediaIDs := compactLibraryIDs(opts.MediaIDs...)
	filter := map[string]struct{}{}
	for _, id := range filterIDs {
		filter[id] = struct{}{}
	}
	libByID := make(map[string]model.Library, len(libraries))
	for _, lib := range libraries {
		if !lib.Enabled || strings.TrimSpace(lib.ID) == "" {
			continue
		}
		if len(filter) > 0 {
			if _, ok := filter[lib.ID]; !ok {
				continue
			}
		}
		libByID[lib.ID] = lib
	}
	if len(libByID) == 0 {
		return res, nil
	}

	query := o.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("deleted_at IS NULL")
	if len(filter) > 0 {
		query = query.Where("library_id IN ?", filterIDs)
	}
	if len(mediaIDs) > 0 {
		query = query.Where("id IN ?", mediaIDs)
	}
	var rows []model.Media
	err = query.FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
		for i := range rows {
			lib, ok := libByID[rows[i].LibraryID]
			if !ok {
				continue
			}
			changed, err := o.reclassifyScannedMedia(ctx, rows[i], lib, opts.DryRun, res)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %s", rows[i].Title, err.Error()))
				if o.log != nil {
					o.log.Warn("metadata category reclassify failed",
						zap.String("media", rows[i].ID),
						zap.String("path", rows[i].Path),
						zap.Error(err))
				}
				continue
			}
			if changed && o.log != nil {
				o.log.Debug("metadata category reclassify applied",
					zap.String("media", rows[i].ID),
					zap.String("title", rows[i].Title))
			}
		}
		return nil
	}).Error
	return res, err
}

func (o *OrganizerService) reclassifyScannedMedia(ctx context.Context, media model.Media, lib model.Library, dryRun bool, res *OrganizeResult) (bool, error) {
	if res == nil || !lib.Enabled || strings.TrimSpace(media.Path) == "" {
		return false, nil
	}
	if mount, ok := ParseCloudLibraryMount(lib.Path); ok {
		return o.reclassifyCloudScannedMedia(ctx, media, lib, mount, dryRun, res)
	}
	if !organizeFileExists(media.Path) {
		return false, nil
	}

	mediaType := normalizeOrganizeMediaType(lib.Type)
	metadataMatch := organizeMatchFromMedia(&media)
	if !mediaHasReliableCategoryMetadata(media) {
		metadataMatch = o.lookupReclassifyMetadata(ctx, media, lib, mediaType)
		if metadataMatch == nil {
			return false, nil
		}
		media = mediaWithReclassifyMatch(media, metadataMatch)
	}
	if matchType := normalizeOrganizeMediaType(metadataMatchMediaType(metadataMatch)); matchType != "" {
		mediaType = matchType
	}
	category := o.classifyMedia(ctx, &media, mediaType)
	if category == "" {
		return false, nil
	}
	if impliedType, normalizedCategory := o.mediaTypeForDirectoryCategory(category); impliedType != "" {
		mediaType = impliedType
		category = normalizedCategory
	}
	if mediaType == "" {
		mediaType = normalizeOrganizeMediaType(lib.Type)
	}
	if mediaType == "" {
		return false, nil
	}

	baseRoot := normalizeOrganizeDestinationRoot(o.resolveBaseRoot(ctx, &lib, ""))
	targetLibrary, matched := o.organizeLibraryForLayout(ctx, baseRoot, mediaType, category)
	if !matched || strings.TrimSpace(targetLibrary.ID) == "" || strings.TrimSpace(targetLibrary.Path) == "" {
		targetRoot := categoryRoot(o.organizeRoot(baseRoot, mediaType, category), category)
		if dryRun {
			targetLibrary = model.Library{Path: targetRoot}
		} else if created, ok := o.ensureOrganizeLibraryForRoot(ctx, targetRoot, mediaType, category); ok {
			targetLibrary = created
		} else {
			return false, nil
		}
	}
	if strings.EqualFold(targetLibrary.ID, lib.ID) && pathWithin(media.Path, targetLibrary.Path) {
		return false, nil
	}
	if pathWithin(media.Path, targetLibrary.Path) {
		return o.reclassifyScannedMediaLibraryOnly(ctx, media, lib, targetLibrary, category, mediaType, dryRun, res, metadataMatch)
	}

	title := sanitizeFilename(strings.TrimSpace(media.Title))
	if title == "" {
		title = "Unknown"
	}
	target, err := o.buildOrganizeTargetPath(ctx, organizeTargetInput{
		Root:      targetLibrary.Path,
		MediaType: mediaType,
		Category:  category,
		Title:     title,
		Source:    media.Path,
		Ext:       filepath.Ext(media.Path),
		Year:      media.Year,
		Season:    media.SeasonNum,
		Episode:   media.EpisodeNum,
		Series:    isSeriesLibraryType(mediaType),
	})
	if err != nil {
		return false, err
	}
	return o.reclassifyExistingMedia(ctx, organizeExistingReclassifyRequest{
		Source:          media.Path,
		Target:          target.Path,
		DestRoot:        firstNonEmpty(baseRoot, lib.Path),
		TargetLibraryID: targetLibrary.ID,
		Existing:        []string{media.Path},
		DryRun:          dryRun,
		MediaType:       mediaType,
		Category:        category,
		Title:           title,
		Year:            media.Year,
		Season:          media.SeasonNum,
		Episode:         media.EpisodeNum,
		MetadataMatch:   metadataMatch,
		Result:          res,
	})
}

func (o *OrganizerService) reclassifyCloudScannedMedia(ctx context.Context, media model.Media, lib model.Library, mount CloudMountInfo, dryRun bool, res *OrganizeResult) (bool, error) {
	mediaType := normalizeOrganizeMediaType(lib.Type)
	metadataMatch := organizeMatchFromMedia(&media)
	if !mediaHasReliableCategoryMetadata(media) {
		metadataMatch = o.lookupReclassifyMetadata(ctx, media, lib, mediaType)
		if metadataMatch == nil {
			return false, nil
		}
		media = mediaWithReclassifyMatch(media, metadataMatch)
	}
	if matchType := normalizeOrganizeMediaType(metadataMatchMediaType(metadataMatch)); matchType != "" {
		mediaType = matchType
	}
	category := o.classifyMedia(ctx, &media, mediaType)
	if category == "" {
		return false, nil
	}
	if impliedType, normalizedCategory := o.mediaTypeForDirectoryCategory(category); impliedType != "" {
		mediaType = impliedType
		category = normalizedCategory
	}
	if mediaType == "" {
		mediaType = normalizeOrganizeMediaType(lib.Type)
	}
	displayDir := o.cloudReclassifyCategoryDisplayDir(mediaType, category)
	if displayDir == "" {
		return false, nil
	}
	if normalizeCloudMountDir(mount.Provider, mount.DisplayDir) == normalizeCloudMountDir(mount.Provider, displayDir) {
		return false, nil
	}
	targetLibrary, ok, err := o.ensureCloudReclassifyLibrary(ctx, mount.Provider, displayDir, mediaType, dryRun)
	if err != nil || !ok {
		return false, err
	}
	if strings.TrimSpace(targetLibrary.ID) != "" && targetLibrary.ID == lib.ID {
		return false, nil
	}
	title := sanitizeFilename(strings.TrimSpace(media.Title))
	if title == "" {
		title = "Unknown"
	}
	res.Items = append(res.Items, OrganizePreviewItem{
		Source:    media.Path,
		Target:    targetLibrary.Path,
		Action:    "reclassify",
		Reason:    "cloud metadata category library changed",
		MediaType: mediaType,
		Category:  category,
		Title:     title,
	})
	if dryRun {
		res.Reclassified++
		return true, nil
	}
	updates := map[string]any{
		"library_id": targetLibrary.ID,
		"series_id":  "",
	}
	applyReclassifyMatchUpdates(updates, metadataMatch)
	if err := o.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", media.ID).Updates(updates).Error; err != nil {
		return false, err
	}
	if o.log != nil {
		o.log.Info("cloud media library reclassified by metadata",
			zap.String("media", media.ID),
			zap.String("path", media.Path),
			zap.String("from_library", lib.ID),
			zap.String("to_library", targetLibrary.ID),
			zap.String("category", category),
			zap.String("media_type", mediaType),
			zap.String("display_dir", displayDir))
	}
	res.Reclassified++
	return true, nil
}

func (o *OrganizerService) cloudReclassifyCategoryDisplayDir(mediaType, category string) string {
	category = sanitizeFilename(strings.TrimSpace(category))
	if category == "" {
		return ""
	}
	root := o.mediaTypeRootDirForCategory(mediaType, category)
	if root == "" {
		return ""
	}
	return strings.Join([]string{root, category}, "/")
}

func (o *OrganizerService) ensureCloudReclassifyLibrary(ctx context.Context, provider, displayDir, mediaType string, dryRun bool) (model.Library, bool, error) {
	if o == nil || o.repo == nil || o.repo.Library == nil {
		return model.Library{}, false, nil
	}
	provider = strings.TrimSpace(provider)
	displayDir = normalizeCloudMountDir(provider, displayDir)
	if provider == "" || displayDir == "" {
		return model.Library{}, false, nil
	}
	if existing := o.findCloudReclassifyLibrary(ctx, provider, displayDir); existing != nil {
		return *existing, true, nil
	}
	path := BuildCloudAutoCategoryLibraryPath(provider, displayDir)
	if path == "" {
		return model.Library{}, false, nil
	}
	name := cloudMountDirBase(displayDir)
	if name == "" {
		name = displayDir
	}
	libType := InferCloudMountMediaType(displayDir, name)
	if normalizeOrganizeMediaType(libType) == "" {
		libType = organizeLibraryModelType(mediaType)
	}
	lib := model.Library{
		Name:    name,
		Path:    path,
		Type:    libType,
		Enabled: true,
	}
	if dryRun {
		return lib, true, nil
	}
	if err := o.repo.Library.Create(ctx, &lib); err != nil {
		if existing := o.findCloudReclassifyLibrary(ctx, provider, displayDir); existing != nil {
			return *existing, true, nil
		}
		return model.Library{}, false, err
	}
	if o.log != nil {
		o.log.Info("created cloud metadata reclassify library",
			zap.String("library_id", lib.ID),
			zap.String("provider", provider),
			zap.String("display_dir", displayDir),
			zap.String("type", lib.Type))
	}
	return lib, true, nil
}

func (o *OrganizerService) findCloudReclassifyLibrary(ctx context.Context, provider, displayDir string) *model.Library {
	if o == nil || o.repo == nil || o.repo.Library == nil {
		return nil
	}
	libs, err := o.repo.Library.List(ctx)
	if err != nil {
		if o.log != nil {
			o.log.Warn("list cloud libraries for metadata reclassify failed", zap.Error(err))
		}
		return nil
	}
	displayDir = normalizeCloudMountDir(provider, displayDir)
	for _, lib := range libs {
		if !lib.Enabled {
			continue
		}
		info, ok := ParseCloudLibraryMount(lib.Path)
		if !ok || info.Provider != provider {
			continue
		}
		if normalizeCloudMountDir(provider, info.DisplayDir) == displayDir {
			return &lib
		}
	}
	return nil
}

func (o *OrganizerService) reclassifyScannedMediaLibraryOnly(ctx context.Context, media model.Media, oldLib, targetLib model.Library, category, mediaType string, dryRun bool, res *OrganizeResult, metadataMatch *Match) (bool, error) {
	res.Items = append(res.Items, OrganizePreviewItem{
		Source:    media.Path,
		Target:    media.Path,
		Action:    "reclassify",
		Reason:    "metadata category library changed",
		MediaType: mediaType,
		Category:  category,
		Title:     media.Title,
	})
	if dryRun {
		res.Reclassified++
		return true, nil
	}
	updates := map[string]any{"library_id": targetLib.ID, "series_id": ""}
	applyReclassifyMatchUpdates(updates, metadataMatch)
	if err := o.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Where("id = ?", media.ID).
		Updates(updates).Error; err != nil {
		return false, err
	}
	if o.log != nil {
		o.log.Info("media library reclassified by metadata",
			zap.String("media", media.ID),
			zap.String("path", media.Path),
			zap.String("from_library", oldLib.ID),
			zap.String("to_library", targetLib.ID),
			zap.String("category", category),
			zap.String("media_type", mediaType))
	}
	res.Reclassified++
	return true, nil
}

func (o *OrganizerService) lookupReclassifyMetadata(ctx context.Context, media model.Media, lib model.Library, mediaType string) *Match {
	if o == nil || o.scraper == nil || !o.scraper.AnyEnabled() {
		return nil
	}
	title := strings.TrimSpace(media.Title)
	if title == "" {
		title, _ = CleanQuery(media.Path)
	}
	for _, typ := range reclassifyMetadataLookupTypes(mediaType, media) {
		if match := o.lookupOrganizeMetadata(ctx, media.Path, lib.Path, typ, title, media.Year, media.SeasonNum, media.EpisodeNum, nil); match != nil {
			if o.log != nil {
				o.log.Info("metadata category reclassify filled missing metadata",
					zap.String("media", media.ID),
					zap.String("path", media.Path),
					zap.String("title", match.Title),
					zap.String("media_type", typ),
					zap.Int("tmdb_id", match.TMDbID),
					zap.Int("bangumi_id", match.BangumiID),
					zap.String("douban_id", match.DoubanID),
					zap.String("thetvdb_id", match.TheTVDBID))
			}
			return match
		}
	}
	return nil
}

func reclassifyMetadataLookupTypes(mediaType string, media model.Media) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	add := func(value string) {
		value = normalizeOrganizeMediaType(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(mediaType)
	if media.SeasonNum > 0 || media.EpisodeNum > 0 {
		add("tv")
		add("anime")
	}
	switch normalizeOrganizeMediaType(mediaType) {
	case "tv":
		add("anime")
		add("movie")
	case "anime":
		add("tv")
		add("movie")
	case "movie", "":
		add("tv")
		add("anime")
		add("movie")
	default:
		add("tv")
		add("movie")
	}
	return out
}

func mediaWithReclassifyMatch(media model.Media, match *Match) model.Media {
	if match == nil {
		return media
	}
	if value := strings.TrimSpace(match.Title); value != "" {
		media.Title = value
	}
	if value := strings.TrimSpace(match.OriginalName); value != "" {
		media.OriginalName = value
	}
	if match.Year > 0 {
		media.Year = match.Year
	}
	if match.TMDbID > 0 {
		media.TMDbID = match.TMDbID
	}
	if match.BangumiID > 0 {
		media.BangumiID = match.BangumiID
	}
	if value := strings.TrimSpace(match.DoubanID); value != "" {
		media.DoubanID = value
	}
	if value := strings.TrimSpace(match.TheTVDBID); value != "" {
		media.TheTVDBID = value
	}
	if len(match.Languages) > 0 {
		media.Languages = strings.Join(match.Languages, ",")
	}
	if len(match.Countries) > 0 {
		media.Countries = strings.Join(match.Countries, ",")
	}
	if len(match.Genres) > 0 {
		media.Genres = strings.Join(match.Genres, ",")
	}
	if match.NSFW {
		media.NSFW = true
	}
	media.ScrapeStatus = "matched"
	return media
}

func metadataMatchMediaType(match *Match) string {
	if match == nil {
		return ""
	}
	return match.MediaType
}

func mediaHasReliableCategoryMetadata(media model.Media) bool {
	return media.NSFW ||
		strings.TrimSpace(media.Languages) != "" ||
		strings.TrimSpace(media.Countries) != "" ||
		strings.TrimSpace(media.Genres) != ""
}
