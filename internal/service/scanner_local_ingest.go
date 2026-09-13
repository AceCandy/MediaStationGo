package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// ingestFile upserts a single media file. seenInodes dedups hardlinks within a
// single scan; pass a fresh map for one-off ingests. It mutates res counters.
func (s *ScannerService) ingestFile(ctx context.Context, lib *model.Library, root *model.LibraryRoot, path string, size, modTimeNS int64, seenInodes map[string]string, existingMedia map[string]existingLocalMedia, writeBatch *localMediaWriteBatch, res *ScanResult) {
	res.Visited++
	ext := strings.ToLower(filepath.Ext(path))
	cleanPath := filepath.Clean(path)

	fileID, skippedDuplicate := s.recordLocalFileIdentity(ctx, path, seenInodes, existingMedia, res)
	if skippedDuplicate {
		return
	}
	isNewMedia, skipUnchanged, updateReason := s.localMediaScanState(localMediaScanStateInput{
		ctx:           ctx,
		libraryID:     lib.ID,
		path:          path,
		cleanPath:     cleanPath,
		size:          size,
		modTimeNS:     modTimeNS,
		existingMedia: existingMedia,
	})
	if skipUnchanged {
		res.Skipped++
		return
	}

	parsedSeason, parsedEpisode := scanEpisodeNumbers(lib, path)
	localMeta := s.readLocalScanMetadata(lib, root, path, parsedSeason, parsedEpisode)
	media := s.buildLocalScanMedia(localScanMediaInput{
		lib:           lib,
		root:          root,
		path:          path,
		ext:           ext,
		fileID:        fileID,
		size:          size,
		modTimeNS:     modTimeNS,
		parsedSeason:  parsedSeason,
		parsedEpisode: parsedEpisode,
		localMeta:     localMeta,
	})
	if localMeta != nil {
		res.LocalMetadata++
	}

	s.writeLocalScanMedia(localScanWriteInput{
		ctx:          ctx,
		path:         path,
		media:        media,
		isNewMedia:   isNewMedia,
		updateReason: updateReason,
		writeBatch:   writeBatch,
		res:          res,
	})
}

func scanEpisodeNumbers(lib *model.Library, path string) (int, int) {
	if libraryIsMovieType(lib) {
		return 0, 0
	}
	if lib != nil {
		switch strings.ToLower(strings.TrimSpace(lib.Type)) {
		case "tv", "show", "shows", model.LibraryTypeNFOTV, model.LibraryTypeHongGuo:
			return parseStandardEpisode(path)
		}
	}
	return ParseEpisode(path)
}

func (s *ScannerService) recordLocalFileIdentity(ctx context.Context, path string, seenInodes map[string]string, existingMedia map[string]existingLocalMedia, res *ScanResult) (string, bool) {
	fileID, hasID := fileIdentity(path)
	if !hasID {
		return "", false
	}
	if first, ok := seenInodes[fileID]; ok && first != path {
		res.Skipped++
		s.log.Debug("scan skip hardlink duplicate",
			zap.String("path", path), zap.String("primary", first))
		return fileID, true
	}
	if existingMedia == nil {
		if other, ok := s.duplicateByFileID(ctx, fileID, path); ok {
			res.Skipped++
			s.log.Debug("scan skip hardlink duplicate (existing)",
				zap.String("path", path), zap.String("primary", other))
			return fileID, true
		}
	}
	seenInodes[fileID] = path
	return fileID, false
}

func (s *ScannerService) readLocalScanMetadata(lib *model.Library, root *model.LibraryRoot, path string, parsedSeason, parsedEpisode int) *LocalMetadata {
	if lib.Type == model.LibraryTypeHongGuo {
		return nil
	}
	rootPath := lib.Path
	if root != nil && strings.TrimSpace(root.Path) != "" {
		rootPath = root.Path
	}
	seriesLike := librarySupportsSeasons(lib) || parsedSeason > 0 || parsedEpisode > 0
	localMeta, err := ReadLocalMetadata(path, rootPath, seriesLike)
	if err != nil {
		s.log.Warn("read local metadata failed", zap.String("path", path), zap.Error(err))
	}
	if !libraryUsesNFOOnly(lib) {
		_, hints := pathHintMetadata(path, seriesLike)
		localMeta = hints.applyToLocalMetadata(localMeta)
	}
	return localMeta
}

type localMediaScanStateInput struct {
	ctx           context.Context
	libraryID     string
	path          string
	cleanPath     string
	size          int64
	modTimeNS     int64
	existingMedia map[string]existingLocalMedia
}

