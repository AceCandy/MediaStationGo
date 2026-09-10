package service

import (
	"context"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// StorageService 汇总库内作品数量与已探测文件容量，不访问媒体文件。
type StorageService struct {
	repo *repository.Container
}

// NewStorageService is the constructor.
func NewStorageService(repo *repository.Container) *StorageService {
	return &StorageService{repo: repo}
}

// Breakdown is what /api/storage returns.
type Breakdown struct {
	TotalBytes int64          `json:"total_bytes"`
	ByLibrary  []LibraryUsage `json:"by_library"`
}

// LibraryUsage 的数量按本库关联的 metadata 去重，容量仍按文件累加。
type LibraryUsage struct {
	LibraryID    string `json:"library_id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Path         string `json:"path"`
	MovieCount   int64  `json:"movie_count"`
	SeriesCount  int64  `json:"series_count"`
	SeasonCount  int64  `json:"season_count"`
	EpisodeCount int64  `json:"episode_count"`
	TotalBytes   int64  `json:"total_bytes"`
}

// Compute returns the full breakdown.
func (s *StorageService) Compute(ctx context.Context) (*Breakdown, error) {
	out := &Breakdown{ByLibrary: []LibraryUsage{}}
	// 先按库去重 metadata，再将同季的集汇总；上级关联只处理季、剧集合。
	// 容量独立按文件累加，未匹配 metadata 的文件也计入，目录中的无文件条目不计数。
	err := s.repo.DB.WithContext(ctx).Raw(`
		WITH linked AS (
			SELECT DISTINCT library_id, metadata_id FROM media WHERE metadata_id IS NOT NULL
		), items AS MATERIALIZED (
			SELECT m.library_id, mi.kind,
				CASE WHEN mi.kind = 'episode' THEN mi.parent_id
					WHEN mi.kind IN ('season', 'series') THEN mi.id END AS id,
				COUNT(*) AS item_count
			FROM linked m JOIN metadata_items mi ON mi.id = m.metadata_id
			GROUP BY m.library_id, mi.kind,
				CASE WHEN mi.kind = 'episode' THEN mi.parent_id
					WHEN mi.kind IN ('season', 'series') THEN mi.id END
		), season_ids AS (
			SELECT DISTINCT library_id, id FROM items WHERE kind IN ('season', 'episode')
		), seasons AS MATERIALIZED (
			SELECT s.library_id, mi.id, mi.parent_id
			FROM season_ids s JOIN metadata_items mi ON mi.id = s.id AND mi.kind = 'season'
		), series_ids AS (
			SELECT library_id, id FROM items WHERE kind = 'series'
			UNION SELECT library_id, parent_id FROM seasons
		), counts AS (
			SELECT library_id,
				COALESCE(SUM(item_count) FILTER (WHERE kind = 'movie'), 0) AS movie_count,
				0::bigint AS series_count, 0::bigint AS season_count,
				COALESCE(SUM(item_count) FILTER (WHERE kind = 'episode'), 0) AS episode_count
			FROM items GROUP BY library_id
			UNION ALL SELECT library_id, 0, 0, COUNT(*), 0 FROM seasons GROUP BY library_id
			UNION ALL
			SELECT s.library_id, 0, COUNT(*), 0, 0
			FROM series_ids s JOIN metadata_items mi ON mi.id = s.id AND mi.kind = 'series'
			GROUP BY s.library_id
		), totals AS (
			SELECT library_id, SUM(movie_count) AS movie_count, SUM(series_count) AS series_count,
				SUM(season_count) AS season_count, SUM(episode_count) AS episode_count
			FROM counts GROUP BY library_id
		), capacity AS (
			SELECT m.library_id, SUM(pm.size_bytes) AS total_bytes
			FROM media_probe_metadata pm JOIN media m ON m.id = pm.media_id GROUP BY m.library_id
		)
		SELECT l.id AS library_id, l.name, l.type, l.path,
			COALESCE(c.movie_count, 0) AS movie_count, COALESCE(c.series_count, 0) AS series_count,
			COALESCE(c.season_count, 0) AS season_count, COALESCE(c.episode_count, 0) AS episode_count,
			COALESCE(p.total_bytes, 0) AS total_bytes
		FROM libraries l LEFT JOIN totals c ON c.library_id = l.id
		LEFT JOIN capacity p ON p.library_id = l.id
		WHERE l.deleted_at IS NULL ORDER BY l.created_at, l.id
	`).Scan(&out.ByLibrary).Error
	if err != nil {
		return nil, err
	}
	for _, library := range out.ByLibrary {
		out.TotalBytes += library.TotalBytes
	}
	return out, nil
}
