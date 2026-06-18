// Package service — media file organizer (auto-rename + move).
//
// OrganizerService takes a source file (typically from a download
// completion) and moves/renames it into the configured library directory
// using a Jinja2-like template. The default templates:
//
//	movie: {Title} ({Year})/{Title} ({Year}).{Ext}
//	tv:    {Title}/Season {Season:02d}/{Title} - S{Season:02d}E{Episode:02d}.{Ext}
//
// Organizing moves the source file. On the same filesystem this is an
// os.Rename; across filesystems it falls back to streaming copy + source
// removal, so operators should keep downloads and media on the same volume
// when they want instant moves and no temporary duplicate disk usage.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	cfg     *config.Config
	log     *zap.Logger
	repo    *repository.Container
	probe   *FFprobeService // optional; used for 洗版 resolution comparison
	scraper *ScraperService // optional; used to identify metadata before rename
}

// NewOrganizerService is the constructor.
func NewOrganizerService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *OrganizerService {
	return &OrganizerService{cfg: cfg, log: log, repo: repo}
}

// SetProbe wires an FFprobe service so directory organize can compare real
// pixel dimensions when deciding whether to 洗版 (replace by higher resolution).
// Optional: when nil the organizer falls back to filename resolution tokens.
func (o *OrganizerService) SetProbe(p *FFprobeService) { o.probe = p }

// SetScraper wires the scraper so directory organize can resolve TMDb/Bangumi
// metadata before it decides the final folder and filename.
func (o *OrganizerService) SetScraper(s *ScraperService) { o.scraper = s }

// OrganizeResult reports what happened.
type OrganizeResult struct {
	Organized  int                     `json:"organized"`
	Skipped    int                     `json:"skipped"`
	Replaced   int                     `json:"replaced,omitempty"`
	Errors     []string                `json:"errors,omitempty"`
	SourcePath string                  `json:"source_path,omitempty"`
	DestPath   string                  `json:"dest_path,omitempty"`
	DryRun     bool                    `json:"dry_run,omitempty"`
	Items      []OrganizePreviewItem   `json:"items,omitempty"`
	Scans      []OrganizeScanSummary   `json:"scans,omitempty"`
	Scrapes    []OrganizeScrapeSummary `json:"scrapes,omitempty"`
}

