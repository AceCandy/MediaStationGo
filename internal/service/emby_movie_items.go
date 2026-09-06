package service

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// movieLibraryHasEpisodicContent 报告电影类型库里是否混入了「剧集结构」内容
// (有季集号且路径形如剧集,例如 .../国产剧/某剧/Season 01/某剧 - S01E01.mkv)。
// 用于决定是否需要走 movieLibraryItems 把这些内容聚成 Series 卡片。普通电影库
// 没有这类行时返回 false,继续走常规 mediaItems。
func (e *EmbyService) movieLibraryHasEpisodicContent(ctx context.Context, libraryID string) (bool, error) {
	clause, args := embyLikelyEpisodicPathSQL()
	if clause == "" {
		return false, nil
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("library_id IN ?", e.mergedLibraryIDs(ctx, libraryID)).
		Where("(season_num > 0 OR episode_num > 0) AND ("+clause+")", args...)
	var count int64
	if err := q.Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// movieLibraryItems 处理电影类型库的常规浏览,返回「真正的电影(Movie)」与
// 「库内剧集结构内容聚成的 Series 卡片」的合并列表(按 DateCreated 倒序分页)。
// 与 mediaItems 的区别: 后者会把剧集结构行当散装 Episode 漏出;这里改为聚合成
// Series,从根本上消除「电影库里整部剧被拆成单集」的现象。
func (e *EmbyService) movieLibraryItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	libIDs := e.mergedLibraryIDs(ctx, p.ParentID)
	apply := func(q *gorm.DB) *gorm.DB {
		q = e.applyUserMediaVisibility(ctx, q, p.UserID)
		q = q.Where("media.library_id IN ?", libIDs)
		if containsEmbyFilter(p.Filters, "IsFavorite") {
			if strings.TrimSpace(p.UserID) == "" {
				return nil
			}
			q = q.Joins("LEFT JOIN metadata_items AS favorite_season ON favorite_season.id = emby_metadata.parent_id AND emby_metadata.kind = 'episode' AND favorite_season.kind = 'season'").
				Joins("JOIN favorites ON favorites.user_id = ? AND favorites.deleted_at IS NULL AND favorites.metadata_id = CASE WHEN emby_metadata.kind = 'episode' THEN favorite_season.parent_id ELSE media.metadata_id END", p.UserID)
		}
		return q
	}

	// 先合并两类作品的 ID 与排序字段，统一分页后才加载展示信息。
	clause, args := embyLikelyEpisodicPathSQL()
	movieQ := apply(e.repo.DB.WithContext(ctx).Model(&model.Media{}))
	if movieQ == nil {
		return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0, "StartIndex": p.StartIndex}, nil
	}
	movieQ = filterLikelyEpisodicPathsFromMovieQuery(movieQ)
	epQ := seriesScopeQuery(apply(e.repo.DB.WithContext(ctx).Model(&model.Media{})))
	if clause == "" {
		epQ = epQ.Where("1 = 0")
	} else {
		epQ = epQ.Where("(media.season_num > 0 OR media.episode_num > 0) AND ("+clause+")", args...)
	}
	query := func() *gorm.DB {
		movies := movieQ.Session(&gorm.Session{}).Select("media.metadata_id AS id, 'movie' AS kind, " + embyReleaseOrderSQL("emby_metadata") + " AS sort_at").Group("media.metadata_id")
		series := epQ.Session(&gorm.Session{}).Select("scope_series.id AS id, 'series' AS kind, " + embyReleaseOrderSQL("emby_metadata") + " AS sort_at").Group("scope_series.id")
		return e.repo.DB.WithContext(ctx).Table("(? UNION ALL ?) AS works", movies, series)
	}
	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, err
	}
	var page []struct {
		ID   string
		Kind string
	}
	if err := query().Select("id, kind").Order("sort_at DESC, kind DESC, id DESC").Offset(p.StartIndex).Limit(p.Limit).Scan(&page).Error; err != nil {
		return nil, err
	}
	movieIDs, seriesIDs := []string{}, []string{}
	for _, row := range page {
		if row.Kind == "series" {
			seriesIDs = append(seriesIDs, row.ID)
		} else {
			movieIDs = append(movieIDs, row.ID)
		}
	}
	byID := make(map[string]map[string]any, len(page))
	if len(movieIDs) > 0 {
		views, _, err := e.metadataPage(ctx, movieQ.Where("media.metadata_id IN ?", movieIDs), p.UserID, metadataOrderSQL(p, false), 0, len(movieIDs))
		if err != nil {
			return nil, err
		}
		for _, item := range e.payloadsForViewsWithFields(ctx, views, p.UserID, p.Fields) {
			byID[item["Id"].(string)] = item
		}
	}
	groups, err := e.seriesSummaries(ctx, epQ, seriesIDs)
	if err != nil {
		return nil, err
	}
	for _, item := range e.seriesPayloadsWithFields(ctx, groups, p.UserID, p.Fields) {
		byID[item["Id"].(string)] = item
	}
	items := make([]map[string]any, 0, len(page))
	for _, row := range page {
		if item, ok := byID[row.ID]; ok {
			item["ParentId"] = p.ParentID
			items = append(items, item)
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": int(total), "StartIndex": p.StartIndex}, nil
}

// embyReleaseOrderSQL 保留上映日期、年份和入库时间的降级顺序，仅由内部别名组成。
func embyReleaseOrderSQL(metadataAlias string) string {
	return "MAX(CASE WHEN COALESCE(" + metadataAlias + ".release_date, '') <> '' THEN " + metadataAlias + ".release_date WHEN COALESCE(" + metadataAlias + ".year, 0) > 0 THEN LPAD(" + metadataAlias + ".year::text, 4, '0') || '-12-31' ELSE to_char(media.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US') END)"
}

func (e *EmbyService) libraryIsEpisodic(ctx context.Context, libraryID string) (bool, error) {
	if strings.TrimSpace(libraryID) == "" {
		return false, nil
	}
	if lib, err := e.repo.Library.FindByID(ctx, libraryID); err != nil {
		return false, err
	} else if lib != nil {
		return embyLibraryTypeIsEpisodic(lib.Type), nil
	}
	var count int64
	err := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("library_id IN ? AND (season_num > 0 OR episode_num > 0)", e.mergedLibraryIDs(ctx, libraryID)).
		Count(&count).Error
	return count > 0, err
}

func (e *EmbyService) mediaBelongsToEpisodicLibrary(ctx context.Context, m *model.Media) bool {
	if e == nil || m == nil || strings.TrimSpace(m.LibraryID) == "" {
		return false
	}
	lib, err := e.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil || lib == nil {
		return false
	}
	return embyLibraryTypeIsEpisodic(lib.Type)
}

func (e *EmbyService) mediaShouldBeEpisode(ctx context.Context, m *model.Media) bool {
	if m == nil || (m.SeasonNum <= 0 && m.EpisodeNum <= 0) {
		return false
	}
	if e.mediaBelongsToEpisodicLibrary(ctx, m) {
		return true
	}
	return embyMediaPathLooksEpisodic(m.Path)
}

func embyLibraryTypeIsEpisodic(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "tv", "anime", "variety", "show", "shows", model.LibraryTypeNFOTV:
		return true
	default:
		return false
	}
}

