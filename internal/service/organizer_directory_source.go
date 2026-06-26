package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type organizeDirectoryLayout struct {
	MediaType string
	Category  string
}

type organizeSourceFileRequest struct {
	Source                string
	SourceRoot            string
	DestRoot              string
	Mode                  TransferMode
	MediaTypeOverride     string
	MediaCategoryOverride string
	DryRun                bool
	AllowReplaceExisting  bool
	MetadataCache         map[string]*Match
	Result                *OrganizeResult
}

type organizeSourceIdentity struct {
	Ext         string
	Title       string
	ParsedTitle string
	Year        int
	Season      int
	Episode     int
	SourceMedia *model.Media
}

type organizeSourceFilePlan struct {
	Identity        organizeSourceIdentity
	Layout          organizeDirectoryLayout
	LayoutRoot      string
	TargetLibraryID string
	MetadataMatch   *Match
	Target          organizeTargetPath
}

type organizeExistingSourceVersions struct {
	External []string
	Identity []string
	Folder   []string
	All      []string
}

// organizeSourceFile organizes a single video file from the source directory
// into destRoot, applying dedup + 洗版.
func (o *OrganizerService) organizeSourceFile(ctx context.Context, req organizeSourceFileRequest) error {
	plan, err := o.buildOrganizeSourceFilePlan(ctx, req)
	if err != nil {
		return err
	}
	if filepath.Clean(req.Source) == filepath.Clean(plan.Target.Path) {
		skipAlreadyOrganizedSource(req, plan)
		return nil
	}
	existing := o.collectExistingSourceVersions(ctx, req, plan)
	if len(existing.All) > 0 {
		return o.handleExistingSourceVersions(ctx, req, plan, existing)
	}
	return o.writeOrganizedSourceFile(ctx, req, plan)
}

func (o *OrganizerService) buildOrganizeSourceFilePlan(ctx context.Context, req organizeSourceFileRequest) (organizeSourceFilePlan, error) {
	identity := o.resolveOrganizeSourceIdentity(ctx, req)
	pathLayout := o.inferOrganizeDirectoryLayout(req.Source, req.SourceRoot)
	layout := pathLayout
	forcedType := normalizeOrganizeMediaType(req.MediaTypeOverride)
	inferredType := o.inferMediaTypeForSourceFile(req.Source, identity.Title, identity.Season, identity.Episode)
	layout = applyOrganizeSourceMediaType(layout, forcedType, inferredType)
	lookupType := organizeSourceMetadataLookupType(req.Source, pathLayout, layout, forcedType, inferredType, identity)
	metadataMatch := o.lookupOrganizeSourceMetadata(ctx, req, lookupType, &identity)
	if metadataMatch == nil && lookupType != layout.MediaType {
		metadataMatch = o.lookupOrganizeSourceMetadata(ctx, req, layout.MediaType, &identity)
	}
	if forcedType == "" && metadataMatch != nil {
		if matchType := normalizeOrganizeMediaType(metadataMatch.MediaType); matchType != "" && matchType != layout.MediaType {
			if categoryType, _ := o.mediaTypeForDirectoryCategory(layout.Category); categoryType != "" && !sourceCategoryCompatible(matchType, categoryType) {
				layout.Category = ""
			}
			if o.log != nil {
				o.log.Info("organize media type corrected by metadata",
					zap.String("source", req.Source),
					zap.String("from", layout.MediaType),
					zap.String("to", matchType),
					zap.String("title", metadataMatch.Title),
					zap.Int("tmdb_id", metadataMatch.TMDbID))
			}
			layout.MediaType = matchType
		}
	}
	layout = o.applyOrganizeSourceCategory(ctx, req, pathLayout, layout, forcedType, identity, metadataMatch)
	layoutRoot, targetLibraryID := o.resolveOrganizeSourceLayoutRoot(ctx, req, layout)
	target, err := o.buildOrganizeSourceTarget(ctx, req, layout, layoutRoot, identity)
	if err != nil {
		return organizeSourceFilePlan{}, err
	}
	return organizeSourceFilePlan{
		Identity:        identity,
		Layout:          layout,
		LayoutRoot:      layoutRoot,
		TargetLibraryID: targetLibraryID,
		MetadataMatch:   metadataMatch,
		Target:          target,
	}, nil
}

