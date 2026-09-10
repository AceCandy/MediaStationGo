package service

import (
	"context"
	"github.com/ShukeBta/MediaStationGo/internal/model"
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
	// 仅沿文件关联向上解析季、剧，避免把目录中尚无文件的子项计入收藏。
	err := s.repo.DB.WithContext(ctx).Model(&model.Library{}).
		Joins("LEFT JOIN media AS m ON m.library_id = libraries.id").
		Joins("LEFT JOIN media_probe_metadata AS pm ON pm.media_id = m.id").
		Joins("LEFT JOIN metadata_items AS mi ON mi.id = m.metadata_id").
		Joins("LEFT JOIN metadata_items AS season ON season.id = CASE WHEN mi.kind = 'season' THEN mi.id WHEN mi.kind = 'episode' THEN mi.parent_id END AND season.kind = 'season'").
		Joins("LEFT JOIN metadata_items AS series ON series.id = CASE WHEN mi.kind = 'series' THEN mi.id ELSE season.parent_id END AND series.kind = 'series'").
		Select(`libraries.id AS library_id, libraries.name, libraries.type, libraries.path,
			COUNT(DISTINCT CASE WHEN mi.kind = 'movie' THEN mi.id END) AS movie_count,
			COUNT(DISTINCT series.id) AS series_count,
			COUNT(DISTINCT season.id) AS season_count,
			COUNT(DISTINCT CASE WHEN mi.kind = 'episode' THEN mi.id END) AS episode_count,
			COALESCE(SUM(pm.size_bytes), 0) AS total_bytes`).
		Group("libraries.id").Order("libraries.created_at ASC, libraries.id ASC").
		Scan(&out.ByLibrary).Error
	if err != nil {
		return nil, err
	}
	for _, library := range out.ByLibrary {
		out.TotalBytes += library.TotalBytes
	}
	return out, nil
}
