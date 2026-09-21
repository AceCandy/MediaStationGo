package repository

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// Search runs a LIKE search against the title field. Empty query returns the
// most recently added items.
func (r *MediaRepository) Search(ctx context.Context, query string, limit int) ([]model.Media, error) {
	return r.SearchFiltered(ctx, query, limit, MediaQueryFilter{IncludeNSFW: true})
}

func (r *MediaRepository) SearchFiltered(ctx context.Context, query string, limit int, filter MediaQueryFilter) ([]model.Media, error) {
	items, _, err := r.SearchFilteredPage(ctx, query, 0, limit, filter)
	return items, err
}

func (r *MediaRepository) SearchFilteredPage(ctx context.Context, query string, offset, limit int, filter MediaQueryFilter) ([]model.Media, int64, error) {
	views, total, err := r.viewRepository().SearchFilteredPage(ctx, query, offset, limit, filter)
	if err != nil {
		return nil, 0, err
	}
	return mediaViewsToMedia(views), total, nil
}

func mediaViewsToMedia(views []model.MediaView) []model.Media {
	rows := make([]model.Media, 0, len(views))
	for _, view := range views {
		row := view.Media
		row.SeriesID = view.SeriesID
		row.Title = view.Title
		row.OriginalName = view.OriginalName
		row.PosterURL = view.PosterURL
		row.BackdropURL = view.BackdropURL
		row.Overview = view.Overview
		row.Rating = view.Rating
		row.Year = view.Year
		row.ReleaseDate = view.ReleaseDate
		row.SeasonNum = view.SeasonNum
		row.EpisodeNum = view.EpisodeNum
		row.TMDbID = view.TMDbID
		row.BangumiID = view.BangumiID
		row.DoubanID = view.DoubanID
		row.TheTVDBID = view.TheTVDBID
		row.Languages = view.Languages
		row.Countries = view.Countries
		row.Genres = view.Genres
		row.NSFW = view.NSFW
		rows = append(rows, row)
	}
	return rows
}

// MediaSearchTerms splits a query into normalized, case-insensitively unique terms.
func MediaSearchTerms(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || (r != '%' && r != '_' && r != '\\' && (unicode.IsPunct(r) || unicode.IsSymbol(r)))
	})
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		lower := strings.ToLower(field)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, field)
	}
	return out
}

// EscapeLike escapes PostgreSQL LIKE metacharacters for literal matching.
func EscapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func (r *MediaRepository) BackfillSearchIndex(ctx context.Context, batchLimit int) (int64, error) {
	return r.viewRepository().BackfillSearchIndex(ctx, batchLimit, 0)
}

const metadataPlayableExistsSQL = `(
	(search_metadata.kind = 'movie' AND EXISTS (
		SELECT 1 FROM media AS playable_media
		WHERE playable_media.metadata_id = search_metadata.id
	))
	OR
	(search_metadata.kind = 'series' AND EXISTS (
		SELECT 1
		FROM metadata_items AS playable_season
		JOIN metadata_items AS playable_episode
			ON playable_episode.parent_id = playable_season.id
			AND playable_episode.kind = 'episode'
		JOIN media AS playable_media
			ON playable_media.metadata_id = playable_episode.id
		WHERE playable_season.parent_id = search_metadata.id
			AND playable_season.kind = 'season'
	))
)`

const metadataPlayableInLibrariesSQL = `(
	(search_metadata.kind = 'movie' AND EXISTS (
		SELECT 1 FROM media AS playable_media
		WHERE playable_media.metadata_id = search_metadata.id
			AND playable_media.library_id = ANY(?)
	))
	OR
	(search_metadata.kind = 'series' AND EXISTS (
		SELECT 1
		FROM metadata_items AS playable_season
		JOIN metadata_items AS playable_episode
			ON playable_episode.parent_id = playable_season.id
			AND playable_episode.kind = 'episode'
		JOIN media AS playable_media
			ON playable_media.metadata_id = playable_episode.id
			AND playable_media.library_id = ANY(?)
		WHERE playable_season.parent_id = search_metadata.id
			AND playable_season.kind = 'season'
	))
)`

