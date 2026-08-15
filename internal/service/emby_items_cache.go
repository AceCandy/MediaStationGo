package service

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

const embyItemsCachePrefix = "media:emby:"

type embyItemsCacheValue struct {
	Items            []map[string]any `json:"items"`
	TotalRecordCount int64            `json:"total_record_count"`
	StartIndex       int              `json:"start_index"`
}

type embyLatestCacheValue struct {
	Items []map[string]any `json:"items"`
}

func (e *EmbyService) embyItemsCacheKey(kind string, p ItemsParams) string {
	includeTypes := append([]string(nil), p.IncludeItemTypes...)
	filters := append([]string(nil), p.Filters...)
	fields := append([]string(nil), p.Fields...)
	ids := append([]string(nil), p.IDs...)
	sort.Strings(includeTypes)
	sort.Strings(filters)
	sort.Strings(fields)
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		kind,
		p.UserID,
		p.ParentID,
		strings.Join(ids, ","),
		p.SearchTerm,
		strings.Join(includeTypes, ","),
		strings.Join(filters, ","),
		strings.Join(fields, ","),
		strconv.FormatBool(p.Recursive),
		p.SortBy,
		p.SortOrder,
		strconv.Itoa(p.StartIndex),
		strconv.Itoa(p.Limit),
	}, "|")))
	return embyItemsCachePrefix + hex.EncodeToString(sum[:])
}

func (e *EmbyService) embyLatestCacheKey(userID, parentID string, limit int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"latest", userID, parentID, strconv.Itoa(limit)}, "|")))
	return embyItemsCachePrefix + hex.EncodeToString(sum[:])
}

func (e *EmbyService) mediaCacheTTLSeconds() int {
	if e == nil || e.cfg == nil || e.cfg.Cache.MediaTTLSeconds < 1 {
		return 15
	}
	return e.cfg.Cache.MediaTTLSeconds
}
