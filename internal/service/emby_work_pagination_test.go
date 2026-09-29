package service

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func TestEmbyMetadataWorkPageMatchesFileGrouping(t *testing.T) {
	e := newTestEmbyService(t)
	db := e.repo.DB
	for _, sql := range []string{
		`INSERT INTO metadata_items(id,kind,title,source,release_date,year,rating) SELECT 'movie-'||n,'movie','Title '||n,'local',CASE WHEN n%2=0 THEN '2020-01-01' ELSE '' END,2000+n,n FROM generate_series(1,5) n`,
		`INSERT INTO media(id,metadata_id,library_id,path,created_at) SELECT 'file-'||n||'-'||v,'movie-'||n,CASE WHEN v=3 THEN 'other' ELSE 'movies' END,'/movies/'||n||'/'||v,TIMESTAMP '2026-01-01'+n*INTERVAL '1 day'+v*INTERVAL '1 hour' FROM generate_series(1,4) n CROSS JOIN generate_series(1,3) v`,
		`INSERT INTO favorites(id,user_id,metadata_id,media_id) VALUES ('favorite','viewer','movie-1','file-1-1')`,
		`UPDATE metadata_items SET library_ids=NULL WHERE id='movie-2'`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	e.visibilityCache = map[string]embyVisibilityCacheEntry{e.repo.ReadCacheKey() + "viewer": {visibility: MediaVisibility{AllowedLibraryIDs: []string{"movies"}}, expiresAt: time.Now().Add(time.Hour)}}
	for _, parent := range []string{"", "movies", "missing"} {
		for _, favorite := range []bool{false, true} {
			for _, sortBy := range []string{"", "SortName", "DateCreated", "DateLastContentAdded", "CommunityRating", "PremiereDate"} {
				for _, direction := range []string{"Ascending", "Descending"} {
					for _, offset := range []int{0, 1, 9} {
						p := ItemsParams{UserID: "viewer", ParentID: parent, SortBy: sortBy, SortOrder: direction, StartIndex: offset, Limit: 2}
						q := e.applyUserMediaVisibility(t.Context(), db.Model(&model.Media{}), p.UserID)
						if parent != "" {
							q = q.Where("media.library_id=?", parent)
						}
						if favorite {
							q = q.Joins("JOIN favorites f ON f.metadata_id=media.metadata_id AND f.user_id=? AND f.deleted_at IS NULL", p.UserID)
						}
						want, n, err := e.metadataPage(t.Context(), q, p.UserID, metadataOrderSQL(p, false), offset, p.Limit)
						if err != nil {
							t.Fatal(err)
						}
						got, total, err := e.metadataWorkPage(t.Context(), q, p, false)
						if err != nil || total != n || !reflect.DeepEqual(got, want) {
							t.Fatalf("params=%+v favorite=%v total=%d/%d err=%v", p, favorite, total, n, err)
						}
					}
				}
			}
		}
	}
}

// 原文件级计数/分页仅作行为对照，不随作品级实现变化。
func (e *EmbyService) originalSeriesMetadataPageWithCount(ctx context.Context, q *gorm.DB, userID string, p ItemsParams, start, limit int, countTotal bool) ([]embySeriesGroup, int64, error) {
	// 在可见文件层物化，防止规划器先展开全量元数据、再逐集查询媒体文件。
	// 仅保留父季和入库时间；演员、收藏等整剧条件在关联父级之后应用。
	files := q.Session(&gorm.Session{}).Select("emby_metadata.parent_id, media.created_at")
	cte := "WITH scoped_media AS MATERIALIZED (?) "
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		// 收藏范围通常很小，允许先筛整剧再找文件，避免物化全部可见文件。
		cte = "WITH scoped_media AS NOT MATERIALIZED (?) "
	} else {
		// 每季仅保留最新文件时间，避免后续父级关联和排序展开全部文件版本。
		files = files.Select("emby_metadata.parent_id, MAX(media.created_at) AS created_at").Group("emby_metadata.parent_id")
	}
	if primarySupportedEmbySort(p.SortBy, false) == "datelastcontentadded" {
		// 仅物化文件所属季的存在性，避免空目录逐集探测；排序直接取作品时间。
		files = q.Session(&gorm.Session{}).Select("DISTINCT emby_metadata.parent_id")
	}
	pageScope := e.repo.DB.WithContext(ctx).Table("scoped_media AS media").
		Joins("JOIN metadata_items AS scope_season ON scope_season.id = media.parent_id AND scope_season.kind = 'season'").
		Joins("JOIN metadata_items AS scope_series ON scope_series.id = scope_season.parent_id AND scope_series.kind = 'series'")
	pageScope = e.applySeriesPageFilters(ctx, pageScope, p)
	var total int64

	type seriesIDRow struct {
		SeriesID string `gorm:"column:series_id"`
		Total    int64  `gorm:"column:total"`
	}
	var idRows []seriesIDRow
	idQuery := pageScope.Session(&gorm.Session{}).
		Select("scope_series.id AS series_id").
		Group("scope_series.id").
		Order(seriesOrderSQL(p)).
		Offset(start)
	if limit > 0 {
		idQuery = idQuery.Limit(limit)
	}
	query := e.repo.DB.WithContext(ctx).Raw(cte+"?", files, idQuery)
	if countTotal {
		grouped := pageScope.Session(&gorm.Session{}).
			Select("scope_series.id AS series_id, ROW_NUMBER() OVER (ORDER BY " + seriesOrderSQL(p) + ") AS ordinal").Group("scope_series.id")
		page := e.repo.DB.Table("scoped_series").Select("series_id, ordinal").Order("ordinal").Offset(start)
		if limit > 0 {
			page = page.Limit(limit)
		}
		// 左连接保留越界空页的总数；计数和分页共享一次文件扫描。
		query = e.repo.DB.WithContext(ctx).Raw(strings.TrimSpace(cte)+", scoped_series AS MATERIALIZED (?) SELECT totals.total, COALESCE(page.series_id, '') AS series_id FROM (SELECT COUNT(*) AS total FROM scoped_series) totals LEFT JOIN (?) page ON TRUE ORDER BY page.ordinal", files, grouped, page)
	}
	if err := query.Scan(&idRows).Error; err != nil {
		return nil, 0, err
	}
	seriesIDs := make([]string, 0, len(idRows))
	for _, row := range idRows {
		total = row.Total
		if id := strings.TrimSpace(row.SeriesID); id != "" {
			seriesIDs = append(seriesIDs, id)
		}
	}
	if len(seriesIDs) == 0 {
		return []embySeriesGroup{}, total, nil
	}

	summaryScope := e.applySeriesPageFilters(ctx, seriesScopeQuery(q.Session(&gorm.Session{})), p)
	groups, err := e.seriesSummaries(ctx, summaryScope, seriesIDs)
	return groups, total, err
}
