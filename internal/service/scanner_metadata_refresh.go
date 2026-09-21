package service

import (
	"context"
	"errors"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func mediaPathTitleYear(mediaPath string) (string, int) {
	displayPath := strings.TrimSpace(mediaPath)
	displayPath = strings.Trim(strings.ReplaceAll(displayPath, "\\", "/"), "/")
	if displayPath == "" {
		return "", 0
	}
	parts := strings.Split(displayPath, "/")
	if len(parts) < 2 {
		return "", 0
	}
	dirs := parts[:len(parts)-1]
	if len(dirs) == 0 {
		return "", 0
	}
	base := strings.TrimSpace(dirs[len(dirs)-1])
	usedSeasonFolder := false
	if _, ok := seasonFromDir(base); ok {
		usedSeasonFolder = true
		dirs = dirs[:len(dirs)-1]
		if len(dirs) == 0 {
			return "", 0
		}
		base = strings.TrimSpace(dirs[len(dirs)-1])
	}
	if base == "" || (!usedSeasonFolder && len(dirs) < 2) {
		return "", 0
	}
	title, year := CleanQuery(base)
	if title == "" {
		title = base
	}
	return strings.TrimSpace(title), year
}

// refreshLocalMetadataHints 仅刷新普通库侧车提示，保留已确认的资料绑定及文件探测指纹。
func (s *ScannerService) refreshLocalMetadataHints(ctx context.Context, libraryID, path string) (bool, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil || !lib.Enabled || libraryUsesNFOOnly(lib) || lib.Type == model.LibraryTypeHongGuo {
		return false, err
	}
	root, err := s.localLibraryRootForPath(ctx, lib, path)
	if err != nil {
		return false, err
	}
	season, episode := scanEpisodeNumbers(lib, path)
	seriesLike := librarySupportsSeasons(lib) || season > 0 || episode > 0
	local, err := ReadLocalMetadata(path, root.Path, seriesLike)
	if err != nil {
		return false, err
	}
	_, hints := pathHintMetadata(path, seriesLike)
	local = hints.applyToLocalMetadata(local)
	encoded := encodeLocalMetadataHint(local)
	changed := false
	err = s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var media model.Media
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("library_id = ? AND path = ? AND COALESCE(catalog_source, '') = ''", libraryID, path).Limit(1).Find(&media).Error; err != nil {
			return err
		}
		if media.ID == "" || media.LocalMetadataHint == encoded {
			return nil
		}
		if media.ScrapeStatus == "running" {
			return errors.New("媒体正在刮削，稍后重读本地资料")
		}
		updates := map[string]any{"local_metadata_hint": encoded}
		// 已确认身份不随侧车事件改绑；未匹配文件可使用更正后的提示重试。
		if media.MetadataID == "" {
			media.TMDbID, media.BangumiID, media.DoubanID, media.TheTVDBID = 0, 0, "", ""
			media.SeasonNum, media.EpisodeNum = season, episode
			if local != nil {
				applyLocalScanHints(&media, local)
			}
			if libraryIsMovieType(lib) {
				media.SeasonNum, media.EpisodeNum = 0, 0
			}
			updates["lookup_tmdb_id"], updates["lookup_bangumi_id"] = media.TMDbID, media.BangumiID
			updates["lookup_douban_id"], updates["lookup_thetvdb_id"] = media.DoubanID, media.TheTVDBID
			updates["season_num"], updates["episode_num"] = media.SeasonNum, media.EpisodeNum
			updates["scrape_status"], updates["scrape_error"] = "pending", ""
		}
		if err := tx.Model(&media).Updates(updates).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed && err == nil, err
}