func (e *EmbyService) filterMovieItems(ctx context.Context, q *gorm.DB) *gorm.DB {
	episodicIDs := e.episodicLibraryIDs(ctx)
	if len(episodicIDs) == 0 {
		return filterLikelyEpisodicPathsFromMovieQuery(q)
	}
	q = q.Where("(media.season_num = 0 AND media.episode_num = 0) OR media.library_id <> ALL(?)", &episodicIDs)
	return filterLikelyEpisodicPathsFromMovieQuery(q)
}

func (e *EmbyService) filterEpisodeItems(ctx context.Context, q *gorm.DB) *gorm.DB {
	episodicIDs := e.episodicLibraryIDs(ctx)
	if len(episodicIDs) == 0 {
		return q.Where("1 = 0")
	}
	return q.Where("media.library_id = ANY(?) AND (media.season_num > 0 OR media.episode_num > 0)", &episodicIDs)
}

func (e *EmbyService) episodicLibraryIDs(ctx context.Context) []string {
	if e == nil || e.repo == nil || e.repo.DB == nil {
		return nil
	}
	var ids []string
	if err := e.repo.DB.WithContext(ctx).Model(&model.Library{}).
		Where("LOWER(type) IN ?", []string{"tv", "anime", "variety"}).
		Pluck("id", &ids).Error; err != nil {
		return nil
	}
	return ids
}