func (o *OrganizerService) resolveOrganizeSourceIdentity(ctx context.Context, req organizeSourceFileRequest) organizeSourceIdentity {
	src := req.Source
	ext := filepath.Ext(src)
	season, episode := ParseEpisode(src)
	title, year := CleanQuery(src)
	if organizeWeakFileTitle(title) {
		if folderTitle, folderYear := organizeTitleFromParentFolder(src, req.SourceRoot, season > 0 || episode > 0); folderTitle != "" {
			title = folderTitle
			if year <= 0 {
				year = folderYear
			}
		} else {
			title = strings.TrimSuffix(filepath.Base(src), ext)
		}
	}
	parsedTitle := title
	sourceMedia := o.lookupOrganizeSourceMedia(ctx, src)
	title = sanitizeFilename(titleCaseWords(title))
	if sourceMedia != nil {
		if mediaTitle := sanitizeFilename(strings.TrimSpace(sourceMedia.Title)); mediaTitle != "" {
			title = mediaTitle
			parsedTitle = strings.TrimSpace(sourceMedia.Title)
		}
		if sourceMedia.Year > 0 {
			year = sourceMedia.Year
		}
		if sourceMedia.SeasonNum > 0 {
			season = sourceMedia.SeasonNum
		}
		if sourceMedia.EpisodeNum > 0 {
			episode = sourceMedia.EpisodeNum
		}
	}
	if title == "" {
		title = "Unknown"
	}
	return organizeSourceIdentity{
		Ext:         ext,
		Title:       title,
		ParsedTitle: parsedTitle,
		Year:        year,
		Season:      season,
		Episode:     episode,
		SourceMedia: sourceMedia,
	}
}

func applyOrganizeSourceMediaType(layout organizeDirectoryLayout, forcedType, inferredType string) organizeDirectoryLayout {
	if forcedType != "" {
		if layout.Category != "" && layout.MediaType != "" && layout.MediaType != forcedType {
			layout.Category = ""
		}
		layout.MediaType = forcedType
		return layout
	}
	if inferredType == "" {
		return layout
	}
	if inferredType == "tv" && layout.MediaType == "movie" {
		return organizeDirectoryLayout{MediaType: inferredType}
	}
	if layout.MediaType == "" {
		layout.MediaType = inferredType
	}
	return layout
}

func organizeSourceMetadataLookupType(src string, pathLayout, layout organizeDirectoryLayout, forcedType, inferredType string, identity organizeSourceIdentity) string {
	if forcedType != "" {
		return layout.MediaType
	}
	if pathLayout.MediaType != "" && pathLayout.MediaType != "movie" && organizeStandaloneMovieSourceHint(src, identity) {
		return "movie"
	}
	return layout.MediaType
}

func organizeStandaloneMovieSourceHint(src string, identity organizeSourceIdentity) bool {
	if identity.Season > 0 || identity.Episode > 0 {
		return organizeEpisodeLooksSourcedFromMovieYear(src, identity)
	}
	_, year := CleanQuery(filepath.Base(src))
	if year <= 0 {
		year = identity.Year
	}
	if year <= 0 {
		return false
	}
	text := strings.ToLower(strings.Join([]string{filepath.Base(src), identity.Title, identity.ParsedTitle}, " "))
	return !classifierEpisodeRE.MatchString(text) && !classifierSeasonRE.MatchString(text)
}

func organizeEpisodeLooksSourcedFromMovieYear(src string, identity organizeSourceIdentity) bool {
	year := identity.Year
	if identity.SourceMedia != nil && identity.SourceMedia.Year > 0 {
		year = identity.SourceMedia.Year
	}
	if year < 1900 || year > 2099 || identity.Season != 1 || identity.Episode != year/10 {
		return false
	}
	showDir := showDirFromEpisodePath(src)
	if showDir == "" {
		return false
	}
	_, folderYear := CleanQuery(filepath.Base(showDir))
	return folderYear == year
}