type OrganizePreviewItem struct {
	Source    string `json:"source"`
	Target    string `json:"target,omitempty"`
	Action    string `json:"action"` // organize / skip / replace / error
	Reason    string `json:"reason,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Category  string `json:"category,omitempty"`
	Title     string `json:"title,omitempty"`
}

// OrganizeOptions carries per-request overrides for an organize operation.
// 空值表示沿用系统设置中的默认值。
//
// 整理是「从源目录整理到目的地目录」：SourcePath 指定待整理文件所在的源目录，
// DestPath 指定整理输出的目的地目录。两者相互独立，不再混用同一个目录。
type OrganizeOptions struct {
	// SourcePath 本次整理的源目录（待整理文件所在目录），覆盖 organize.source_dir
	// 设置与媒体库路径。仅整理位于该目录下的媒体；留空表示整个媒体库。
	SourcePath string
	// DestPath 本次整理的目的地根路径（整理输出到哪里），覆盖 organize.target_dir 设置。
	// 留空则使用设置中的默认目的地目录，再退回媒体库路径。
	DestPath string
	// TransferMode 本次整理的转移方式，覆盖 organize.transfer_mode 设置。
	TransferMode TransferMode
	// MediaType 手动整理时由 UI 指定的媒体类型。空值时按文件名/目录推断。
	MediaType string
	// MediaCategory 由订阅/下载任务或 UI 指定的分类。空值时按目录/NFO/规则推断。
	MediaCategory string
	// DryRun 仅生成整理预览，不实际移动/复制/硬链接文件。
	DryRun bool
	// AllowReplaceExisting 允许用本次来源替换目标库中已存在的同一媒体。
	// 默认 false：只去重不洗版，避免未开启洗版的订阅/手动整理留下或替换出多份版本。
	AllowReplaceExisting bool
}

// OrganizeMedia moves a single media file into the target library directory.
// It auto-detects whether the media is a movie or TV episode based on the
// parsed season/episode numbers and builds the destination path accordingly.
// When smart classify is enabled, it adds a category subfolder (e.g., "华语电影").
func (o *OrganizerService) OrganizeMedia(ctx context.Context, mediaID string) (string, error) {
	return o.OrganizeMediaWithOptions(ctx, mediaID, OrganizeOptions{})
}

// OrganizeMediaWithOptions is OrganizeMedia with per-request overrides for the
// target path and transfer mode.
func (o *OrganizerService) OrganizeMediaWithOptions(ctx context.Context, mediaID string, opts OrganizeOptions) (string, error) {
	m, err := o.repo.Media.FindByID(ctx, mediaID)
	if err != nil || m == nil {
		return "", errors.New("media not found")
	}
	lib, err := o.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil || lib == nil {
		return "", errors.New("library not found")
	}
	if _, ok := ParseCloudLibraryMount(lib.Path); ok {
		return "", errors.New("local organize cannot use cloud libraries directly; use external storage scan/mount for cloud media or enable cloud transfer to write to cloud")
	}
	baseRoot := redirectOrganizeStagingRoot(o.resolveBaseRoot(ctx, lib, opts.DestPath))
	if _, ok := ParseCloudLibraryMount(baseRoot); ok {
		return "", errors.New("organize destination must be a local writable media directory; enable cloud transfer in external storage when writing to cloud")
	}
	if !opts.DryRun {
		if err := ensureOrganizeDestinationWritable(baseRoot); err != nil {
			return "", err
		}
	}
	mode := o.resolveTransferMode(ctx, opts.TransferMode)
	if isSeriesLibraryType(lib.Type) {
		if err := o.refreshEpisodeIdentity(m, lib); err != nil {
			return "", err
		}
	}
	ext := filepath.Ext(m.Path)
	title := sanitizeFilename(m.Title)
	if title == "" {
		title = "Unknown"
	}

	// Determine category folder (if smart classify is enabled)
	category := o.SmartClassify(ctx, m)

	var dst string
	if isSeriesLibraryType(lib.Type) {
		root := o.organizeRoot(baseRoot, lib.Type, category)
		target, err := o.buildOrganizeTargetPath(ctx, organizeTargetInput{
			Root:      categoryRoot(root, category),
			MediaType: lib.Type,
			Category:  category,
			Title:     title,
			Source:    m.Path,
			Ext:       ext,
			Year:      m.Year,
			Season:    m.SeasonNum,
			Episode:   m.EpisodeNum,
			Series:    true,
		})
		if err != nil {
			return "", err
		}
		dst = target.Path
	} else {
		root := o.organizeRoot(baseRoot, lib.Type, category)
		target, err := o.buildOrganizeTargetPath(ctx, organizeTargetInput{
			Root:      categoryRoot(root, category),
			MediaType: lib.Type,
			Category:  category,
			Title:     title,
			Source:    m.Path,
			Ext:       ext,
			Year:      m.Year,
		})
		if err != nil {
			return "", err
		}
		dst = target.Path
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
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { // #nosec G301 -- organized media directories must remain readable by NAS/player users.
		return "", err
	}

	// Transfer the file according to the resolved mode. move 删除源；
	// copy/hardlink/symlink 保留源文件，从而让下载器可继续做种。
	if err := transferFile(m.Path, dst, mode); err != nil {
		return "", err
	}

	// Update the database row.
	if err := o.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Where("id = ?", m.ID).
		Updates(map[string]any{
			"path":        dst,
			"season_num":  m.SeasonNum,
			"episode_num": m.EpisodeNum,
		}).Error; err != nil {
		return dst, err
	}
	if err := transferSidecarNFO(m.Path, dst, mode); err != nil {
		o.log.Warn("organize sidecar nfo failed",
			zap.String("media", m.ID),
			zap.String("from", nfoPath(m.Path)),
			zap.String("to", nfoPath(dst)),
			zap.Error(err))
	}
	o.log.Info("organized",
		zap.String("media", m.ID),
		zap.String("from", m.Path),
		zap.String("to", dst),
		zap.String("category", category),
		zap.String("mode", string(mode)),
	)
	return dst, nil
}

// resolveBaseRoot picks the organize destination root (目的地目录): a
// per-request override wins, then the organize.target_dir setting, then the
// library's own path.
func (o *OrganizerService) resolveBaseRoot(ctx context.Context, lib *model.Library, override string) string {
	if r := strings.TrimSpace(override); r != "" {
		return r
	}
	if o.repo != nil && o.repo.Setting != nil {
		if v, err := o.repo.Setting.Get(ctx, "organize.target_dir"); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return lib.Path
}

// resolveSourceRoot picks the organize source root (源目录，待整理文件所在目录):
// a per-request override wins, then the organize.source_dir setting, then the
// library's own path. Library organize only touches media located under this
// root, so operators can point at a specific download/staging folder.
func (o *OrganizerService) resolveSourceRoot(ctx context.Context, lib *model.Library, override string) string {
	if r := strings.TrimSpace(override); r != "" {
		return r
	}
	if o.repo != nil && o.repo.Setting != nil {
		if v, err := o.repo.Setting.Get(ctx, "organize.source_dir"); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return lib.Path
}

// resolveTransferMode picks the transfer mode: a per-request override wins,
// otherwise the organize.transfer_mode setting (default move). When the
// effective mode is move and 做种保种 (organize.keep_seeding) is enabled, it is
// upgraded to hardlink so the source stays in place for the torrent client.
func (o *OrganizerService) resolveTransferMode(ctx context.Context, override TransferMode) TransferMode {
	mode := override
	if mode == "" {
		mode = TransferMove
		if o.repo != nil && o.repo.Setting != nil {
			if v, err := o.repo.Setting.Get(ctx, "organize.transfer_mode"); err == nil && strings.TrimSpace(v) != "" {
				mode = parseTransferMode(v)
			}
		}
	}
	if mode == TransferMove && o.keepSeedingEnabled(ctx) {
		// 移动会删除源文件导致 qBittorrent 停止做种；保种开启时改用硬链接
		// 既规范命名又保留源文件继续做种上传。硬链接失败时会报错，避免静默
		// 退化复制后占用双份磁盘空间。
		return TransferHardlink
	}
	return mode
}

// keepSeedingEnabled reports whether 做种保种 is on. Defaults to true so an
// unconfigured instance never silently breaks seeding on organize.
func (o *OrganizerService) keepSeedingEnabled(ctx context.Context) bool {
	if o.repo == nil || o.repo.Setting == nil {
		return true
	}
	v, err := o.repo.Setting.Get(ctx, "organize.keep_seeding")
	if err != nil || strings.TrimSpace(v) == "" {
		return true
	}
	return v == "true" || v == "1" || v == "on"
}

// OrganizeLibrary organizes every media row in a library whose file is
// not already in the expected path structure.
func (o *OrganizerService) OrganizeLibrary(ctx context.Context, libraryID string) (*OrganizeResult, error) {
	return o.OrganizeLibraryWithOptions(ctx, libraryID, OrganizeOptions{})
}

// OrganizeLibraryWithOptions is OrganizeLibrary with per-request overrides for
// the target path and transfer mode.
func (o *OrganizerService) OrganizeLibraryWithOptions(ctx context.Context, libraryID string, opts OrganizeOptions) (*OrganizeResult, error) {
	lib, err := o.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil {
		return nil, errors.New("library not found")
	}
	if _, ok := ParseCloudLibraryMount(lib.Path); ok {
		return nil, errors.New("local organize cannot use cloud libraries directly; use external storage scan/mount for cloud media or enable cloud transfer to write to cloud")
	}
	var rows []model.Media
	if err := o.repo.DB.WithContext(ctx).
		Where("library_id = ? AND deleted_at IS NULL", libraryID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	// 源目录（待整理）：仅整理位于该目录下的媒体；留空 = 整个媒体库。
	sourceRoot := o.resolveSourceRoot(ctx, lib, opts.SourcePath)
	if _, ok := ParseCloudLibraryMount(sourceRoot); ok {
		return nil, errors.New("organize source must be a local directory; cloud libraries should be managed from external storage scan/mount")
	}
	// 目的地目录：已位于该根下的文件视为已整理；受 dest_path 覆盖与设置影响。
	baseRoot := redirectOrganizeStagingRoot(o.resolveBaseRoot(ctx, lib, opts.DestPath))
	if _, ok := ParseCloudLibraryMount(baseRoot); ok {
		return nil, errors.New("organize destination must be a local writable media directory; enable cloud transfer in external storage when writing to cloud")
	}
	if !opts.DryRun {
		if err := ensureOrganizeDestinationWritable(baseRoot); err != nil {
			return nil, err
		}
	}
	res := &OrganizeResult{SourcePath: sourceRoot, DestPath: baseRoot, DryRun: opts.DryRun}
	for i := range rows {
		// 不在源目录内的文件跳过（不属于本次「从源目录整理」的范围）。
		if !pathWithin(rows[i].Path, sourceRoot) {
			res.Skipped++
			continue
		}
		if pathWithin(rows[i].Path, baseRoot) {
			res.Skipped++
			continue
		}
		dst, err := o.OrganizeMediaWithOptions(ctx, rows[i].ID, opts)
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

func (o *OrganizerService) refreshEpisodeIdentity(m *model.Media, lib *model.Library) error {
	season, episode := m.SeasonNum, m.EpisodeNum

	if parsedSeason, parsedEpisode := ParseEpisode(m.Path); parsedSeason > 0 || parsedEpisode > 0 {
		season = parsedSeason
		if parsedEpisode > 0 {
			episode = parsedEpisode
		}
	}

	if local, err := ReadLocalMetadata(m.Path, lib.Path, true); err == nil && local != nil {
		if local.SeasonNum > 0 || local.EpisodeNum > 0 {
			season = local.SeasonNum
		}
		if local.EpisodeNum > 0 {
			episode = local.EpisodeNum
		}
	} else if err != nil {
		o.log.Warn("organize read local metadata failed", zap.String("path", m.Path), zap.Error(err))
	}

	if season < 0 || episode <= 0 {
		return fmt.Errorf("cannot determine season/episode for %s", m.Path)
	}
	m.SeasonNum = season
	m.EpisodeNum = episode
	return nil
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
	// Cross-device: stream copy → remove. This can temporarily consume the
	// destination file size while copying, but the source is removed after the
	// copy succeeds.
	in, err := os.Open(src) // #nosec G304 -- src is selected from configured media/download roots by the organizer.
	if err != nil {
		return err
	}
	defer in.Close()
	// O_EXCL 保证不会覆盖已存在的目标。
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G304,G302 -- dst is organizer-generated; media files must remain readable by local players.
	if err != nil {
		return err
	}
	if _, werr := io.Copy(f, in); werr != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return werr
	}
	if cerr := f.Close(); cerr != nil {
		return cerr
	}
	return os.Remove(src)
}

// transferSidecarNFO moves/copies/links the .nfo sidecar alongside its media
// using the same transfer mode, so metadata follows the organized file.
func transferSidecarNFO(srcMedia, dstMedia string, mode TransferMode) error {
	src := nfoPath(srcMedia)
	dst := nfoPath(dstMedia)
	if src == dst {
		return nil
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { // #nosec G301 -- sidecar media directories must remain readable by NAS/player users.
		return err
	}
	return transferFile(src, dst, mode)
}

// sanitizeFilename removes characters not safe for filesystem names.
func sanitizeFilename(s string) string {
	r := strings.NewReplacer(
		"/", " ", "\\", " ", ":", " ", "*", "", "?", "",
		"\"", "", "<", "", ">", "", "|", "",
	)
	return strings.TrimSpace(r.Replace(s))
}

func (o *OrganizerService) organizeRoot(libraryPath, mediaType, category string) string {
	typeDir := o.mediaTypeRootDirForCategory(mediaType, category)
	if typeDir == "" || pathAlreadyEndsWith(libraryPath, typeDir) {
		return libraryPath
	}
	if isGenericMediaRoot(libraryPath) {
		return filepath.Join(libraryPath, typeDir)
	}
	return libraryPath
}

func (o *OrganizerService) mediaTypeRootDirForCategory(mediaType, category string) string {
	if root := o.categoryPhysicalRootDir(category); root != "" {
		return root
	}
	return mediaTypeRootDir(mediaType)
}

func (o *OrganizerService) categoryPhysicalRootDir(category string) string {
	key := normalizeOrganizeCategoryKey(category)
	if key == "" {
		return ""
	}
	categories := o.categoryMap()
	match := func(values ...string) bool {
		for _, value := range values {
			if key == normalizeOrganizeCategoryKey(value) {
				return true
			}
		}
		return false
	}
	switch {
	case match(
		categoryName(categories, "cn_anime", "国漫"),
		categoryName(categories, "jp_anime", "日番"),
		categoryName(categories, "children", "儿童"),
		"国漫", "国产动漫", "日番", "番剧", "日漫", "日本动漫", "日本动画", "儿童", "少儿",
	):
		return "动漫"
	case match(
		categoryName(categories, "domestic_tv", "国产剧"),
		categoryName(categories, "euus_tv", "欧美剧"),
		categoryName(categories, "jk_tv", "日韩剧"),
		categoryName(categories, "variety", "综艺"),
		categoryName(categories, "documentary", "纪录片"),
		categoryName(categories, "uncategorized_tv", "未分类"),
		"国产剧", "欧美剧", "日韩剧", "日剧", "韩剧", "综艺", "真人秀", "纪录片", "纪录", "未分类",
	):
		return "电视剧"
	case match(
		categoryName(categories, "animation_movie", "动画电影"),
		categoryName(categories, "chinese_movie", "华语电影"),
		categoryName(categories, "foreign_movie", "外语电影"),
		categoryName(categories, "euus_movie", "欧美电影"),
		categoryName(categories, "jk_movie", "日韩电影"),
		"动画电影", "动漫电影", "华语电影", "国产电影", "外语电影", "欧美电影", "日韩电影",
	):
		return "电影"
	case match(categoryName(categories, "adult", "成人"), categoryName(categories, "adult_9kg", "9KG"), categoryName(categories, "adult_jav", "番号"), "成人", "9kg", "番号", "jav"):
		return "成人"
	default:
		return ""
	}
}

func categoryRoot(root, category string) string {
	if strings.TrimSpace(category) == "" || pathAlreadyEndsWith(root, category) {
		return root
	}
	return filepath.Join(root, category)
}

func pathWithin(path, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if strings.EqualFold(cleanPath, cleanRoot) {
		return true
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func mediaTypeRootDir(mediaType string) string {
	switch normalizeMediaType(mediaType, "", "") {
	case "movie":
		return "电影"
	case "anime":
		return "动漫"
	case "tv", "variety":
		return "电视剧"
	case "adult":
		return "成人"
	default:
		return ""
	}
}

func isGenericMediaRoot(path string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(filepath.Clean(path))))
	switch base {
	case "media", "medias", "library", "libraries", "organized", "整理":
		return true
	default:
		return false
	}
}

// organizeStagingFolderNames 列出"手动整理"类暂存目录名。这些目录只是修正错误
// 入库时的中转工作区,不能作为一级分类目录留存。整理时若目标根落在这类目录
// 内,应重定向到其父级媒体根,让媒体真正归入 电影/电视剧/动漫/成人 的二级分类
// 目录中(如 媒体/电影/华语电影/片名),而不是停留在暂存目录下。
func organizeStagingFolderNames() map[string]struct{} {
	return map[string]struct{}{
		"手动整理": {}, "手动整理入库": {}, "待整理": {}, "待分类": {},
		"manual": {}, "manual_organize": {}, "manualorganize": {}, "staging": {}, "inbox": {},
	}
}

func isOrganizeStagingDir(path string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(filepath.Clean(path))))
	if base == "" {
		return false
	}
	_, ok := organizeStagingFolderNames()[base]
	return ok
}

// redirectOrganizeStagingRoot 把"手动整理"类暂存目录的目标根重定向到父级媒体根。
// 连续多层暂存目录(如 .../media/手动整理/待整理)会被逐层上提到真正的媒体根,
// 随后分类逻辑会补上 电影/电视剧/动漫/成人 等顶层与二级分类。
func redirectOrganizeStagingRoot(root string) string {
	cleaned := filepath.Clean(strings.TrimSpace(root))
	if cleaned == "" || cleaned == "." {
		return root
	}
	for isOrganizeStagingDir(cleaned) {
		parent := filepath.Dir(cleaned)
		if parent == cleaned || parent == "." || parent == string(filepath.Separator) {
			break
		}
		cleaned = parent
	}
	return cleaned
}

func pathAlreadyEndsWith(path, suffix string) bool {
	base := strings.TrimSpace(filepath.Base(filepath.Clean(path)))
	return strings.EqualFold(base, suffix)
}

func isSeriesLibraryType(mediaType string) bool {
	switch normalizeMediaType(mediaType, "", "") {
	case "tv", "anime", "variety":
		return true
	default:
		return false
	}
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

	// Determine media type from library
	lib, err := o.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil || lib == nil {
		return ""
	}
	return o.classifyMedia(ctx, m, lib.Type)
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