func filterLikelyEpisodicPathsFromMovieQuery(q *gorm.DB) *gorm.DB {
	clause, args := embyLikelyEpisodicPathSQL()
	if clause == "" {
		return q
	}
	return q.Where("NOT ((media.season_num > 0 OR media.episode_num > 0) AND ("+clause+"))", args...)
}

func embyLikelyEpisodicPathSQL() (string, []any) {
	patterns := []string{
		"%/season %/%", "%/season.%/%", "%/season-%/%", "%/season_%/%",
		"%/s0%/%", "%/s1%/%", "%/s2%/%", "%/s3%/%", "%/s4%/%", "%/s5%/%", "%/s6%/%", "%/s7%/%", "%/s8%/%", "%/s9%/%",
		"%/special/%", "%/specials/%", "%/sp/%", "%/ova/%", "%/oad/%", "%/extra/%", "%/extras/%",
		"%/电视剧/%", "%/剧集/%", "%/连续剧/%", "%/短剧/%", "%/国产剧/%", "%/国剧/%", "%/大陆剧/%", "%/华语剧/%", "%/国产电视剧/%", "%/大陆电视剧/%", "%/华语电视剧/%", "%/欧美剧/%", "%/欧美电视剧/%", "%/美剧/%", "%/英剧/%", "%/日韩剧/%", "%/日韩电视剧/%", "%/日剧/%", "%/韩剧/%", "%/港剧/%", "%/台剧/%", "%/港台剧/%", "%/泰剧/%",
		"%/日番/%", "%/国漫/%", "%/番剧/%", "%/动漫/%", "%/特别篇/%", "%/特別篇/%", "%/番外/%", "%/特典/%",
	}
	clauses := make([]string, 0, len(patterns)*2)
	args := make([]any, 0, len(patterns)*2)
	for _, pattern := range patterns {
		clauses = append(clauses, "LOWER(media.path) LIKE ?")
		args = append(args, pattern)
		if strings.Contains(pattern, "/") {
			clauses = append(clauses, "LOWER(media.path) LIKE ?")
			args = append(args, strings.ReplaceAll(pattern, "/", `\`))
		}
	}
	return strings.Join(clauses, " OR "), args
}

func embyMediaPathLooksEpisodic(path string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
	if normalized == "" {
		return false
	}
	for _, marker := range []string{
		"/season ", "/season.", "/season-", "/season_", "/special/", "/specials/", "/sp/", "/ova/", "/oad/", "/extra/", "/extras/",
		"/电视剧/", "/剧集/", "/连续剧/", "/短剧/", "/国产剧/", "/国剧/", "/大陆剧/", "/华语剧/", "/国产电视剧/", "/大陆电视剧/", "/华语电视剧/", "/欧美剧/", "/欧美电视剧/", "/美剧/", "/英剧/", "/日韩剧/", "/日韩电视剧/", "/日剧/", "/韩剧/", "/港剧/", "/台剧/", "/港台剧/", "/泰剧/",
		"/日番/", "/国漫/", "/番剧/", "/动漫/", "/特别篇/", "/特別篇/", "/番外/", "/特典/",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	for _, marker := range []string{"/s0", "/s1", "/s2", "/s3", "/s4", "/s5", "/s6", "/s7", "/s8", "/s9"} {
		if idx := strings.Index(normalized, marker); idx >= 0 {
			after := idx + len(marker)
			if after < len(normalized) && normalized[after] >= '0' && normalized[after] <= '9' {
				slash := after + 1
				if slash < len(normalized) && normalized[slash] == '/' {
					return true
				}
			}
		}
	}
	return false
}
