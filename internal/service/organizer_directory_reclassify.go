package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type organizeExistingReclassifyRequest struct {
	Source          string
	Target          string
	DestRoot        string
	TargetLibraryID string
	Existing        []string
	DryRun          bool
	MediaType       string
	Category        string
	Title           string
	Year            int
	Season          int
	Episode         int
	Result          *OrganizeResult
}

func (o *OrganizerService) reclassifyExistingMedia(ctx context.Context, req organizeExistingReclassifyRequest) (bool, error) {
	if req.Result == nil || len(req.Existing) == 0 {
		return false, nil
	}
	if strings.TrimSpace(req.Category) == "" {
		return false, nil
	}
	target := filepath.Clean(strings.TrimSpace(req.Target))
	if target == "" || target == "." {
		return false, nil
	}
	candidates := reclassifyExistingCandidates(req.Existing, target, req.DestRoot)
	if len(candidates) == 0 {
		return false, nil
	}
	if organizeFileExists(target) {
		cleaned, err := o.cleanupReclassifiedDuplicates(ctx, req, target, candidates)
		return cleaned > 0, err
	}
	if len(candidates) != 1 || o.mediaPathExists(ctx, target) {
		return false, nil
	}
	oldPath := candidates[0]
	req.Result.Items = append(req.Result.Items, OrganizePreviewItem{
		Source: oldPath, Target: target, Action: "reclassify",
		MediaType: req.MediaType, Category: req.Category, Title: req.Title,
		Reason: "metadata category changed",
	})
	if req.DryRun {
		req.Result.Reclassified++
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { // #nosec G301 -- organized media directories must remain readable by NAS/player users.
		return false, err
	}
	if err := moveFile(oldPath, target); err != nil {
		return false, err
	}
	if err := moveSidecarNFO(oldPath, target); err != nil && o != nil && o.log != nil {
		o.log.Warn("organize reclassify sidecar nfo failed",
			zap.String("from", nfoPath(oldPath)),
			zap.String("to", nfoPath(target)),
			zap.Error(err))
	}
	if err := o.updateReclassifiedMediaRow(ctx, oldPath, target, req); err != nil {
		return false, err
	}
	cleanupEmptyMediaDirs(filepath.Dir(oldPath), req.DestRoot)
	if o != nil && o.log != nil {
		o.log.Info("organize reclassified existing media",
			zap.String("from", oldPath),
			zap.String("to", target),
			zap.String("category", req.Category),
			zap.String("media_type", req.MediaType))
	}
	req.Result.Reclassified++
	return true, nil
}

func reclassifyExistingCandidates(existing []string, target, destRoot string) []string {
	target = filepath.Clean(target)
	destRoot = filepath.Clean(strings.TrimSpace(destRoot))
	seen := map[string]struct{}{}
	out := make([]string, 0, len(existing))
	for _, path := range existing {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." || strings.EqualFold(path, target) {
			continue
		}
		if destRoot != "" && destRoot != "." && !pathWithin(path, destRoot) {
			continue
		}
		if !organizeFileExists(path) {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

func (o *OrganizerService) cleanupReclassifiedDuplicates(ctx context.Context, req organizeExistingReclassifyRequest, target string, candidates []string) (int, error) {
	cleaned := 0
	for _, oldPath := range candidates {
		if !safeToRemoveReclassifiedDuplicate(oldPath, target) {
			if o != nil && o.log != nil {
				o.log.Warn("organize kept duplicate with different size during reclassify",
					zap.String("path", oldPath),
					zap.String("target", target))
			}
			continue
		}
		req.Result.Items = append(req.Result.Items, OrganizePreviewItem{
			Source: oldPath, Target: target, Action: "cleanup",
			MediaType: req.MediaType, Category: req.Category, Title: req.Title,
			Reason: "duplicate after metadata category changed",
		})
		if req.DryRun {
			cleaned++
			continue
		}
		if err := removeMediaAndNFO(oldPath); err != nil {
			return cleaned, err
		}
		o.deleteMediaRowForPath(ctx, oldPath)
		cleanupEmptyMediaDirs(filepath.Dir(oldPath), req.DestRoot)
		if o != nil && o.log != nil {
			o.log.Info("organize cleaned duplicate after reclassify",
				zap.String("path", oldPath),
				zap.String("target", target),
				zap.String("category", req.Category),
				zap.String("media_type", req.MediaType))
		}
		cleaned++
	}
	req.Result.Reclassified += cleaned
	return cleaned, nil
}

func safeToRemoveReclassifiedDuplicate(path, target string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return false
	}
	if os.SameFile(info, targetInfo) {
		return true
	}
	return info.Size() > 0 && info.Size() == targetInfo.Size()
}

func organizeFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func moveSidecarNFO(oldMedia, newMedia string) error {
	oldNFO := nfoPath(oldMedia)
	newNFO := nfoPath(newMedia)
	if oldNFO == newNFO || !organizeFileExists(oldNFO) || organizeFileExists(newNFO) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(newNFO), 0o755); err != nil { // #nosec G301 -- sidecar media directories must remain readable by NAS/player users.
		return err
	}
	return moveFile(oldNFO, newNFO)
}

func removeMediaAndNFO(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if nfo := nfoPath(path); nfo != "" {
		if err := os.Remove(nfo); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (o *OrganizerService) updateReclassifiedMediaRow(ctx context.Context, oldPath, newPath string, req organizeExistingReclassifyRequest) error {
	if o == nil || o.repo == nil || o.repo.DB == nil {
		return nil
	}
	updates := map[string]any{
		"path": newPath,
	}
	if strings.TrimSpace(req.TargetLibraryID) != "" {
		updates["library_id"] = strings.TrimSpace(req.TargetLibraryID)
	}
	if strings.TrimSpace(req.Title) != "" {
		updates["title"] = strings.TrimSpace(req.Title)
	}
	if req.Year > 0 {
		updates["year"] = req.Year
	}
	if normalizeOrganizeMediaType(req.MediaType) == "movie" {
		updates["season_num"] = 0
		updates["episode_num"] = 0
	} else if req.Season > 0 {
		updates["season_num"] = req.Season
		if req.Episode > 0 {
			updates["episode_num"] = req.Episode
		}
	} else if req.Episode > 0 {
		updates["episode_num"] = req.Episode
	}
	return o.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("path = ?", oldPath).Updates(updates).Error
}

func (o *OrganizerService) deleteMediaRowForPath(ctx context.Context, path string) {
	if o == nil || o.repo == nil || o.repo.DB == nil {
		return
	}
	_ = o.repo.DB.WithContext(ctx).Where("path = ?", path).Delete(&model.Media{}).Error
}

func (o *OrganizerService) mediaPathExists(ctx context.Context, path string) bool {
	if o == nil || o.repo == nil || o.repo.DB == nil {
		return false
	}
	var count int64
	if err := o.repo.DB.WithContext(ctx).Unscoped().Model(&model.Media{}).Where("path = ?", path).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func cleanupEmptyMediaDirs(startDir, stopRoot string) {
	dir := filepath.Clean(strings.TrimSpace(startDir))
	stopRoot = filepath.Clean(strings.TrimSpace(stopRoot))
	for dir != "" && dir != "." {
		if stopRoot != "" && stopRoot != "." && (!pathWithin(dir, stopRoot) || strings.EqualFold(dir, stopRoot)) {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}
