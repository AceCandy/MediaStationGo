package service

import (
	"strconv"
	"strings"
	"time"
)

func embyRandomSort(p ItemsParams) bool {
	return primarySupportedEmbySort(p.SortBy, containsEmbyFilter(p.Filters, "IsResumable")) == "random"
}

// embyRandomOrder 只接收固定身份表达式；同种子使 OFFSET 补取不重新洗牌。
// ponytail: 仍扫描轻量作品候选；候选规模成为瓶颈时再改为索引采样。
func embyRandomOrder(p ItemsParams, identity string) string {
	return "md5((" + identity + ")::text || '" + strconv.FormatUint(p.randomSeed, 10) + "')"
}

const embyDateTimeLayout = "2006-01-02T15:04:05.0000000Z"

func formatEmbyDateTime(value time.Time) string {
	return value.UTC().Format(embyDateTimeLayout)
}

func containsItemType(types []string, want string) bool {
	for _, t := range types {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}

func containsSupportedEmbyItemType(types []string) bool {
	for _, itemType := range types {
		switch strings.ToLower(strings.TrimSpace(itemType)) {
		case "movie", "series", "season", "episode", "video", "folder", "collectionfolder":
			return true
		}
	}
	return false
}

func containsOnlyPersonItemTypes(types []string) bool {
	if len(types) == 0 {
		return false
	}
	for _, typ := range types {
		if !strings.EqualFold(strings.TrimSpace(typ), "person") {
			return false
		}
	}
	return true
}

func containsOnlyFavoriteItemTypes(types []string) bool {
	for _, itemType := range types {
		switch strings.ToLower(strings.TrimSpace(itemType)) {
		case "movie", "series":
		default:
			return false
		}
	}
	return len(types) > 0
}

func containsOnlyFolderItemTypes(types []string) bool {
	if len(types) == 0 {
		return false
	}
	for _, itemType := range types {
		switch strings.ToLower(strings.TrimSpace(itemType)) {
		case "folder", "collectionfolder":
		default:
			return false
		}
	}
	return true
}

func emptyItemsEnvelope(startIndex int) map[string]any {
	return map[string]any{
		"Items":            []map[string]any{},
		"TotalRecordCount": int64(0),
		"StartIndex":       startIndex,
	}
}

func containsEmbyFilter(filters []string, want string) bool {
	for _, filter := range filters {
		if strings.EqualFold(strings.TrimSpace(filter), want) {
			return true
		}
	}
	return false
}

func firstCSVValue(value string) string {
	if i := strings.Index(value, ","); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

func primarySupportedEmbySort(sortBy string, resumeFilter bool) string {
	for _, part := range strings.Split(sortBy, ",") {
		key := strings.ToLower(strings.TrimSpace(part))
		switch key {
		case "random", "sortname", "name", "premieredate", "productionyear", "datecreated", "datelastcontentadded", "communityrating":
			return key
		case "dateplayed":
			if resumeFilter {
				return key
			}
		}
	}
	return strings.ToLower(strings.TrimSpace(firstCSVValue(sortBy)))
}

// libraryWorkSortParams 将媒体库作品的默认和入库排序统一为全局作品时间。
func libraryWorkSortParams(p ItemsParams) ItemsParams {
	switch primarySupportedEmbySort(p.SortBy, false) {
	case "", "datecreated", "datelastcontentadded":
		p.SortBy = "DateLastContentAdded"
		if strings.TrimSpace(p.SortOrder) == "" {
			p.SortOrder = "Descending"
		}
	}
	return p
}

func pageSlice[T any](items []T, start, limit int) []T {
	if start < 0 {
		start = 0
	}
	if limit <= 0 {
		limit = len(items)
	}
	if start >= len(items) {
		return []T{}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func emptyUserData() map[string]any {
	return map[string]any{
		"PlaybackPositionTicks": 0,
		"PlayCount":             0,
		"IsFavorite":            false,
		"Played":                false,
		"PlayedPercentage":      0,
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
