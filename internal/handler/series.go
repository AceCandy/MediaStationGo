// Package handler — TV series endpoints.
//
// These return episode lists grouped by season number for a library that
// holds TV episodes. Series rows are distinct from Movies — the front
// end uses /api/libraries/:id/seasons to render a season selector.
package handler

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// seasonGroup is the JSON returned to the React UI per season.
type seasonGroup struct {
	Season   int               `json:"season"`
	Episodes []model.MediaView `json:"episodes"`
}

func listSeasonsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libID := c.Param("id")
		if lib, err := svc.Repo.Library.FindByID(c.Request.Context(), libID); err == nil && lib != nil {
			if !service.LibraryVisibleForUser(c.Request.Context(), svc.Repo, *lib, mediaVisibilityForRequest(c, svc)) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
		}
		visibility := mediaVisibilityForRequest(c, svc)
		var rows []model.MediaView
		const pageSize = 2000
		for page := 1; ; page++ {
			pageRows, total, err := svc.Media.ListMediaVisible(c.Request.Context(), libID, page, pageSize, visibility)
			if err != nil && err != gorm.ErrRecordNotFound {
				writeInternalOrCanceled(c, err)
				return
			}
			rows = append(rows, pageRows...)
			if int64(len(rows)) >= total || len(pageRows) < pageSize {
				break
			}
		}
		buckets := make(map[int][]model.MediaView)
		for _, r := range rows {
			if !visibility.AllowsView(&r) {
				continue
			}
			buckets[r.SeasonNum] = append(buckets[r.SeasonNum], r)
		}
		out := make([]seasonGroup, 0, len(buckets))
		for s, items := range buckets {
			out = append(out, seasonGroup{Season: s, Episodes: items})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Season < out[j].Season })
		c.JSON(http.StatusOK, gin.H{"seasons": out})
	}
}

func listLibrarySeriesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libID := c.Param("id")
		if lib, err := svc.Repo.Library.FindByID(c.Request.Context(), libID); err == nil && lib != nil {
			if !service.LibraryVisibleForUser(c.Request.Context(), svc.Repo, *lib, mediaVisibilityForRequest(c, svc)) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
		}
		visibility := mediaVisibilityForRequest(c, svc)
		visibility.MissingPoster = c.Query("missing_poster") == "1"
		visibility.MissingChineseTitle = c.Query("missing_chinese_title") == "1"
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		size, _ := strconv.Atoi(c.DefaultQuery("page_size", "500"))
		if page < 1 {
			page = 1
		}
		if size <= 0 || size > 1000 {
			size = 500
		}
		items, total, err := svc.Media.ListLibrarySeriesCards(c.Request.Context(), libID, page, size, c.Query("series_id"), c.Query("key"), visibility)
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		if items == nil {
			// 非 nil 空切片，避免空库返回 "items": null 触发前端崩溃。
			items = []service.SeriesCard{}
		}
		c.JSON(http.StatusOK, gin.H{
			"items":     items,
			"total":     total,
			"page":      page,
			"page_size": size,
		})
	}
}

func listLibrarySeriesEpisodesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libID := c.Param("id")
		key := c.Query("key")
		if key == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
			return
		}
		if lib, err := svc.Repo.Library.FindByID(c.Request.Context(), libID); err == nil && lib != nil {
			if !service.LibraryVisibleForUser(c.Request.Context(), svc.Repo, *lib, mediaVisibilityForRequest(c, svc)) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
		}
		var season *int
		if raw, ok := c.GetQuery("season"); ok {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "season must be a non-negative integer"})
				return
			}
			season = &value
		}
		items, err := svc.Media.ListLibrarySeriesEpisodes(c.Request.Context(), libID, key, season, mediaVisibilityForRequest(c, svc))
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		ids := make([]string, 0, len(items))
		for _, item := range items {
			if item.CatalogSource == "nfo" {
				ids = append(ids, "nfo-"+item.LookupCatalogID)
			} else {
				ids = append(ids, item.MetadataID)
			}
		}
		uid, _ := c.Get(middleware.CtxUserID)
		visibility := mediaVisibilityForRequest(c, svc)
		history, err := svc.Repo.History.ListByUserMetadataIDs(c.Request.Context(), toString(uid), ids, repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		var resume *service.HistoryItem
		if season != nil && len(items) > 0 {
			resume, err = svc.Playback.ContinueSeriesHistory(c.Request.Context(), toString(uid), libID, items[0].SeriesID, visibility)
			if err != nil {
				writeInternalOrCanceled(c, err)
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items), "history": history, "resume": resume})
	}
}
