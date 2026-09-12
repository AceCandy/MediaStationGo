package repository

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var ErrTMDbRecheckFilter = errors.New("invalid recheck filter")

type TMDbRecheckRow struct {
	model.TMDbRecheckJob
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	SeriesTitle string `json:"series_title"`
	SeasonNum   int    `json:"season_num"`
	EpisodeNum  int    `json:"episode_num"`
}

type TMDbRecheckFile struct {
	MediaID    string `json:"media_id"`
	Path       string `json:"path"`
	LibraryID  string `json:"library_id"`
	CanPreview bool   `json:"can_preview"`
}

type TMDbRecheckFilesPage struct {
	Items   []TMDbRecheckFile `json:"items"`
	HasMore bool              `json:"has_more"`
	Page    int               `json:"page"`
}

// ListTMDbRecheckFiles 按需列出具体文件版本，不读取 STRM 内容或返回播放地址。
func (r *MetadataRepository) ListTMDbRecheckFiles(ctx context.Context, id string, page, size int) (TMDbRecheckFilesPage, error) {
	out := TMDbRecheckFilesPage{Items: []TMDbRecheckFile{}, Page: page}
	if id == "" || page < 1 || page > 1000000 || size < 1 || size > 100 {
		return out, ErrTMDbRecheckFilter
	}
	err := r.db.WithContext(ctx).Raw(`SELECT m.id AS media_id, m.path, m.library_id
FROM tm_db_recheck_jobs job
JOIN metadata_items target ON target.id=job.metadata_id AND target.kind IN ('season','episode')
JOIN LATERAL (
 SELECT target.id
 UNION ALL
 SELECT child.id FROM metadata_items child WHERE target.kind='season' AND child.parent_id=target.id AND child.kind='episode'
) linked ON true
JOIN media m ON m.metadata_id=linked.id
WHERE job.metadata_id=? AND job.status='not_found'
ORDER BY m.id LIMIT ? OFFSET ?`, id, size+1, (page-1)*size).Scan(&out.Items).Error
	if err != nil {
		return out, err
	}
	out.HasMore = len(out.Items) > size
	if out.HasMore {
		out.Items = out.Items[:size]
	}
	for i := range out.Items {
		file := &out.Items[i]
		if !filepath.IsAbs(file.Path) {
			file.Path = "非本地文件"
			continue
		}
		file.CanPreview = strings.EqualFold(filepath.Ext(file.Path), ".strm")
	}
	return out, nil
}