func (o *OrganizerService) lookupOrganizeSourceMetadata(ctx context.Context, req organizeSourceFileRequest, mediaType string, identity *organizeSourceIdentity) *Match {
	match := o.lookupOrganizeMetadata(ctx, req.Source, req.SourceRoot, mediaType, identity.Title, identity.Year, identity.Season, identity.Episode, req.MetadataCache)
	if match == nil && identity.SourceMedia != nil {
		match = organizeMatchFromMedia(identity.SourceMedia)
	}
	if match != nil {
		applyOrganizeMetadataMatch(match, &identity.Title, &identity.ParsedTitle, &identity.Year)
	}
	return match
}

func (o *OrganizerService) applyOrganizeSourceCategory(
	ctx context.Context,
	req organizeSourceFileRequest,
	pathLayout, layout organizeDirectoryLayout,
	forcedType string,
	identity organizeSourceIdentity,
	metadataMatch *Match,
) organizeDirectoryLayout {
	if category := strings.TrimSpace(req.MediaCategoryOverride); category != "" {
		layout.Category = sanitizeFilename(category)
	} else if category := o.smartClassifySourceFile(ctx, req.Source, req.SourceRoot, layout.MediaType, identity.Title, identity.ParsedTitle, metadataMatch); category != "" {
		layout.Category = category
	}
	if forcedType == "" {
		if impliedType, normalizedCategory := o.mediaTypeForDirectoryCategory(layout.Category); impliedType != "" {
			layout.Category = normalizedCategory
			if layout.MediaType == "" || layout.MediaType == "tv" || layout.MediaType == "anime" || pathLayout.Category != layout.Category {
				layout.MediaType = impliedType
			}
		}
	}
	return layout
}

func (o *OrganizerService) resolveOrganizeSourceLayoutRoot(ctx context.Context, req organizeSourceFileRequest, layout organizeDirectoryLayout) (string, string) {
	targetLibrary, matchedLibrary := o.organizeLibraryForLayout(ctx, req.DestRoot, layout.MediaType, layout.Category)
	layoutRoot := targetLibrary.Path
	targetLibraryID := targetLibrary.ID
	if !matchedLibrary && layout.MediaType != "" {
		layoutRoot = o.organizeRoot(req.DestRoot, layout.MediaType, layout.Category)
	}
	if !matchedLibrary && layout.Category != "" {
		layoutRoot = categoryRoot(layoutRoot, sanitizeFilename(layout.Category))
	}
	if !matchedLibrary && !req.DryRun {
		if targetLibrary, ok := o.ensureOrganizeLibraryForRoot(ctx, layoutRoot, layout.MediaType, layout.Category); ok {
			targetLibraryID = targetLibrary.ID
		}
	}
	return layoutRoot, targetLibraryID
}

func (o *OrganizerService) buildOrganizeSourceTarget(
	ctx context.Context,
	req organizeSourceFileRequest,
	layout organizeDirectoryLayout,
	layoutRoot string,
	identity organizeSourceIdentity,
) (organizeTargetPath, error) {
	isSeries := identity.Season > 0 || identity.Episode > 0
	if layout.MediaType != "" {
		isSeries = isSeriesLibraryType(layout.MediaType) && isSeries
	}
	return o.buildOrganizeTargetPath(ctx, organizeTargetInput{
		Root:      layoutRoot,
		MediaType: layout.MediaType,
		Category:  layout.Category,
		Title:     identity.Title,
		Source:    req.Source,
		Ext:       identity.Ext,
		Year:      identity.Year,
		Season:    identity.Season,
		Episode:   identity.Episode,
		Series:    isSeries,
	})
}

func skipAlreadyOrganizedSource(req organizeSourceFileRequest, plan organizeSourceFilePlan) {
	req.Result.Skipped++
	req.Result.Items = append(req.Result.Items, OrganizePreviewItem{
		Source: req.Source, Target: plan.Target.Path, Action: "skip", Reason: organizeSkipAlreadyOrganized,
		MediaType: plan.Layout.MediaType, Category: plan.Layout.Category, Title: plan.Identity.Title,
	})
}