func (r *MediaViewRepository) SearchMetadataIDs(ctx context.Context, query string, offset, limit int, filter MetadataSearchFilter) ([]string, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query = strings.TrimSpace(query)
	terms := MediaSearchTerms(query)
	if query != "" && len(terms) == 0 {
		return []string{}, 0, nil
	}
	groups := buildMetadataSearchTermGroups(terms)
	prepared, err := r.prepareMetadataSearchFilter(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	if prepared.LibraryRestricted && len(prepared.VisibleLibraryIDs) == 0 {
		return []string{}, 0, nil
	}
	if query == "" {
		return r.searchMetadataIDsPostgres(ctx, query, groups, offset, limit, prepared)
	}
	complex := len(prepared.PersonIDs) > 0 || prepared.FavoriteUserID != "" || prepared.ResumableUserID != ""
	if r.searchBackend != nil && !prepared.ForcePostgres && !complex {
		if ids, _, searchErr := r.searchBackend.SearchMetadataIDs(ctx, query, 0, maxMetadataSearchCandidates, prepared); searchErr == nil {
			return r.rankMetadataSearchIDs(ctx, query, groups, ids, offset, limit, prepared)
		}
	}
	ids, _, err := r.searchMetadataIDsPostgres(ctx, query, groups, 0, maxMetadataSearchCandidates, prepared)
	if err != nil {
		return nil, 0, err
	}
	return r.rankMetadataSearchIDs(ctx, query, groups, ids, offset, limit, prepared)
}

func (r *MediaViewRepository) prepareMetadataSearchFilter(ctx context.Context, filter MetadataSearchFilter) (MetadataSearchFilter, error) {
	filter.Kinds = topLevelMetadataKinds(filter.Kinds)
	if filter.Fields == "" {
		filter.Fields = MetadataSearchFieldsWeb
	}
	if filter.LibraryRestricted {
		filter.VisibleLibraryIDs = uniqueNonEmptyStrings(filter.VisibleLibraryIDs)
		return filter, nil
	}
	hidden := stringSet(filter.HiddenLibraryIDs)
	allowed := uniqueNonEmptyStrings(filter.AllowedLibraryIDs)
	if len(allowed) > 0 {
		filter.LibraryRestricted = true
		for _, id := range allowed {
			if _, blocked := hidden[id]; !blocked {
				filter.VisibleLibraryIDs = append(filter.VisibleLibraryIDs, id)
			}
		}
		return filter, nil
	}
	if len(hidden) == 0 {
		return filter, nil
	}
	filter.LibraryRestricted = true
	q := r.db.WithContext(ctx).Table("media").Distinct("library_id")
	q = q.Where("library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	if err := q.Pluck("library_id", &filter.VisibleLibraryIDs).Error; err != nil {
		return filter, err
	}
	filter.VisibleLibraryIDs = uniqueNonEmptyStrings(filter.VisibleLibraryIDs)
	return filter, nil
}

func topLevelMetadataKinds(kinds []string) []string {
	if len(kinds) == 0 {
		return []string{model.MetadataKindMovie, model.MetadataKindSeries}
	}
	wanted := stringSet(kinds)
	out := make([]string, 0, 2)
	for _, kind := range []string{model.MetadataKindMovie, model.MetadataKindSeries} {
		if _, ok := wanted[kind]; ok {
			out = append(out, kind)
		}
	}
	return out
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func (r *MediaViewRepository) metadataSearchQuery(ctx context.Context, filter MetadataSearchFilter) *gorm.DB {
	q := r.db.WithContext(ctx).
		Table("metadata_items AS search_metadata").
		Where("search_metadata.kind IN ?", filter.Kinds).
		Where(metadataPlayableExistsSQL)
	if !filter.IncludeNSFW {
		q = q.Where("search_metadata.nsfw = FALSE")
	}
	if filter.LibraryRestricted {
		if len(filter.VisibleLibraryIDs) == 0 {
			return q.Where("FALSE")
		}
		q = q.Where(metadataPlayableInLibrariesSQL, &filter.VisibleLibraryIDs, &filter.VisibleLibraryIDs)
	}
	if len(filter.PersonIDs) > 0 {
		q = q.Where(`EXISTS (
			SELECT 1 FROM metadata_credits AS search_credit
			WHERE (search_credit.metadata_id = search_metadata.id OR search_credit.metadata_id IN (
				SELECT id FROM metadata_items WHERE kind = 'season' AND parent_id = search_metadata.id
			))
				AND search_credit.person_id = ANY(?)
		)`, &filter.PersonIDs)
	}
	if filter.FavoriteUserID != "" {
		q = q.Where(`EXISTS (
			SELECT 1 FROM favorites AS search_favorite
			WHERE search_favorite.metadata_id = search_metadata.id
				AND search_favorite.user_id = ?
				AND search_favorite.deleted_at IS NULL
		)`, filter.FavoriteUserID)
	}
	if filter.ResumableUserID != "" {
		stateFilter := filter.MediaQueryFilter
		if filter.LibraryRestricted {
			stateFilter.AllowedLibraryIDs = filter.VisibleLibraryIDs
		}
		q = q.Where(`EXISTS (
			SELECT 1 FROM (?) AS search_history
			WHERE search_history.position_ms > 0
				AND (
					search_history.metadata_id = search_metadata.id
					OR (search_metadata.kind = 'series' AND EXISTS (
						SELECT 1
						FROM metadata_items AS history_episode
						JOIN metadata_items AS history_season
							ON history_season.id = history_episode.parent_id
								AND history_season.kind = 'season'
							WHERE history_episode.id = search_history.metadata_id
								AND history_episode.kind = 'episode'
							AND history_season.parent_id = search_metadata.id
					))
				)
		)`, PlaybackStates(ctx, r.db, "legacy", filter.ResumableUserID, stateFilter))
	}
	return q
}

func applyMetadataSearchLIKEFilter(q *gorm.DB, groups []metadataSearchTermGroup, fields MetadataSearchFields) *gorm.DB {
	searchFields := []string{"search_metadata.title", "search_metadata.original_name"}
	if fields != MetadataSearchFieldsTitle {
		searchFields = append(searchFields, "search_metadata.overview", "search_metadata.genres")
	}
	for _, group := range groups {
		var (
			alternatives []string
			args         []any
		)
		for _, variant := range group.variants {
			for _, field := range searchFields {
				if group.numeric {
					numberRunes := "0-9"
					if isMetadataSearchChineseNumberRune([]rune(variant.value)[0]) {
						numberRunes = "零一二三四五六七八九十百"
					}
					alternatives = append(alternatives, "("+field+" ~ ?)")
					args = append(args, "(^|[^"+numberRunes+"])"+variant.value+"([^"+numberRunes+"]|$)")
					continue
				}
				predicates := make([]string, 0, len(variant.tokens))
				for _, token := range variant.tokens {
					predicates = append(predicates, field+" ILIKE ? ESCAPE '\\'")
					args = append(args, "%"+EscapeLike(token)+"%")
				}
				alternatives = append(alternatives, "("+strings.Join(predicates, " AND ")+")")
			}
		}
		q = q.Where("("+strings.Join(alternatives, " OR ")+")", args...)
	}
	return q
}

func (r *MediaViewRepository) searchMetadataIDsPostgres(ctx context.Context, query string, groups []metadataSearchTermGroup, offset, limit int, filter MetadataSearchFilter) ([]string, int64, error) {
	q := applyMetadataSearchLIKEFilter(r.metadataSearchQuery(ctx, filter), groups, filter.Fields)
	if query == "" {
		if has, err := (&NFORepository{db: r.db}).HasMedia(ctx); err != nil {
			return nil, 0, err
		} else if has {
			legacy := q.Select("search_metadata.id, search_metadata.created_at")
			local := r.nfoSearchQuery(ctx, filter).Select("'nfo-' || search_metadata.id AS id, search_metadata.created_at")
			combined := r.db.WithContext(ctx).Table("(?) AS search_metadata", r.db.Raw("? UNION ALL ?", legacy, local))
			var total int64
			if err := combined.Session(&gorm.Session{}).Count(&total).Error; err != nil {
				return nil, 0, err
			}
			var ids []string
			err := combined.Order("created_at DESC,id DESC").Offset(offset).Limit(limit).Pluck("id", &ids).Error
			return ids, total, err
		}
	}
	var total int64
	if query == "" {
		if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, 0, err
		}
	} else {
		offset, limit = 0, maxMetadataSearchCandidates
	}
	type metadataIDRow struct {
		ID string `gorm:"column:id"`
	}
	var rows []metadataIDRow
	idQuery := q.Session(&gorm.Session{}).Select("search_metadata.id AS id")
	if query != "" {
		prefix := EscapeLike(query) + "%"
		idQuery = idQuery.Order(gorm.Expr("CASE WHEN search_metadata.title = ? THEN 0 WHEN search_metadata.original_name = ? THEN 1 WHEN search_metadata.title LIKE ? ESCAPE '\\' THEN 2 WHEN search_metadata.original_name LIKE ? ESCAPE '\\' THEN 3 ELSE 4 END, search_metadata.created_at DESC, search_metadata.id DESC", query, query, prefix, prefix))
	} else {
		idQuery = idQuery.Order("search_metadata.created_at DESC, search_metadata.id DESC")
	}
	if err := idQuery.Offset(offset).Limit(limit).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if query != "" {
		total = int64(len(ids))
	}
	return ids, total, nil
}

// rankMetadataSearchIDs 从数据库复核候选字段，统一排序后才应用调用方分页。
func (r *MediaViewRepository) rankMetadataSearchIDs(ctx context.Context, query string, groups []metadataSearchTermGroup, ids []string, offset, limit int, filter MetadataSearchFilter) ([]string, int64, error) {
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) > maxMetadataSearchCandidates {
		ids = ids[:maxMetadataSearchCandidates]
	}
	var candidates []metadataSearchCandidate
	err := r.metadataSearchQuery(ctx, filter).
		Where("search_metadata.id IN ?", ids).
		Select("search_metadata.id, search_metadata.title, search_metadata.original_name, search_metadata.overview, search_metadata.genres, search_metadata.year").
		Scan(&candidates).Error
	if err != nil {
		return nil, 0, err
	}
	if has, err := (&NFORepository{db: r.db}).HasMedia(ctx); err != nil {
		return nil, 0, err
	} else if has {
		var local []metadataSearchCandidate
		q := applyMetadataSearchLIKEFilter(r.nfoSearchQuery(ctx, filter), groups, filter.Fields)
		if err := q.Select("'nfo-' || search_metadata.id AS id, search_metadata.title, search_metadata.original_name, search_metadata.overview, search_metadata.genres, search_metadata.year").Order("search_metadata.created_at DESC,search_metadata.id DESC").Limit(maxMetadataSearchCandidates).Scan(&local).Error; err != nil {
			return nil, 0, err
		}
		candidates = append(candidates, local...)
	}
	ranked := rankMetadataSearchCandidates(query, groups, candidates, filter.Fields)
	page, total := pageMetadataSearchCandidates(ranked, offset, limit)
	return page, total, nil
}