func (s *ScannerService) localMediaScanState(in localMediaScanStateInput) (bool, bool, string) {
	var existing existingLocalMedia
	var exists bool
	if in.existingMedia == nil {
		var media model.Media
		query := s.repo.DB.WithContext(in.ctx).
			Select("season_num", "scan_file_size_bytes", "scan_file_mtime_ns").
			Where("library_id = ? AND path = ?", in.libraryID, in.path).
			Limit(1).Find(&media)
		if query.Error != nil {
			return false, false, "未加载旧文件指纹"
		}
		if query.RowsAffected == 0 {
			return true, false, ""
		}
		existing = existingLocalMedia{
			SeasonNum:         media.SeasonNum,
			ScanFileSizeBytes: media.ScanFileSizeBytes,
			ScanFileMTimeNS:   media.ScanFileMTimeNS,
		}
		exists = true
	} else {
		existing, exists = in.existingMedia[in.cleanPath]
	}
	isNewMedia := !exists
	if !exists {
		return true, false, ""
	}
	// 旧负数季号不能因文件未变而永久跳过；仅按明确的 SxxExx 标记修复。
	if existing.SeasonNum < 0 {
		if season, episode := parseStandardEpisode(in.path); season >= 0 && episode > 0 {
			return false, false, "按文件名纠正异常季号"
		}
	}
	if existing.ScanFileMTimeNS != 0 && existing.ScanFileSizeBytes == in.size && existing.ScanFileMTimeNS == in.modTimeNS {
		return false, true, ""
	}
	reasons := make([]string, 0, 3)
	if existing.ScanFileMTimeNS == 0 {
		reasons = append(reasons, "首次补录文件指纹")
	}
	if existing.ScanFileSizeBytes != in.size {
		reasons = append(reasons, fmt.Sprintf("文件大小变化：%d → %d", existing.ScanFileSizeBytes, in.size))
	}
	if existing.ScanFileMTimeNS != 0 && existing.ScanFileMTimeNS != in.modTimeNS {
		reasons = append(reasons, fmt.Sprintf("mtime_ns 变化：%d → %d", existing.ScanFileMTimeNS, in.modTimeNS))
	}
	return isNewMedia, false, strings.Join(reasons, "；")
}

type localScanMediaInput struct {
	lib           *model.Library
	root          *model.LibraryRoot
	path          string
	ext           string
	fileID        string
	size          int64
	modTimeNS     int64
	parsedSeason  int
	parsedEpisode int
	localMeta     *LocalMetadata
}

func (s *ScannerService) buildLocalScanMedia(in localScanMediaInput) *model.Media {
	titlePath := in.path
	part, partGroupKey, multipart := activeMediaPartCandidate(in.lib.ID, in.path)
	if multipart {
		titlePath = mediaPartBasePath(in.path, part)
	}
	title, year := CleanQueryWithRecognition(context.Background(), s.repo, titlePath)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(titlePath), filepath.Ext(titlePath))
	}
	title, year = preferISOParentScrapeIdentity(in.path, in.lib.Path, title, year)

	media := &model.Media{
		LibraryID:         in.lib.ID,
		LibraryRootID:     libraryRootID(in.root),
		RelativePath:      localRelativePath(in.path, in.root),
		Title:             title,
		Year:              year,
		Path:              in.path,
		ScanFileSizeBytes: in.size,
		ScanFileMTimeNS:   in.modTimeNS,
		FileID:            in.fileID,
		SeasonNum:         in.parsedSeason,
		EpisodeNum:        in.parsedEpisode,
		PartGroupKey:      partGroupKey,
	}
	if multipart {
		media.PartIndex = part.index
	}
	if in.ext == ".strm" {
		if targetURL, err := readLocalSTRMTarget(in.path); err == nil && targetURL != "" {
			media.STRMURL = targetURL
		} else if err != nil {
			s.log.Debug("read local strm failed", zap.String("path", in.path), zap.Error(err))
		}
	}
	if in.localMeta != nil {
		if libraryUsesNFOOnly(in.lib) {
			applyLocalEpisodeMetadata(media, in.localMeta)
		} else {
			applyLocalScanHints(media, in.localMeta)
		}
		media.LocalMetadataHint = encodeLocalMetadataHint(in.localMeta)
	}
	if libraryIsMovieType(in.lib) {
		media.SeasonNum = 0
		media.EpisodeNum = 0
		media.SeriesID = ""
	}
	if media.EpisodeNum > 0 {
		media.SeriesID = localSeriesIdentity(media)
	}
	if in.lib.Type == model.LibraryTypeHongGuo {
		media.CatalogSource = model.TaskSystemHongGuo
		media.ScrapeStatus = "source_pending"
		idPath := media.RelativePath
		if idPath == "" {
			idPath = in.path
		}
		media.LookupCatalogID, _ = hongguo.PathID(idPath)
	}
	return media
}

type localScanWriteInput struct {
	ctx          context.Context
	path         string
	media        *model.Media
	isNewMedia   bool
	updateReason string
	writeBatch   *localMediaWriteBatch
	res          *ScanResult
}