func (o *OrganizerService) collectExistingSourceVersions(ctx context.Context, req organizeSourceFileRequest, plan organizeSourceFilePlan) organizeExistingSourceVersions {
	externalExisting := o.existingByExternalIdentity(ctx, req.DestRoot, plan.MetadataMatch, plan.Identity.Season, plan.Identity.Episode)
	identityExisting := o.existingByIdentity(ctx, req.DestRoot, plan.Identity.ParsedTitle, plan.Identity.Year, plan.Identity.Season, plan.Identity.Episode)
	folderExisting := o.existingByFolder(plan.Target.Dir, plan.Target.EpisodeTag)
	return organizeExistingSourceVersions{
		External: externalExisting,
		Identity: identityExisting,
		Folder:   folderExisting,
		All:      mergeExistingVersionPaths(externalExisting, identityExisting, folderExisting),
	}
}

func (o *OrganizerService) handleExistingSourceVersions(ctx context.Context, req organizeSourceFileRequest, plan organizeSourceFilePlan, existing organizeExistingSourceVersions) error {
	reclassified, err := o.reclassifyExistingMedia(ctx, organizeExistingReclassifyRequest{
		Source:          req.Source,
		Target:          plan.Target.Path,
		DestRoot:        req.DestRoot,
		TargetLibraryID: plan.TargetLibraryID,
		Existing:        existing.All,
		DryRun:          req.DryRun,
		MediaType:       plan.Layout.MediaType,
		Category:        plan.Layout.Category,
		Title:           plan.Identity.Title,
		Year:            plan.Identity.Year,
		Season:          plan.Identity.Season,
		Episode:         plan.Identity.Episode,
		MetadataMatch:   plan.MetadataMatch,
		Result:          req.Result,
	})
	if err != nil || reclassified {
		return err
	}
	if replaced, err := o.replaceSourceWithBetterVersion(ctx, req, plan, existing.All); replaced || err != nil {
		return err
	}
	o.skipExistingSourceDuplicate(ctx, req, plan, existing)
	return nil
}

func (o *OrganizerService) replaceSourceWithBetterVersion(ctx context.Context, req organizeSourceFileRequest, plan organizeSourceFilePlan, existing []string) (bool, error) {
	srcArea := o.resolutionArea(ctx, req.Source)
	bestArea := 0
	for _, e := range existing {
		if a := o.resolutionArea(ctx, e); a > bestArea {
			bestArea = a
		}
	}
	if !req.AllowReplaceExisting || srcArea <= 0 || bestArea <= 0 || srcArea <= bestArea {
		return false, nil
	}
	req.Result.Items = append(req.Result.Items, OrganizePreviewItem{
		Source: req.Source, Target: plan.Target.Path, Action: "replace", Reason: "higher resolution",
		MediaType: plan.Layout.MediaType, Category: plan.Layout.Category, Title: plan.Identity.Title,
	})
	if req.DryRun {
		req.Result.Replaced++
		return true, nil
	}
	if err := o.replaceVersions(ctx, req.Source, existing, plan.Target.Path, req.Mode); err != nil {
		return true, err
	}
	o.log.Info("organize replaced lower-resolution media",
		zap.String("from", req.Source),
		zap.String("to", plan.Target.Path),
		zap.Int("src_area", srcArea),
		zap.Int("existing_area", bestArea),
	)
	req.Result.Replaced++
	return true, nil
}

func (o *OrganizerService) skipExistingSourceDuplicate(ctx context.Context, req organizeSourceFileRequest, plan organizeSourceFilePlan, existing organizeExistingSourceVersions) {
	reason := organizeSkipTargetExists
	if len(existing.External) > 0 || len(existing.Identity) > 0 || o.allExistingPathsInDB(ctx, existing.All) {
		reason = organizeSkipDuplicateLibrary
	}
	o.log.Debug("organize skip duplicate",
		zap.String("src", req.Source), zap.String("dest_dir", plan.Target.Dir), zap.String("reason", reason))
	req.Result.Skipped++
	req.Result.Items = append(req.Result.Items, OrganizePreviewItem{
		Source: req.Source, Target: plan.Target.Path, Action: "skip", Reason: reason,
		MediaType: plan.Layout.MediaType, Category: plan.Layout.Category, Title: plan.Identity.Title,
	})
}

