package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/huangguoai"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func registerHuangGuoAIRoutes(authed *gin.RouterGroup, svc *service.Container) {
	registerHuangGuoAIDownloadRoutes(authed, svc)
	group := authed.Group("/catalogs/huangguoai")
	group.Use(func(c *gin.Context) {
		if svc.HuangGuoAI == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		// Source metadata and posters follow the same adult-content/profile lock as its files.
		if !mediaVisibilityForRequest(c, svc).IncludeNSFW {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	})
	group.GET("/works", requirePermission(svc, "can_view_discover"), func(c *gin.Context) {
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		size, se := strconv.Atoi(c.DefaultQuery("page_size", "24"))
		category, rank, tag := c.Query("category"), c.Query("rank"), strings.TrimSpace(c.Query("tag"))
		keyword := strings.TrimSpace(c.Query("keyword"))
		if e != nil || se != nil || page < 1 || page > 1000000 || size < 1 || size > 100 || (category != "" && !huangguoai.ValidCategory(category)) || (rank != "" && !huangguoai.ValidRank(rank)) || len(tag) > 200 || len(keyword) > 200 {
			c.Status(400)
			return
		}
		items, total, err := svc.Repo.HuangGuoAI.List(c.Request.Context(), keyword, category, tag, rank, page, size)
		if err != nil {
			c.JSON(500, gin.H{"error": "黄果 AI 资料读取失败"})
			return
		}
		c.JSON(200, gin.H{"items": items, "total": total, "page": page, "page_size": size})
	})
	group.GET("/search", requirePermission(svc, "can_view_discover"), func(c *gin.Context) {
		keyword := strings.TrimSpace(c.Query("keyword"))
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		if e != nil || page < 1 || page > huangguoai.MaxPage || keyword == "" || len(keyword) > 200 {
			c.Status(400)
			return
		}
		items, err := svc.HuangGuoAI.Search(c.Request.Context(), keyword, page)
		if errors.Is(err, service.ErrHuangGuoAIDisabled) {
			c.Status(409)
			return
		}
		if err != nil {
			c.JSON(502, gin.H{"error": "官网搜索失败，请稍后重试"})
			return
		}
		results, err := svc.Repo.HuangGuoAI.SearchResults(c.Request.Context(), items)
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"items": results, "total": len(results), "page": page, "has_more": len(results) == 24})
	})
	group.GET("/works/:sourceID", requirePermission(svc, "can_view_discover"), func(c *gin.Context) {
		if !huangguoai.ValidID(c.Param("sourceID")) {
			c.Status(400)
			return
		}
		item, err := svc.Repo.HuangGuoAI.Detail(c.Request.Context(), c.Param("sourceID"))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(404)
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "黄果 AI 详情读取失败"})
			return
		}
		c.JSON(200, item)
	})
	group.GET("/works/:sourceID/episodes", requirePermission(svc, "can_view_discover"), func(c *gin.Context) {
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		if e != nil || page < 1 || page > 1000000 || !huangguoai.ValidID(c.Param("sourceID")) {
			c.Status(400)
			return
		}
		items, total, err := svc.Repo.HuangGuoAI.Episodes(c.Request.Context(), c.Param("sourceID"), page, 100)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(404)
			return
		}
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"items": items, "total": total, "page": page, "page_size": 100})
	})
	group.POST("/works/:sourceID/refresh", middleware.AdminRequired(), func(c *gin.Context) {
		if !requireTasksReady(c, svc) {
			return
		}
		if !huangguoai.ValidID(c.Param("sourceID")) {
			c.Status(400)
			return
		}
		err := svc.HuangGuoAI.Run(c.Request.Context(), service.TaskKindHuangGuoAIRefresh, c.Param("sourceID"))
		if errors.Is(err, service.ErrHuangGuoAIRunning) || errors.Is(err, service.ErrHuangGuoAIDisabled) {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		if err != nil {
			c.JSON(502, gin.H{"error": "黄果 AI 资料刷新失败，请查看任务日志"})
			return
		}
		c.Status(204)
	})
	group.GET("/artwork/:id", func(c *gin.Context) {
		if !canServeHuangGuoAIArtwork(c, svc) {
			c.Status(404)
			return
		}
		if err := svc.HuangGuoAI.ServeArtwork(c.Request.Context(), c.Writer, c.Request, c.Param("id")); err != nil {
			c.Status(404)
		}
	})
	group.GET("/works/:sourceID/media", func(c *gin.Context) {
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		if e != nil || page < 1 || page > 1000000 || !huangguoai.ValidID(c.Param("sourceID")) {
			c.Status(400)
			return
		}
		visibility := mediaVisibilityForRequest(c, svc)
		if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
			c.JSON(200, gin.H{"items": []model.MediaView{}, "total": 0})
			return
		}
		rows, total, err := svc.Repo.MediaView.HuangGuoAIMediaPage(c.Request.Context(), c.Param("sourceID"), page, 50, repository.MediaQueryFilter{AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total, "page": page})
	})
	group.GET("/me", func(c *gin.Context) {
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		tab := c.DefaultQuery("tab", "favourites")
		if e != nil || page < 1 || page > 1000000 || (tab != "favourites" && tab != "history" && tab != "continue") {
			c.Status(400)
			return
		}
		visibility := mediaVisibilityForRequest(c, svc)
		if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
			c.JSON(200, gin.H{"items": []repository.HuangGuoAIUserCard{}, "total": 0})
			return
		}
		rows, total, err := svc.Repo.HuangGuoAI.UserCards(c.Request.Context(), currentUserID(c), tab, page, 50, repository.MediaQueryFilter{AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total})
	})
	group.GET("/works/:sourceID/state", func(c *gin.Context) {
		id := c.Param("sourceID")
		episode, e := strconv.Atoi(c.DefaultQuery("episode", "1"))
		if e != nil || episode < 1 || episode > 100000 || !huangguoai.ValidID(id) {
			c.Status(400)
			return
		}
		visibility := mediaVisibilityForRequest(c, svc)
		if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
			c.Status(404)
			return
		}
		filter := repository.MediaQueryFilter{AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs}
		rows, _, err := svc.Repo.MediaView.HuangGuoAIMediaPage(c.Request.Context(), id, 1, 1, filter)
		if err != nil {
			c.Status(500)
			return
		}
		if len(rows) == 0 {
			c.Status(404)
			return
		}
		state, err := svc.Repo.HuangGuoAI.UserState(c.Request.Context(), currentUserID(c), id, episode, filter)
		if err != nil {
			c.Status(500)
			return
		}
		favorite, err := svc.Repo.HuangGuoAI.Favorite(c.Request.Context(), currentUserID(c), id, nil)
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"state": state, "favorite": favorite})
	})
	group.PUT("/works/:sourceID/favorite", func(c *gin.Context) {
		id := c.Param("sourceID")
		var body struct {
			Favorite *bool `json:"favorite"`
		}
		if !huangguoai.ValidID(id) || c.ShouldBindJSON(&body) != nil || body.Favorite == nil {
			c.Status(400)
			return
		}
		visibility := mediaVisibilityForRequest(c, svc)
		if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
			c.Status(404)
			return
		}
		rows, _, err := svc.Repo.MediaView.HuangGuoAIMediaPage(c.Request.Context(), id, 1, 1, repository.MediaQueryFilter{AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		if err != nil {
			c.Status(500)
			return
		}
		if len(rows) == 0 {
			c.Status(404)
			return
		}
		if _, err = svc.Repo.HuangGuoAI.Favorite(c.Request.Context(), currentUserID(c), id, body.Favorite); err != nil {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	group.PUT("/media/:mediaID/played", func(c *gin.Context) {
		var body struct {
			Played *bool `json:"played"`
		}
		if c.ShouldBindJSON(&body) != nil || body.Played == nil {
			c.Status(400)
			return
		}
		view, err := svc.Repo.MediaView.FindByID(c.Request.Context(), c.Param("mediaID"))
		if err != nil {
			c.Status(500)
			return
		}
		if view == nil || view.CatalogSource != model.TaskSystemHuangGuoAI || !mediaViewVisibleForRequest(c, svc, view) {
			c.Status(404)
			return
		}
		if err = svc.Repo.HuangGuoAI.MarkPlayed(c.Request.Context(), currentUserID(c), *view, *body.Played); err != nil {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	group.GET("/status", func(c *gin.Context) {
		enabled, err := svc.HuangGuoAI.Enabled(c.Request.Context())
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"enabled": enabled})
	})
	group.PUT("/status", middleware.AdminRequired(), func(c *gin.Context) {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if c.ShouldBindJSON(&body) != nil || body.Enabled == nil {
			c.Status(400)
			return
		}
		if err := svc.HuangGuoAI.SetEnabled(c.Request.Context(), *body.Enabled); err != nil {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	group.POST("/cancel", middleware.AdminRequired(), func(c *gin.Context) { svc.HuangGuoAI.Cancel(); c.Status(204) })
}

// canServeHuangGuoAIArtwork 发现权限或该作品的可见文件权限满足其一即可。
func canServeHuangGuoAIArtwork(c *gin.Context, svc *service.Container) bool {
	role, _ := c.Get(middleware.CtxUserRole)
	permission, err := svc.Permissions.Effective(c.Request.Context(), currentUserID(c))
	if role == "admin" || (err == nil && permission != nil && permission.PermissionMap()["can_view_discover"]) {
		return true
	}
	row, err := svc.Repo.HuangGuoAI.Artwork(c.Request.Context(), c.Param("id"))
	if err != nil {
		return false
	}
	v := mediaVisibilityForRequest(c, svc)
	if !v.IncludeNSFW || (v.LibraryRestricted && len(v.AllowedLibraryIDs) == 0) {
		return false
	}
	var id string
	q := svc.Repo.DB.WithContext(c.Request.Context()).Table("media m").Joins("JOIN huangguoai_media_bindings b ON b.media_id=m.id").Joins("JOIN huangguoai_works w ON w.id=b.work_id AND w.projection_error=''").Joins("JOIN huangguoai_episodes ep ON ep.id=b.episode_id AND ep.work_id=w.id").Where("b.work_id=? AND m.catalog_source='huangguoai' AND (w.kind='series' OR ep.number=1)", row.WorkID)
	if len(v.AllowedLibraryIDs) > 0 {
		q = q.Where("m.library_id=ANY(?)", &v.AllowedLibraryIDs)
	}
	if len(v.HiddenLibraryIDs) > 0 {
		q = q.Where("m.library_id<>ALL(?)", &v.HiddenLibraryIDs)
	}
	return q.Select("m.id").Limit(1).Scan(&id).Error == nil && id != ""
}