type TMDbRecheckPage struct {
	Items    []TMDbRecheckRow `json:"items"`
	Counts   map[string]int64 `json:"counts"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Changes  int64            `json:"changes"`
}

// TMDbRecheckSummary 独立于分页加载，计数仍按当前媒体关联计算。
type TMDbRecheckSummary struct {
	Counts  map[string]int64 `json:"counts"`
	Changes int64            `json:"changes"`
}

// 集的半连接可批量执行；季先限定到队列范围，避免为全库无待办的季检查文件。
const tmdbRecheckCountsSQL = `SELECT status, count(*) AS n FROM (
 SELECT jobs.status FROM (?) jobs
 JOIN metadata_items target ON target.id=jobs.metadata_id AND target.kind='episode'
 WHERE EXISTS (SELECT 1 FROM media m WHERE m.metadata_id=target.id)
 UNION ALL
 SELECT target.status FROM (
  SELECT jobs.status, mi.id FROM (?) jobs
  JOIN metadata_items mi ON mi.id=jobs.metadata_id AND mi.kind='season' OFFSET 0
 ) target
 WHERE EXISTS (SELECT 1 FROM media m WHERE m.metadata_id=target.id)
 OR EXISTS (SELECT 1 FROM metadata_items child JOIN media m ON m.metadata_id=child.id
  WHERE child.parent_id=target.id AND child.kind='episode')
) visible GROUP BY status`

func (r *MetadataRepository) tmdbRecheckCounts(ctx context.Context, status string) (map[string]int64, error) {
	jobs := r.db.WithContext(ctx).Table("tm_db_recheck_jobs").Select("metadata_id, status")
	if status != "" {
		jobs = jobs.Where("status=?", status)
	}
	var rows []struct {
		Status string
		N      int64
	}
	err := r.db.WithContext(ctx).Raw(tmdbRecheckCountsSQL, jobs, jobs).Scan(&rows).Error
	counts := map[string]int64{}
	for _, row := range rows {
		counts[row.Status] = row.N
	}
	return counts, err
}

func (r *MetadataRepository) SummarizeTMDbRechecks(ctx context.Context) (TMDbRecheckSummary, error) {
	counts, err := r.tmdbRecheckCounts(ctx, "")
	out := TMDbRecheckSummary{Counts: counts}
	if err != nil {
		return out, err
	}
	err = r.db.WithContext(ctx).Raw(`SELECT
 (SELECT count(*) FROM tm_db_recheck_changes WHERE pending) +
 (SELECT count(*) FROM tm_db_recheck_asset_changes)`).Scan(&out.Changes).Error
	return out, err
}

// ListTMDbRechecks 按剧名、季号和集号排序后分页，不改变后台复查调度顺序。
func (r *MetadataRepository) ListTMDbRechecks(ctx context.Context, status, keyword string, page, size int) (TMDbRecheckPage, error) {
	return r.listTMDbRechecks(ctx, status, keyword, page, size, true)
}

// ListTMDbRecheckItems 只计算当前筛选总数，不随翻页重复查询全状态统计。
func (r *MetadataRepository) ListTMDbRecheckItems(ctx context.Context, status, keyword string, page, size int) (TMDbRecheckPage, error) {
	return r.listTMDbRechecks(ctx, status, keyword, page, size, false)
}

func (r *MetadataRepository) listTMDbRechecks(ctx context.Context, status, keyword string, page, size int, withSummary bool) (TMDbRecheckPage, error) {
	out := TMDbRecheckPage{Items: []TMDbRecheckRow{}, Counts: map[string]int64{}, Page: page, PageSize: size}
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return out, ErrTMDbRecheckFilter
	}
	switch status {
	case "", "pending", "running", "retry", "not_found", "blocked", "done":
	default:
		return out, ErrTMDbRecheckFilter
	}
	// 与关联文件列表使用相同范围；媒体删除后无需等待后台归并，统计与分页同步排除空待办。
	q := r.db.WithContext(ctx).Table("tm_db_recheck_jobs AS jobs").Where(`EXISTS (
SELECT 1 FROM metadata_items target
WHERE target.id=jobs.metadata_id AND target.kind IN ('season','episode')
AND (EXISTS (SELECT 1 FROM media m WHERE m.metadata_id=target.id)
 OR (target.kind='season' AND EXISTS (
  SELECT 1 FROM metadata_items child JOIN media m ON m.metadata_id=child.id
  WHERE child.parent_id=target.id AND child.kind='episode'))))`)
	keyword = strings.TrimSpace(keyword)
	if withSummary {
		summary, err := r.SummarizeTMDbRechecks(ctx)
		if err != nil {
			return out, err
		}
		out.Counts, out.Changes = summary.Counts, summary.Changes
		for key, n := range summary.Counts {
			if status == "" || key == status {
				out.Total += n
			}
		}
	} else if keyword == "" {
		counts, err := r.tmdbRecheckCounts(ctx, status)
		if err != nil {
			return out, err
		}
		for _, n := range counts {
			out.Total += n
		}
	}
	if status != "" {
		q = q.Where("jobs.status=?", status)
	}
	q = q.
		Joins("LEFT JOIN metadata_items mi ON mi.id=jobs.metadata_id").
		Joins("LEFT JOIN metadata_items season ON season.id=mi.parent_id AND season.kind='season' AND mi.kind='episode'").
		Joins("LEFT JOIN metadata_items series ON series.id=CASE WHEN mi.kind='season' THEN mi.parent_id ELSE season.parent_id END AND series.kind='series'").
		Select(`jobs.*, mi.title, mi.kind, series.title AS series_title,
CASE WHEN mi.kind='season' THEN mi.season_num ELSE season.season_num END AS season_num, mi.episode_num`)
	const displayOrder = "LOWER(COALESCE(NULLIF(series_title,''), title,'')), season_num, episode_num, metadata_id"
	if keyword != "" {
		pattern := "%" + strings.ToLower(EscapeLike(keyword)) + "%"
		q = q.Where(`LOWER(COALESCE(mi.title,'')) LIKE ? ESCAPE '\' OR LOWER(COALESCE(series.title,'')) LIKE ? ESCAPE '\' OR LOWER(jobs.metadata_id) LIKE ? ESCAPE '\' OR LOWER(CONCAT('s',LPAD(CAST(CASE WHEN mi.kind='season' THEN mi.season_num ELSE season.season_num END AS text),2,'0'),'e',LPAD(CAST(mi.episode_num AS text),2,'0'))) LIKE ? ESCAPE '\'`, pattern, pattern, pattern, pattern)
		var rows []struct {
			TMDbRecheckRow
			SearchTotal int64
		}
		// 匹配只执行一次；空页也返回精确总数，避免回退到第二次联表搜索。
		err := r.db.WithContext(ctx).Raw(`WITH matched AS MATERIALIZED (?)
SELECT page.*, totals.search_total FROM (SELECT count(*) AS search_total FROM matched) totals
LEFT JOIN (SELECT * FROM matched ORDER BY `+displayOrder+` LIMIT ? OFFSET ?) page ON TRUE
ORDER BY `+displayOrder, q, size, (page-1)*size).Scan(&rows).Error
		for _, row := range rows {
			out.Total = row.SearchTotal
			if row.MetadataID != "" {
				out.Items = append(out.Items, row.TMDbRecheckRow)
			}
		}
		return out, err
	}
	err := r.db.WithContext(ctx).Table("(?) AS listed", q).
		Order(displayOrder).Offset((page - 1) * size).Limit(size).Scan(&out.Items).Error
	return out, err
}
