package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (e *EmbyService) seriesIDForMedia(m *model.MediaView) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.SeriesID)
}

func (e *EmbyService) seriesNameForMedia(m *model.MediaView) string {
	if name := strings.TrimSpace(m.SeriesTitle); name != "" {
		return name
	}
	if m.MetadataKind == model.MetadataKindSeries {
		return strings.TrimSpace(m.Title)
	}
	if name := inferSeriesNameFromPath(m.Path); name != "" {
		return name
	}
	name := strings.TrimSpace(m.Title)
	name = embyEpisodeTitleRE.ReplaceAllString(name, "")
	name = embyYearSuffixRE.ReplaceAllString(name, "")
	if name == "" {
		name = strings.TrimSpace(m.OriginalName)
	}
	return name
}

func inferSeriesNameFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	base := filepath.Base(dir)
	if embySeasonDirRE.MatchString(base) {
		dir = filepath.Dir(dir)
		base = filepath.Base(dir)
	}
	base = strings.TrimSpace(embyYearSuffixRE.ReplaceAllString(base, ""))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func seasonName(seasonNum int) string {
	if seasonNum == 0 {
		return "特别篇"
	}
	if seasonNum < 0 {
		seasonNum = 1
	}
	return fmt.Sprintf("第 %d 季", seasonNum)
}

func sortSeriesGroups(groups []embySeriesGroup, p ItemsParams) {
	switch primarySupportedEmbySort(p.SortBy, false) {
	case "sortname", "name":
		sort.SliceStable(groups, func(i, j int) bool {
			if strings.EqualFold(p.SortOrder, "Descending") {
				return groups[i].Name > groups[j].Name
			}
			return groups[i].Name < groups[j].Name
		})
	case "datecreated":
		sort.SliceStable(groups, func(i, j int) bool {
			if strings.EqualFold(p.SortOrder, "Ascending") {
				return groups[i].CreatedAt.Before(groups[j].CreatedAt)
			}
			return groups[i].CreatedAt.After(groups[j].CreatedAt)
		})
	default:
		sort.SliceStable(groups, func(i, j int) bool {
			if strings.EqualFold(p.SortOrder, "Ascending") {
				return embySeriesReleaseSortTime(groups[i]).Before(embySeriesReleaseSortTime(groups[j]))
			}
			return embySeriesReleaseSortTime(groups[i]).After(embySeriesReleaseSortTime(groups[j]))
		})
	}
}

func embySeriesReleaseSortTime(group embySeriesGroup) time.Time {
	return mediaReleaseSortTime(model.Media{
		ReleaseDate: group.ReleaseDate,
		Year:        group.Year,
		PermanentBase: model.PermanentBase{
			CreatedAt: group.CreatedAt,
			UpdatedAt: group.CreatedAt,
		},
	})
}
