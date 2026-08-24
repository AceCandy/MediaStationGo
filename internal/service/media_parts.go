package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var mediaPartPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(.*?)[ ._-]+[([]?(cd|dvd|part|pt|disc|disk)[ ._-]*([0-9]+|[a-d])[]) ]?$`),
	regexp.MustCompile(`(?i)^(.*?[])}])[(]?(cd|dvd|part|pt|disc|disk)[ ._-]*([0-9]+|[a-d])[)]?$`),
}

type mediaPartCandidate struct {
	baseName string
	partType string
	index    int
}

// parseMediaPartCandidate 只识别文件名末尾、边界明确的标准 multipart 标记。
func parseMediaPartCandidate(path string) (mediaPartCandidate, bool) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for _, pattern := range mediaPartPatterns {
		match := pattern.FindStringSubmatch(name)
		if len(match) != 4 {
			continue
		}
		baseName := strings.TrimSpace(strings.TrimRight(match[1], " ._-"))
		index, ok := mediaPartIndex(match[3])
		if baseName == "" || !ok {
			return mediaPartCandidate{}, false
		}
		return mediaPartCandidate{baseName: baseName, partType: strings.ToLower(match[2]), index: index}, true
	}
	return mediaPartCandidate{}, false
}

func mediaPartIndex(value string) (int, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) == 1 && value[0] >= 'a' && value[0] <= 'd' {
		return int(value[0]-'a') + 1, true
	}
	index, err := strconv.Atoi(value)
	return index, err == nil && index > 0
}

func mediaPartCandidateKey(libraryID, path string, candidate mediaPartCandidate) string {
	raw := strings.Join([]string{
		strings.TrimSpace(libraryID),
		filepath.Clean(filepath.Dir(path)),
		strings.ToLower(candidate.baseName),
		candidate.partType,
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func mediaPartBasePath(path string, candidate mediaPartCandidate) string {
	return filepath.Join(filepath.Dir(path), candidate.baseName+filepath.Ext(path))
}

// activeMediaPartCandidate 要求同目录至少有两个不同序号，避免把作品名中的 Part 1 当成文件分段。
func activeMediaPartCandidate(libraryID, path string) (mediaPartCandidate, string, bool) {
	candidate, ok := parseMediaPartCandidate(path)
	if !ok {
		return mediaPartCandidate{}, "", false
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return mediaPartCandidate{}, "", false
	}
	// ponytail: 只为 multipart 候选读取同目录；候选密集目录出现实测瓶颈时再下沉扫描批次索引。
	indexes := map[int]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if _, video := videoExtensions[ext]; !video {
			continue
		}
		other, matched := parseMediaPartCandidate(filepath.Join(filepath.Dir(path), entry.Name()))
		if !matched || !strings.EqualFold(other.baseName, candidate.baseName) || other.partType != candidate.partType {
			continue
		}
		if _, duplicate := indexes[other.index]; duplicate {
			return mediaPartCandidate{}, "", false
		}
		indexes[other.index] = struct{}{}
	}
	if len(indexes) < 2 {
		return mediaPartCandidate{}, "", false
	}
	return candidate, mediaPartCandidateKey(libraryID, path, candidate), true
}

// reconcileMediaParts 根据扫描后的真实文件集合写入或清除 multipart 关系。
func (s *ScannerService) reconcileMediaParts(ctx context.Context, libraryID, directory string) ([]string, error) {
	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).Where("library_id = ? AND path NOT LIKE ?", libraryID, "cloud://%").Find(&rows).Error; err != nil {
		return nil, err
	}
	directory = filepath.Clean(strings.TrimSpace(directory))
	if directory != "" && directory != "." {
		filtered := rows[:0]
		for _, row := range rows {
			parent := filepath.Dir(row.Path)
			if sameLibraryPath(parent, directory) || pathWithin(parent, directory) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	type candidateRow struct {
		row       model.Media
		candidate mediaPartCandidate
		key       string
	}
	groups := map[string][]candidateRow{}
	candidates := map[string]candidateRow{}
	for _, row := range rows {
		candidate, ok := parseMediaPartCandidate(row.Path)
		if !ok {
			continue
		}
		key := mediaPartCandidateKey(libraryID, row.Path, candidate)
		entry := candidateRow{row: row, candidate: candidate, key: key}
		groups[key] = append(groups[key], entry)
		candidates[row.ID] = entry
	}
	valid := map[string]bool{}
	for key, group := range groups {
		indexes := map[int]struct{}{}
		valid[key] = len(group) >= 2
		for _, entry := range group {
			if _, duplicate := indexes[entry.candidate.index]; duplicate {
				valid[key] = false
				break
			}
			indexes[entry.candidate.index] = struct{}{}
		}
	}

	changed := make([]string, 0)
	for _, row := range rows {
		desiredKey := ""
		desiredIndex := 0
		entry, candidate := candidates[row.ID]
		if candidate && valid[entry.key] {
			desiredKey = entry.key
			desiredIndex = entry.candidate.index
		}
		if row.PartGroupKey == desiredKey && row.PartIndex == desiredIndex {
			continue
		}
		updates := map[string]any{"part_group_key": desiredKey, "part_index": desiredIndex}
		if strings.TrimSpace(row.LocalMetadataHint) == "" {
			titlePath := row.Path
			if desiredKey != "" {
				titlePath = mediaPartBasePath(row.Path, entry.candidate)
			}
			title, year := CleanQueryWithRecognition(ctx, s.repo, titlePath)
			if title == "" {
				title = strings.TrimSuffix(filepath.Base(titlePath), filepath.Ext(titlePath))
			}
			updates["scan_title"] = title
			updates["scan_year"] = year
			if row.ScrapeStatus == "no_match" && (!strings.EqualFold(strings.TrimSpace(row.Title), strings.TrimSpace(title)) || row.Year != year) {
				updates["scrape_status"] = "pending"
			}
		}
		if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return changed, err
		}
		changed = append(changed, row.Path)
	}
	sort.Strings(changed)
	return changed, nil
}

func recordMediaPartScanChanges(res *ScanResult, paths []string) {
	for _, path := range paths {
		alreadyRecorded := false
		for _, change := range res.Changes {
			if filepath.Clean(change.Path) == filepath.Clean(path) {
				alreadyRecorded = true
				break
			}
		}
		if alreadyRecorded {
			continue
		}
		res.Updated++
		res.addChange(ScanChangeUpdated, path, "多 Part 关系变化")
	}
}

func collapseMediaParts(rows []model.Media) []model.Media {
	if len(rows) < 2 {
		return rows
	}
	minimum := map[string]int{}
	for _, row := range rows {
		if row.PartGroupKey == "" || row.PartIndex <= 0 {
			continue
		}
		if current, ok := minimum[row.PartGroupKey]; !ok || row.PartIndex < current {
			minimum[row.PartGroupKey] = row.PartIndex
		}
	}
	out := rows[:0]
	seen := map[string]struct{}{}
	for _, row := range rows {
		if row.PartGroupKey == "" || row.PartIndex <= 0 {
			out = append(out, row)
			continue
		}
		if row.PartIndex != minimum[row.PartGroupKey] {
			continue
		}
		if _, ok := seen[row.PartGroupKey]; ok {
			continue
		}
		seen[row.PartGroupKey] = struct{}{}
		out = append(out, row)
	}
	return out
}

func collapseMediaPartViews(rows []model.MediaView) []model.MediaView {
	media := make([]model.Media, len(rows))
	viewsByID := make(map[string]model.MediaView, len(rows))
	for i := range rows {
		media[i] = rows[i].Media
		viewsByID[rows[i].ID] = rows[i]
	}
	collapsed := collapseMediaParts(media)
	out := make([]model.MediaView, 0, len(collapsed))
	for _, row := range collapsed {
		out = append(out, viewsByID[row.ID])
	}
	return out
}

// mediaPartViews 返回当前用户可见、按播放顺序排列的同版本物理 Part。
func (e *EmbyService) mediaPartViews(ctx context.Context, m *model.MediaView, userID string) ([]model.MediaView, error) {
	if e == nil || e.repo == nil || e.repo.DB == nil || e.repo.MediaView == nil || m == nil || strings.TrimSpace(m.PartGroupKey) == "" {
		return nil, nil
	}
	var rows []model.Media
	if err := e.repo.DB.WithContext(ctx).
		Where("part_group_key = ? AND part_index > 0", m.PartGroupKey).
		Order("part_index ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	views, err := e.repo.MediaView.FindByIDs(ctx, ids, e.mediaQueryFilter(ctx, userID))
	if err != nil {
		return nil, err
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].PartIndex != views[j].PartIndex {
			return views[i].PartIndex < views[j].PartIndex
		}
		return views[i].ID < views[j].ID
	})
	return views, nil
}