func (s *ScannerService) writeLocalScanMedia(in localScanWriteInput) {
	if in.isNewMedia && in.writeBatch != nil {
		in.writeBatch.AddWithReason(in.path, in.media, in.updateReason)
		return
	}
	if err := s.invalidateChangedMediaProbe(in.ctx, in.path, in.updateReason); err != nil {
		addScanError(in.res, in.path, err)
		s.log.Warn("invalidate changed media probe failed", zap.String("path", in.path), zap.Error(err))
		return
	}
	if err := s.upsertLocalScanMedia(in.ctx, in.media); err != nil {
		addScanError(in.res, in.path, err)
		s.log.Warn("upsert media failed", zap.String("path", in.path), zap.Error(err))
		return
	}
	if in.isNewMedia {
		in.res.Added++
		in.res.addChange(ScanChangeAdded, in.path, "")
	} else {
		in.res.Updated++
		in.res.addChange(ScanChangeUpdated, in.path, in.updateReason)
	}
	s.publishLocalScanProgress(in.path, in.res)
}

func (s *ScannerService) invalidateChangedMediaProbe(ctx context.Context, path, updateReason string) error {
	if strings.TrimSpace(updateReason) == "" || s.repo == nil || s.repo.MediaProbe == nil || s.repo.Media == nil {
		return nil
	}
	media, err := s.repo.Media.FindByPath(ctx, path)
	if err != nil || media == nil {
		return err
	}
	return s.repo.MediaProbe.DeleteByMediaID(ctx, media.ID)
}

func (s *ScannerService) upsertLocalScanMedia(ctx context.Context, media *model.Media) error {
	task, expectedMetadataID, err := s.startExistingMetadataMatchTask(ctx, media)
	if err != nil {
		return err
	}
	err = s.repo.Media.Upsert(ctx, media)
	if task == nil {
		return err
	}
	if err == nil && (media.ScrapeStatus != "matched" || media.MetadataID != expectedMetadataID) {
		err = errors.New("existing metadata binding did not complete")
	}
	safeErr := sanitizeTaskLogError(err)
	detail := "✅ " + existingMetadataMatchName(media) + "：命中已有元数据"
	stage, message := "completed", "已入库媒体命中已有元数据"
	if safeErr != nil {
		stage, message = "scrape", "已有元数据绑定失败"
		detail = "❌ " + existingMetadataMatchName(media) + "：绑定失败: " + safeErr.Error()
	}
	metrics := map[string]int64{"processed": 1}
	if safeErr == nil {
		metrics["matched"] = 1
	}
	task.Finish(safeErr, TaskUpdate{Stage: stage, Message: message, Metrics: metrics, Details: []string{detail}})
	return err
}

func (s *ScannerService) startExistingMetadataMatchTask(ctx context.Context, media *model.Media) (*TaskHandle, string, error) {
	if s == nil || media == nil || s.repo == nil || s.repo.Media == nil || s.repo.DB == nil {
		return nil, "", nil
	}
	var existing model.Media
	result := s.repo.DB.WithContext(ctx).
		Select("metadata_id", "scrape_status").Where("path = ?", media.Path).Limit(1).Find(&existing)
	if result.Error != nil {
		return nil, "", result.Error
	}
	if result.RowsAffected > 0 && (strings.TrimSpace(existing.MetadataID) != "" || strings.TrimSpace(existing.ScrapeStatus) == "matched") {
		return nil, "", nil
	}
	exact, err := s.repo.Media.FindExactMetadata(ctx, media)
	if err != nil || exact == nil {
		return nil, "", err
	}
	if s.scraper == nil || s.scraper.tasks == nil {
		return nil, exact.ID, nil
	}
	task := s.scraper.tasks.StartTriggered(TaskKindScrape, TaskTriggerEvent, "媒体入库刮削："+existingMetadataMatchName(media), TaskUpdate{
		Stage: "scrape", SourcePath: media.Path, Message: "正在绑定已有元数据",
	})
	if task == nil {
		return nil, exact.ID, nil
	}
	return task, exact.ID, nil
}

func existingMetadataMatchName(media *model.Media) string {
	if media == nil {
		return "媒体"
	}
	if name := strings.TrimSpace(media.Title); name != "" {
		return name
	}
	if name := strings.TrimSpace(media.ID); name != "" {
		return name
	}
	return "媒体"
}

func (s *ScannerService) publishLocalScanProgress(path string, res *ScanResult) {
	s.hub.Publish("scan", map[string]any{
		"library_id": res.LibraryID,
		"path":       path,
		"visited":    res.Visited,
		"added":      res.Added,
		"updated":    res.Updated,
		"probed":     res.Probed,
		"local_meta": res.LocalMetadata,
	})
}

// duplicateByFileID reports an existing media path that shares the given inode
// identity but lives at a different path and still exists on disk.
func (s *ScannerService) duplicateByFileID(ctx context.Context, fileID, path string) (string, bool) {
	if fileID == "" {
		return "", false
	}
	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).
		Where("file_id = ? AND path <> ?", fileID, path).
		Limit(8).Find(&rows).Error; err != nil {
		return "", false
	}
	for _, r := range rows {
		if r.Path == "" {
			continue
		}
		if _, err := os.Stat(r.Path); err == nil {
			return r.Path, true
		}
	}
	return "", false
}