type metadataSearchPresentation struct {
	SeasonNum         int     `gorm:"column:season_num"`
	ID                string  `gorm:"column:id"`
	Kind              string  `gorm:"column:kind"`
	Title             string  `gorm:"column:title"`
	OriginalName      string  `gorm:"column:original_name"`
	Overview          string  `gorm:"column:overview"`
	Rating            float32 `gorm:"column:rating"`
	Year              int     `gorm:"column:year"`
	ReleaseDate       string  `gorm:"column:release_date"`
	Languages         string  `gorm:"column:languages"`
	Countries         string  `gorm:"column:countries"`
	Genres            string  `gorm:"column:genres"`
	NSFW              bool    `gorm:"column:nsfw"`
	Source            string  `gorm:"column:source"`
	TMDbExternalID    string  `gorm:"column:tmdb_external_id"`
	BangumiExternalID string  `gorm:"column:bangumi_external_id"`
	DoubanExternalID  string  `gorm:"column:douban_external_id"`
	TheTVDBExternalID string  `gorm:"column:thetvdb_external_id"`
	PosterAssetID     string  `gorm:"column:poster_asset_id"`
	BackdropAssetID   string  `gorm:"column:backdrop_asset_id"`
}

// FindSeriesPresentation 读取整剧自身的展示资料；调用方必须先验证关联文件可见性。
// 不要求存在分集文件，允许文件直接关联整剧或季。
func (r *MediaViewRepository) FindSeriesPresentation(ctx context.Context, metadataID string, includeNSFW bool) (*model.MediaView, error) {
	rows, err := r.FindSeriesPresentations(ctx, []string{metadataID}, includeNSFW)
	if err != nil {
		return nil, err
	}
	row, ok := rows[metadataID]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

// FindSeasonPresentation 读取季自身的资料与图片；调用方必须先验证所属文件可见性。
func (r *MediaViewRepository) FindSeasonPresentation(ctx context.Context, metadataID string, includeNSFW bool) (*model.MediaView, error) {
	if strings.HasPrefix(metadataID, "nfo-") {
		return r.NFOPresentation(ctx, metadataID, includeNSFW)
	}
	rows, err := r.metadataSearchPresentations(ctx, []string{metadataID})
	if err != nil {
		return nil, err
	}
	row, ok := rows[metadataID]
	if !ok || row.Kind != model.MetadataKindSeason || (row.NSFW && !includeNSFW) {
		return nil, nil
	}
	view := model.MediaView{Media: model.Media{PermanentBase: model.PermanentBase{ID: metadataID}}}
	applyMetadataSearchPresentation(&view, row)
	view.SeasonNum = row.SeasonNum
	return &view, nil
}

// FindSeriesPresentations 批量读取整剧展示字段；调用方只可传入已验证文件可见性的整剧 ID。
func (r *MediaViewRepository) FindSeriesPresentations(ctx context.Context, metadataIDs []string, includeNSFW bool) (map[string]model.MediaView, error) {
	out := make(map[string]model.MediaView)
	ordinaryIDs := make([]string, 0, len(metadataIDs))
	for _, id := range metadataIDs {
		if strings.HasPrefix(id, "nfo-") {
			view, err := r.NFOPresentation(ctx, id, includeNSFW)
			if err != nil {
				return nil, err
			}
			if view != nil {
				out[id] = *view
			}
		} else {
			ordinaryIDs = append(ordinaryIDs, id)
		}
	}
	metadataIDs = ordinaryIDs
	if len(metadataIDs) == 0 {
		return out, nil
	}
	rows, err := r.metadataSearchPresentations(ctx, metadataIDs)
	if err != nil {
		return nil, err
	}
	views := make([]model.MediaView, 0, len(rows))
	for id, row := range rows {
		if row.Kind != model.MetadataKindSeries || (row.NSFW && !includeNSFW) {
			continue
		}
		view := model.MediaView{Media: model.Media{PermanentBase: model.PermanentBase{ID: id}}}
		applyMetadataSearchPresentation(&view, row)
		views = append(views, view)
	}
	if err := attachMediaViewDoubanRatings(r.db.WithContext(ctx), views); err != nil {
		return nil, err
	}
	for _, view := range views {
		out[view.ID] = view
	}
	return out, nil
}

// FindMetadataSearchRepresentatives revalidates current visibility and returns
// one playable MediaView per top-level Metadata ID in the requested order.
func (r *MediaViewRepository) FindMetadataSearchRepresentatives(ctx context.Context, metadataIDs []string, filter MediaQueryFilter) ([]model.MediaView, error) {
	metadataIDs = uniqueNonEmptyStrings(metadataIDs)
	if len(metadataIDs) == 0 {
		return []model.MediaView{}, nil
	}
	localIDs, ordinaryIDs := []string{}, []string{}
	for _, id := range metadataIDs {
		if strings.HasPrefix(id, "nfo-") {
			localIDs = append(localIDs, id)
		} else {
			ordinaryIDs = append(ordinaryIDs, id)
		}
	}
	if len(localIDs) > 0 {
		ordinary, err := r.FindMetadataSearchRepresentatives(ctx, ordinaryIDs, filter)
		if err != nil {
			return nil, err
		}
		byID := map[string]model.MediaView{}
		for _, view := range ordinary {
			byID[view.MetadataID] = view
		}
		for _, id := range localIDs {
			files, err := r.NFOItemViews(ctx, id, filter)
			if err != nil {
				return nil, err
			}
			if len(files) == 0 {
				continue
			}
			view, err := r.NFOPresentation(ctx, id, filter.IncludeNSFW)
			if err != nil {
				return nil, err
			}
			if view == nil {
				continue
			}
			view.ID = files[0].ID
			view.LookupCatalogID = strings.TrimPrefix(id, "nfo-")
			if view.MetadataKind == model.MetadataKindSeries {
				view.SeriesID, view.SeriesTitle = id, view.Title
			}
			byID[id] = *view
		}
		result := make([]model.MediaView, 0, len(metadataIDs))
		for _, id := range metadataIDs {
			if view, ok := byID[id]; ok {
				result = append(result, view)
			}
		}
		return result, nil
	}
	topID := "CASE WHEN attached_metadata.kind = 'movie' THEN attached_metadata.id WHEN attached_metadata.kind = 'episode' THEN top_series.id ELSE NULL END"
	topNSFW := "CASE WHEN attached_metadata.kind = 'movie' THEN attached_metadata.nsfw WHEN attached_metadata.kind = 'episode' THEN top_series.nsfw ELSE TRUE END"
	base := r.db.WithContext(ctx).
		Table("media AS search_media").
		Joins("JOIN metadata_items AS attached_metadata ON attached_metadata.id = search_media.metadata_id").
		Joins("LEFT JOIN metadata_items AS top_season ON top_season.id = attached_metadata.parent_id AND attached_metadata.kind = 'episode' AND top_season.kind = 'season'").
		Joins("LEFT JOIN metadata_items AS top_series ON top_series.id = top_season.parent_id AND top_series.kind = 'series'").
		Where(topID+" IN ?", metadataIDs)
	if !filter.IncludeNSFW {
		base = base.Where("COALESCE(" + topNSFW + ", TRUE) = FALSE")
	}
	if len(filter.HiddenLibraryIDs) > 0 {
		base = base.Where("search_media.library_id <> ALL(?)", &filter.HiddenLibraryIDs)
	}
	if len(filter.AllowedLibraryIDs) > 0 {
		base = base.Where("search_media.library_id = ANY(?)", &filter.AllowedLibraryIDs)
	}
	type representativeRow struct {
		MetadataID string `gorm:"column:metadata_id"`
		MediaID    string `gorm:"column:media_id"`
	}
	var rankedRows []representativeRow
	ranked := base.Select(topID + " AS metadata_id, search_media.id AS media_id, ROW_NUMBER() OVER (PARTITION BY " + topID + " ORDER BY search_media.created_at DESC, search_media.id DESC) AS media_rank")
	if err := r.db.WithContext(ctx).Table("(?) AS ranked_search_media", ranked).
		Where("media_rank = 1").Scan(&rankedRows).Error; err != nil {
		return nil, err
	}
	mediaIDs := make([]string, 0, len(rankedRows))
	topByMediaID := make(map[string]string, len(rankedRows))
	for _, row := range rankedRows {
		mediaIDs = append(mediaIDs, row.MediaID)
		topByMediaID[row.MediaID] = row.MetadataID
	}
	views, err := r.FindByIDs(ctx, mediaIDs, filter)
	if err != nil {
		return nil, err
	}
	presentations, err := r.metadataSearchPresentations(ctx, metadataIDs)
	if err != nil {
		return nil, err
	}
	viewByMetadataID := make(map[string]model.MediaView, len(views))
	for _, view := range views {
		metadataID := topByMediaID[view.ID]
		presentation, ok := presentations[metadataID]
		if !ok {
			continue
		}
		applyMetadataSearchPresentation(&view, presentation)
		viewByMetadataID[metadataID] = view
	}
	out := make([]model.MediaView, 0, len(viewByMetadataID))
	for _, id := range metadataIDs {
		if view, ok := viewByMetadataID[id]; ok {
			out = append(out, view)
		}
	}
	return out, attachMediaViewDoubanRatings(r.db.WithContext(ctx), out)
}

func (r *MediaViewRepository) metadataSearchPresentations(ctx context.Context, metadataIDs []string) (map[string]metadataSearchPresentation, error) {
	var rows []metadataSearchPresentation
	err := r.db.WithContext(ctx).
		Table("metadata_items AS search_metadata").
		Select(`search_metadata.id, search_metadata.kind, search_metadata.title,
			COALESCE(search_metadata.season_num, 0) AS season_num,
			COALESCE(search_metadata.original_name, '') AS original_name,
			COALESCE(search_metadata.overview, '') AS overview,
			COALESCE(search_metadata.rating, 0) AS rating,
			COALESCE(search_metadata.year, 0) AS year,
			COALESCE(search_metadata.release_date, '') AS release_date,
			COALESCE(search_metadata.languages, '') AS languages,
			COALESCE(search_metadata.countries, '') AS countries,
			COALESCE(search_metadata.genres, '') AS genres,
			COALESCE(search_metadata.nsfw, FALSE) AS nsfw,
			COALESCE(search_metadata.source, '') AS source,
			COALESCE(search_identifiers.tmdb_external_id, '') AS tmdb_external_id,
			COALESCE(search_identifiers.bangumi_external_id, '') AS bangumi_external_id,
			COALESCE(search_identifiers.douban_external_id, '') AS douban_external_id,
			COALESCE(search_identifiers.thetvdb_external_id, '') AS thetvdb_external_id,
			COALESCE(search_poster.asset_id, '') AS poster_asset_id,
			COALESCE(search_backdrop.asset_id, '') AS backdrop_asset_id`).
		Joins(`LEFT JOIN LATERAL (
			SELECT
				MIN(CASE WHEN provider = 'tmdb' THEN external_id END) AS tmdb_external_id,
				MIN(CASE WHEN provider = 'bangumi' THEN external_id END) AS bangumi_external_id,
				MIN(CASE WHEN provider = 'douban' THEN external_id END) AS douban_external_id,
				MIN(CASE WHEN provider = 'thetvdb' THEN external_id END) AS thetvdb_external_id
				FROM metadata_identifiers
				WHERE metadata_id = search_metadata.id
					AND entity_kind = search_metadata.kind
			) AS search_identifiers ON TRUE`).
		Joins("LEFT JOIN metadata_artworks AS search_poster ON search_poster.metadata_id = search_metadata.id AND search_poster.artwork_type = 'poster'").
		Joins("LEFT JOIN metadata_artworks AS search_backdrop ON search_backdrop.metadata_id = search_metadata.id AND search_backdrop.artwork_type = 'backdrop'").
		Where("search_metadata.id IN ?", metadataIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]metadataSearchPresentation, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

func applyMetadataSearchPresentation(view *model.MediaView, row metadataSearchPresentation) {
	view.MetadataID = row.ID
	view.Title = row.Title
	view.OriginalName = row.OriginalName
	view.Overview = row.Overview
	view.Rating = row.Rating
	view.Year = row.Year
	view.ReleaseDate = row.ReleaseDate
	view.Languages = row.Languages
	view.Countries = row.Countries
	view.Genres = row.Genres
	view.NSFW = row.NSFW
	view.MetadataKind = row.Kind
	view.MetadataSource = row.Source
	view.TMDbExternalID = row.TMDbExternalID
	view.BangumiExternalID = row.BangumiExternalID
	view.DoubanID = row.DoubanExternalID
	view.TheTVDBID = row.TheTVDBExternalID
	view.PosterAssetID = row.PosterAssetID
	view.BackdropAssetID = row.BackdropAssetID
	if row.Kind == model.MetadataKindSeries {
		view.SeriesID = row.ID
		view.SeriesTitle = row.Title
	}
	view.Normalize()
}

func (r *MediaViewRepository) BackfillSearchIndex(ctx context.Context, batchLimit int, batchPause time.Duration) (total int64, err error) {
	return r.searchIndex.backfill(ctx, batchLimit, batchPause, r.metadataSearchDocumentIDs, r.metadataSearchDocuments)
}

func (r *searchIndex) backfill(ctx context.Context, batchLimit int, batchPause time.Duration,
	listIDs func(context.Context, string, int) ([]string, error),
	loadDocuments func(context.Context, []string) ([]MetadataSearchDocument, error),
) (total int64, err error) {
	backend, ok := r.searchBackend.(MediaSearchSyncBackend)
	if !ok {
		return 0, nil
	}
	if batchLimit <= 0 {
		batchLimit = 1000
	}
	r.searchMu.Lock()
	if r.searchRebuild {
		r.searchMu.Unlock()
		return 0, nil
	}
	r.searchRebuild = true
	r.searchDirty = map[string]struct{}{}
	r.searchMu.Unlock()

	index, err := backend.PrepareMetadataIndex(ctx)
	if err != nil {
		r.finishSearchRebuild()
		return 0, err
	}
	activated := false
	defer func() {
		if !activated {
			_ = backend.DiscardMetadataIndex(context.Background(), index)
			r.finishSearchRebuild()
		}
	}()

	var lastID string
	for {
		ids, listErr := listIDs(ctx, lastID, batchLimit)
		if listErr != nil {
			return total, listErr
		}
		if len(ids) == 0 {
			break
		}
		documents, documentErr := loadDocuments(ctx, ids)
		if documentErr != nil {
			return total, documentErr
		}
		if err := backend.IndexMetadata(ctx, index, documents); err != nil {
			return 0, err
		}
		total += int64(len(documents))
		lastID = ids[len(ids)-1]
		if len(ids) < batchLimit {
			break
		}
		if batchPause > 0 {
			timer := time.NewTimer(batchPause)
			select {
			case <-ctx.Done():
				timer.Stop()
				return total, ctx.Err()
			case <-timer.C:
			}
		}
	}

	r.searchMu.Lock()
	dirty := make([]string, 0, len(r.searchDirty))
	for id := range r.searchDirty {
		dirty = append(dirty, id)
	}
	sort.Strings(dirty)
	documents, err := loadDocuments(ctx, dirty)
	if err == nil {
		byID := make(map[string]MetadataSearchDocument, len(documents))
		for _, document := range documents {
			byID[document.ID] = document
		}
		for _, id := range dirty {
			if document, exists := byID[id]; exists {
				err = backend.IndexMetadata(ctx, index, []MetadataSearchDocument{document})
			} else {
				err = backend.DeleteMetadataFromIndex(ctx, index, id)
			}
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		err = backend.ActivateMetadataIndex(ctx, index)
	}
	if err != nil {
		r.searchMu.Unlock()
		return total, err
	}
	r.searchRebuild = false
	r.searchDirty = nil
	r.searchFailed.Store(false)
	r.searchMu.Unlock()
	activated = true
	return total, nil
}

func (r *searchIndex) finishSearchRebuild() {
	r.searchMu.Lock()
	r.searchRebuild = false
	r.searchDirty = nil
	r.searchMu.Unlock()
}

func (r *MediaViewRepository) metadataSearchDocumentIDs(ctx context.Context, afterID string, limit int) ([]string, error) {
	// 这里只分页候选，文档组装会按本批 ID 过滤无文件作品，避免每页重算全库可播放关系。
	q := r.db.WithContext(ctx).Table("metadata_items AS search_metadata").
		Where("search_metadata.kind IN ?", []string{model.MetadataKindMovie, model.MetadataKindSeries})
	if afterID != "" {
		q = q.Where("search_metadata.id > ?", afterID)
	}
	var ids []string
	err := q.Order("search_metadata.id ASC").Limit(limit).Pluck("search_metadata.id", &ids).Error
	return ids, err
}

func (r *MediaViewRepository) metadataSearchDocuments(ctx context.Context, metadataIDs []string) ([]MetadataSearchDocument, error) {
	metadataIDs = uniqueNonEmptyStrings(metadataIDs)
	if len(metadataIDs) == 0 {
		return []MetadataSearchDocument{}, nil
	}
	var items []model.MetadataItem
	if err := r.db.WithContext(ctx).
		Where("id IN ? AND kind IN ?", metadataIDs, []string{model.MetadataKindMovie, model.MetadataKindSeries}).
		Find(&items).Error; err != nil {
		return nil, err
	}
	type libraryRow struct {
		MetadataID string `gorm:"column:metadata_id"`
		LibraryID  string `gorm:"column:library_id"`
	}
	var libraries []libraryRow
	// 分集很多而实际文件稀疏时，逐集探测 media 索引比扫描一次更贵。
	// 有界计数保留小批增量更新的索引点查，避免每次更新都扫描全部媒体。
	// ponytail: 8192 为当前数据规模的切换阈值；媒体规模显著增长时需重新对比执行计划。
	var episodeCount int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM (
		SELECT 1 FROM metadata_items AS series_metadata
		JOIN metadata_items AS season_metadata ON season_metadata.parent_id = series_metadata.id AND season_metadata.kind = 'season'
		JOIN metadata_items AS episode_metadata ON episode_metadata.parent_id = season_metadata.id AND episode_metadata.kind = 'episode'
		WHERE series_metadata.id IN ? AND series_metadata.kind = 'series'
		LIMIT 8193
	) AS candidate_episodes`, metadataIDs).Scan(&episodeCount).Error; err != nil {
		return nil, err
	}
	mediaSource := "media"
	if episodeCount > 8192 {
		mediaSource = "(SELECT metadata_id, library_id FROM media OFFSET 0)"
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT movie_metadata.id AS metadata_id, movie_media.library_id
		FROM metadata_items AS movie_metadata
		JOIN media AS movie_media ON movie_media.metadata_id = movie_metadata.id
			WHERE movie_metadata.id IN ? AND movie_metadata.kind = 'movie'
		UNION
		SELECT series_metadata.id AS metadata_id, episode_media.library_id
		FROM metadata_items AS series_metadata
		JOIN metadata_items AS season_metadata ON season_metadata.parent_id = series_metadata.id AND season_metadata.kind = 'season'
		JOIN metadata_items AS episode_metadata ON episode_metadata.parent_id = season_metadata.id AND episode_metadata.kind = 'episode'
		JOIN `+mediaSource+` AS episode_media ON episode_media.metadata_id = episode_metadata.id
			WHERE series_metadata.id IN ? AND series_metadata.kind = 'series'
	`, metadataIDs, metadataIDs).Scan(&libraries).Error
	if err != nil {
		return nil, err
	}
	librariesByMetadataID := make(map[string][]string, len(items))
	for _, row := range libraries {
		librariesByMetadataID[row.MetadataID] = append(librariesByMetadataID[row.MetadataID], row.LibraryID)
	}
	itemByID := make(map[string]model.MetadataItem, len(items))
	for _, item := range items {
		itemByID[item.ID] = item
	}
	documents := make([]MetadataSearchDocument, 0, len(items))
	for _, id := range metadataIDs {
		item, exists := itemByID[id]
		libraryIDs := uniqueNonEmptyStrings(librariesByMetadataID[id])
		if !exists || len(libraryIDs) == 0 {
			continue
		}
		sort.Strings(libraryIDs)
		documents = append(documents, MetadataSearchDocument{
			ID: item.ID, Kind: item.Kind, Title: item.Title, OriginalName: item.OriginalName,
			Overview: item.Overview, Genres: item.Genres, NSFW: item.NSFW, LibraryIDs: libraryIDs,
		})
	}
	return documents, nil
}

// RefreshMetadataIDs recomputes affected top-level documents. Unknown or no
// longer eligible IDs are deleted from the active alias.
func (r *MediaViewRepository) RefreshMetadataIDs(ctx context.Context, metadataIDs ...string) {
	backend, ok := r.searchBackend.(MediaSearchSyncBackend)
	if !ok || len(metadataIDs) == 0 {
		return
	}
	topIDs, err := r.topMetadataIDsForMetadataIDs(ctx, metadataIDs)
	if err != nil {
		return
	}
	candidates := uniqueNonEmptyStrings(append(append([]string{}, metadataIDs...), topIDs...))
	r.searchMu.Lock()
	if r.searchRebuild {
		for _, id := range candidates {
			r.searchDirty[id] = struct{}{}
		}
	}
	r.searchMu.Unlock()
	documents, err := r.metadataSearchDocuments(ctx, candidates)
	if err != nil {
		return
	}
	byID := make(map[string]MetadataSearchDocument, len(documents))
	for _, document := range documents {
		byID[document.ID] = document
	}
	for _, id := range candidates {
		if document, exists := byID[id]; exists {
			_ = backend.UpsertMetadata(ctx, document)
		} else {
			_ = backend.DeleteMetadata(ctx, id)
		}
	}
}

func (r *searchIndex) refresh(ctx context.Context, candidates []string, loadDocuments func(context.Context, []string) ([]MetadataSearchDocument, error)) {
	backend, ok := r.searchBackend.(MediaSearchSyncBackend)
	if !ok || len(candidates) == 0 {
		return
	}
	// ponytail: 红果增量按索引串行以免旧快照覆盖新文档；吞吐不足时再引入文档版本控制。
	r.searchMu.Lock()
	defer r.searchMu.Unlock()
	if r.searchRebuild {
		for _, id := range candidates {
			r.searchDirty[id] = struct{}{}
		}
	}
	documents, err := loadDocuments(ctx, candidates)
	if err != nil {
		r.searchFailed.Store(true)
		return
	}
	byID := make(map[string]MetadataSearchDocument, len(documents))
	for _, document := range documents {
		byID[document.ID] = document
	}
	for _, id := range candidates {
		if document, exists := byID[id]; exists {
			err = backend.UpsertMetadata(ctx, document)
		} else {
			err = backend.DeleteMetadata(ctx, id)
		}
		if err != nil {
			r.searchFailed.Store(true)
		}
	}
}

func (r *MediaViewRepository) topMetadataIDsForMetadataIDs(ctx context.Context, metadataIDs []string) ([]string, error) {
	metadataIDs = uniqueNonEmptyStrings(metadataIDs)
	if len(metadataIDs) == 0 {
		return nil, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).
		Table("metadata_items AS changed_metadata").
		Select(`DISTINCT CASE
			WHEN changed_metadata.kind IN ('movie', 'series') THEN changed_metadata.id
			WHEN changed_metadata.kind = 'season' THEN changed_parent.id
			WHEN changed_metadata.kind = 'episode' THEN changed_series.id
		END`).
		Joins("LEFT JOIN metadata_items AS changed_parent ON changed_parent.id = changed_metadata.parent_id").
		Joins("LEFT JOIN metadata_items AS changed_series ON changed_series.id = changed_parent.parent_id").
		Where("changed_metadata.id IN ?", metadataIDs).
		Pluck(`DISTINCT CASE
			WHEN changed_metadata.kind IN ('movie', 'series') THEN changed_metadata.id
			WHEN changed_metadata.kind = 'season' THEN changed_parent.id
			WHEN changed_metadata.kind = 'episode' THEN changed_series.id
		END`, &ids).Error
	return uniqueNonEmptyStrings(ids), err
}