func (o *OrganizerService) writeOrganizedSourceFile(ctx context.Context, req organizeSourceFileRequest, plan organizeSourceFilePlan) error {
	req.Result.Items = append(req.Result.Items, OrganizePreviewItem{
		Source: req.Source, Target: plan.Target.Path, Action: "organize",
		MediaType: plan.Layout.MediaType, Category: plan.Layout.Category, Title: plan.Identity.Title,
	})
	if req.DryRun {
		req.Result.Organized++
		return nil
	}
	if err := os.MkdirAll(plan.Target.Dir, 0o755); err != nil { // #nosec G301 -- organized media directories must remain readable by NAS/player users.
		return err
	}
	if _, err := os.Stat(plan.Target.Path); err == nil {
		req.Result.Skipped++
		if len(req.Result.Items) > 0 {
			req.Result.Items[len(req.Result.Items)-1].Action = "skip"
			req.Result.Items[len(req.Result.Items)-1].Reason = organizeSkipTargetExists
		}
		return nil
	}
	if err := transferFile(req.Source, plan.Target.Path, req.Mode); err != nil {
		return err
	}
	if err := transferSidecarNFO(req.Source, plan.Target.Path, req.Mode); err != nil {
		o.log.Warn("organize sidecar nfo failed",
			zap.String("from", req.Source), zap.String("to", plan.Target.Path), zap.Error(err))
	}
	req.Result.Organized++
	return nil
}

func organizeTitleFromParentFolder(src, sourceRoot string, seriesLike bool) (string, int) {
	if !seriesLike {
		return "", 0
	}
	raw := seriesFolderTitle(src, sourceRoot)
	if strings.TrimSpace(raw) == "" {
		return "", 0
	}
	title, year := CleanQuery(raw)
	if title == "" {
		title = strings.TrimSpace(raw)
	}
	return title, year
}

func shouldSkipOrganizeSourceVideo(path, sourceRoot string) (bool, string) {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(sourceRoot)
	if rel, err := filepath.Rel(cleanRoot, cleanPath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		dir := filepath.Dir(rel)
		if dir != "." {
			for _, part := range strings.Split(dir, string(os.PathSeparator)) {
				switch normalizeOrganizeCategoryKey(part) {
				case "sample", "samples", "trailer", "trailers", "preview", "previews", "teaser", "teasers":
					return true, organizeSkipSampleClip
				}
			}
		}
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(cleanPath), filepath.Ext(cleanPath)))
	normalized := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(base)
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return false, ""
	}
	if len(fields) == 1 && strings.HasPrefix(fields[0], "sample") {
		return true, organizeSkipSampleClip
	}
	last := fields[len(fields)-1]
	switch last {
	case "sample", "trailer", "preview", "teaser":
		return true, organizeSkipSampleClip
	}
	return false, ""
}

func normalizeOrganizeMediaType(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "movie", "film":
		return "movie"
	case "tv", "series", "show", "drama":
		return "tv"
	case "anime", "animation":
		return "anime"
	case "variety":
		return "variety"
	case "adult", "nsfw":
		return "adult"
	default:
		return ""
	}
}

func organizeWeakFileTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return true
	}
	fields := strings.Fields(strings.ToLower(title))
	if len(fields) == 0 {
		return true
	}
	meaningful := 0
	for _, field := range fields {
		if _, ok := noiseTokenSet[field]; ok {
			continue
		}
		if _, ok := releaseBoundaryTokenSet[field]; ok {
			continue
		}
		if len(field) == 4 && strings.HasPrefix(field, "20") {
			continue
		}
		meaningful++
	}
	return meaningful == 0
}

func (o *OrganizerService) inferMediaTypeForSourceFile(src, title string, season, episode int) string {
	if season > 0 || episode > 0 {
		return "tv"
	}
	return normalizeMediaType("", title, src)
}
