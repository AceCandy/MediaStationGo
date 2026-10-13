package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// ExtensionIdentity 描述外部作品；季集坐标只用于整剧的精确分集匹配。
type ExtensionIdentity struct {
	Key        string `json:"key"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	MediaType  string `json:"media_type"`
	Season     *int   `json:"season,omitempty"`
	Episode    *int   `json:"episode,omitempty"`
	Kind       string `json:"kind"`
}

func (i *ExtensionIdentity) Normalize() bool {
	if len(i.Key) == 0 || len(i.Key) > 128 {
		return false
	}
	switch i.Source {
	case "tmdb", "douban", "bangumi":
	default:
		return false
	}
	id, err := strconv.ParseInt(i.ExternalID, 10, 64)
	if err != nil || id <= 0 {
		return false
	}
	i.ExternalID = strconv.FormatInt(id, 10)
	switch i.MediaType {
	case "movie":
		i.Kind = "movie"
	case "tv", "series", "anime":
		i.Kind = "series"
	default:
		return false
	}
	if (i.Season == nil) != (i.Episode == nil) {
		return false
	}
	return i.Season == nil || (i.Kind == "series" && *i.Season >= 0 && *i.Season <= 10000 && *i.Episode > 0 && *i.Episode <= 100000)
}

// ExtensionLibraryMatch 只包含当前用户能继续向 Emby 查询的逻辑媒体身份。
type ExtensionLibraryMatch struct {
	Key      string `json:"key"`
	ItemID   string `json:"item_id"`
	ItemType string `json:"item_type"`
}

func (r *MediaViewRepository) ExtensionLibraryMatches(ctx context.Context, items []ExtensionIdentity, filter MediaQueryFilter) ([]ExtensionLibraryMatch, error) {
	if len(items) == 0 || len(items) > 100 {
		return nil, errors.New("每次匹配需提供 1 至 100 个作品")
	}
	seen := map[string]bool{}
	for n := range items {
		if !items[n].Normalize() || seen[items[n].Key] {
			return nil, errors.New("作品身份或匹配键无效")
		}
		seen[items[n].Key] = true
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	out := []ExtensionLibraryMatch{}
	q := r.db.WithContext(ctx).Table(`jsonb_to_recordset(?) AS requested(key text,source text,external_id text,kind text,season integer,episode integer)`, string(raw)).
		Joins(`JOIN metadata_identifiers ident ON ident.provider=requested.source AND ident.entity_kind=requested.kind AND ident.external_id=requested.external_id`).
		Joins(`JOIN metadata_items work ON work.id=ident.metadata_id AND work.kind=requested.kind`).
		Joins(`JOIN LATERAL (
   SELECT work.id,work.kind WHERE requested.season IS NULL
   UNION ALL SELECT ep.id,ep.kind FROM metadata_items s JOIN metadata_items ep ON ep.parent_id=s.id AND ep.kind='episode'
   WHERE work.kind='series' AND s.parent_id=work.id AND s.kind='season' AND s.season_num=requested.season AND ep.episode_num=requested.episode
  ) target ON TRUE`).
		Joins(`JOIN LATERAL (
   SELECT target.id
   UNION ALL SELECT s.id FROM metadata_items s WHERE target.kind='series' AND s.parent_id=target.id AND s.kind='season'
   UNION ALL SELECT ep.id FROM metadata_items s JOIN metadata_items ep ON ep.parent_id=s.id AND ep.kind='episode'
   WHERE target.kind='series' AND s.parent_id=target.id AND s.kind='season'
  ) owned ON TRUE`).
		Joins(`JOIN media m ON m.metadata_id=owned.id`).
		Joins(`JOIN metadata_items mi ON mi.id=m.metadata_id`).
		Joins(`JOIN libraries lib ON lib.id=m.library_id AND lib.enabled=TRUE AND lib.deleted_at IS NULL`)
	err = applyMediaViewFilter(q, filter).Select(`DISTINCT requested.key,target.id AS item_id,target.kind AS item_type`).Order("requested.key,target.id").Scan(&out).Error
	for n := range out {
		out[n].ItemType = strings.ToUpper(out[n].ItemType[:1]) + out[n].ItemType[1:]
	}
	return out, err
}
