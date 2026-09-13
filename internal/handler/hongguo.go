package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var hongGuoWorkCategories = map[string]map[string]bool{
	"real-drama": {
		"爱情": true, "年代": true, "逆袭": true, "传奇": true, "成长": true, "家庭": true, "家族": true, "萌宝": true,
		"悬疑": true, "惊悚": true, "恐怖": true, "志怪": true, "古装": true, "玄幻": true, "奇幻": true, "都市": true,
		"青春": true, "喜剧": true, "科幻": true, "灾难": true, "动作冒险": true, "战争": true, "综艺": true, "剧情": true,
	},
	"comic-drama": {"脑洞": true, "玄幻": true, "剧情": true, "末世": true, "豪门": true, "奇幻": true, "科幻": true, "冒险": true},
	"ai-drama":    {"脑洞": true, "玄幻": true, "剧情": true, "末世": true, "豪门": true, "奇幻": true, "科幻": true, "冒险": true},
}

func registerHongGuoRoutes(authed *gin.RouterGroup, svc *service.Container) {
	group := authed.Group("/catalogs/hongguo")
	group.Use(func(c *gin.Context) {
		if svc.HongGuo == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		c.Next()
	})
	group.GET("/works", requirePermission(svc, "can_view_discover"), func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		size, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "50"))
		category := strings.TrimSpace(c.Query("category"))
		sourceCategory := strings.TrimSpace(c.Query("source_category"))
		rank := strings.TrimSpace(c.Query("rank"))
		if err != nil || sizeErr != nil || page < 1 || page > 1000000 || size < 1 || size > 100 || c.Query("sort") != "" || (sourceCategory != "" && !hongguo.ValidCategory(sourceCategory)) || (category != "" && !hongGuoWorkCategories[sourceCategory][category]) || (rank != "" && !hongguo.ValidRank(rank)) || (rank != "" && (sourceCategory != "" || category != "")) {
			c.JSON(400, gin.H{"error": "查询参数无效"})
			return
		}
		rows, total, err := svc.Repo.HongGuo.List(c.Request.Context(), strings.TrimSpace(c.Query("keyword")), sourceCategory, category, rank, page, size)
		if err != nil {
			c.JSON(500, gin.H{"error": "红果资料读取失败"})
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total, "page": page, "page_size": size})
	})
	group.GET("/libraries/:libraryID", func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 {
			c.Status(400)
			return
		}
		library, err := svc.Repo.Library.FindByID(c.Request.Context(), c.Param("libraryID"))
		if err != nil {
			c.Status(500)
			return
		}
		if library == nil || library.Type != model.LibraryTypeHongGuo || !mediaVisibilityForRequest(c, svc).Allows(&model.Media{LibraryID: library.ID}) {
			c.Status(404)
			return
		}
		items, total, err := svc.Repo.MediaView.HongGuoLibraryCards(c.Request.Context(), library.ID, page)
		if err != nil {
			c.JSON(500, gin.H{"error": "红果媒体库读取失败"})
			return
		}
		c.JSON(200, gin.H{"items": items, "total": total})
	})
	group.GET("/works/:sourceID", requirePermission(svc, "can_view_discover"), func(c *gin.Context) {
		if !hongguo.ValidID(c.Param("sourceID")) {
			c.JSON(400, gin.H{"error": "红果作品 ID 无效"})
			return
		}
		detail, err := svc.Repo.HongGuo.Detail(c.Request.Context(), c.Param("sourceID"))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(404)
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "红果详情读取失败"})
			return
		}
		c.JSON(200, detail)
	})
	group.GET("/works/:sourceID/media", func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 || !hongguo.ValidID(c.Param("sourceID")) {
			c.Status(400)
			return
		}
		visibility := mediaVisibilityForRequest(c, svc)
		if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
			c.JSON(200, gin.H{"items": []model.MediaView{}, "total": 0})
			return
		}
		rows, total, err := svc.Repo.MediaView.HongGuoMediaPage(c.Request.Context(), c.Param("sourceID"), page, 50, repository.MediaQueryFilter{IncludeNSFW: visibility.IncludeNSFW, AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		if err != nil {
			c.JSON(500, gin.H{"error": "红果媒体读取失败"})
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total})
	})
	group.GET("/works/:sourceID/episodes", requirePermission(svc, "can_view_discover"), func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 || !hongguo.ValidID(c.Param("sourceID")) {
			c.Status(400)
			return
		}
		items, total, err := svc.Repo.HongGuo.Episodes(c.Request.Context(), c.Param("sourceID"), page, 100)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(404)
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "红果分集读取失败"})
			return
		}
		c.JSON(200, gin.H{"items": items, "total": total, "page": page, "page_size": 100})
	})
	group.POST("/works/:sourceID/refresh", middleware.AdminRequired(), func(c *gin.Context) {
		if !hongguo.ValidID(c.Param("sourceID")) {
			c.JSON(400, gin.H{"error": "红果作品 ID 无效"})
			return
		}
		err := svc.HongGuo.Run(c.Request.Context(), service.TaskKindHongGuoRefresh, c.Param("sourceID"))
		if errors.Is(err, service.ErrHongGuoRunning) {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, service.ErrHongGuoDisabled) {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		if err != nil {
			c.JSON(502, gin.H{"error": "红果资料刷新失败，请查看任务日志"})
			return
		}
		c.Status(204)
	})
	group.GET("/artwork/:id", func(c *gin.Context) {
		if err := svc.HongGuo.ServeArtwork(c.Request.Context(), c.Writer, c.Request, c.Param("id")); err != nil {
			c.Status(404)
		}
	})
	group.GET("/status", func(c *gin.Context) {
		enabled, err := svc.HongGuo.Enabled(c.Request.Context())
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"enabled": enabled})
	})
	group.GET("/me", func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		tab := c.DefaultQuery("tab", "favourites")
		if err != nil || page < 1 || page > 1000000 || (tab != "favourites" && tab != "history" && tab != "continue") {
			c.Status(400)
			return
		}
		visibility := mediaVisibilityForRequest(c, svc)
		if visibility.LibraryRestricted && len(visibility.AllowedLibraryIDs) == 0 {
			c.JSON(200, gin.H{"items": []repository.HongGuoUserCard{}, "total": 0})
			return
		}
		items, total, err := svc.Repo.HongGuo.UserCards(c.Request.Context(), currentUserID(c), tab, page, 50, repository.MediaQueryFilter{AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		if err != nil {
			c.JSON(500, gin.H{"error": "红果用户记录读取失败"})
			return
		}
		c.JSON(200, gin.H{"items": items, "total": total})
	})
	group.PUT("/media/:mediaID/played", func(c *gin.Context) {
		var request struct {
			Played *bool `json:"played"`
		}
		if c.ShouldBindJSON(&request) != nil || request.Played == nil {
			c.Status(400)
			return
		}
		view, err := svc.Repo.MediaView.FindByID(c.Request.Context(), c.Param("mediaID"))
		if err != nil {
			c.Status(500)
			return
		}
		if view == nil || view.CatalogSource != model.TaskSystemHongGuo || !mediaViewVisibleForRequest(c, svc, view) {
			c.Status(404)
			return
		}
		if err := svc.Repo.HongGuo.MarkPlayed(c.Request.Context(), currentUserID(c), *view, *request.Played); err != nil {
			c.JSON(500, gin.H{"error": "红果观看状态更新失败"})
			return
		}
		c.Status(204)
	})
	group.GET("/works/:sourceID/favorite", func(c *gin.Context) {
		if !hongguo.ValidID(c.Param("sourceID")) {
			c.Status(400)
			return
		}
		state, err := svc.Repo.HongGuo.UserState(c.Request.Context(), currentUserID(c), c.Param("sourceID"), 0)
		if err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"favorite": state.Favorite})
	})
	group.PUT("/works/:sourceID/favorite", func(c *gin.Context) {
		var request struct {
			Favorite *bool `json:"favorite"`
		}
		if !hongguo.ValidID(c.Param("sourceID")) || c.ShouldBindJSON(&request) != nil || request.Favorite == nil {
			c.Status(400)
			return
		}
		if _, err := svc.Repo.HongGuo.FindBySourceID(c.Request.Context(), c.Param("sourceID")); errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(404)
			return
		} else if err != nil {
			c.Status(500)
			return
		}
		if err := svc.Repo.HongGuo.SetFavorite(c.Request.Context(), currentUserID(c), c.Param("sourceID"), *request.Favorite); err != nil {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	group.PUT("/status", middleware.AdminRequired(), func(c *gin.Context) {
		var request struct {
			Enabled *bool `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
			c.JSON(400, gin.H{"error": "enabled 必填"})
			return
		}
		if err := svc.HongGuo.SetEnabled(c.Request.Context(), *request.Enabled); err != nil {
			c.Status(500)
			return
		}
		c.Status(204)
	})
	group.POST("/cancel", middleware.AdminRequired(), func(c *gin.Context) { svc.HongGuo.Cancel(); c.Status(204) })
	group.GET("/pending", middleware.AdminRequired(), func(c *gin.Context) {
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 1000000 {
			c.Status(400)
			return
		}
		items, total, err := svc.Repo.HongGuo.PendingMedia(c.Request.Context(), page)
		if err != nil {
			c.JSON(500, gin.H{"error": "红果待匹配文件读取失败"})
			return
		}
		c.JSON(200, gin.H{"items": items, "total": total})
	})
	group.GET("/groups/:id", requirePermission(svc, "can_view_discover"), func(c *gin.Context) {
		if _, err := uuid.Parse(c.Param("id")); err != nil {
			c.Status(400)
			return
		}
		detail, err := svc.Repo.HongGuo.Group(c.Request.Context(), c.Param("id"))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Status(404)
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "聚合读取失败"})
			return
		}
		c.JSON(200, detail)
	})
	saveGroup := func(c *gin.Context) {
		id := c.Param("id")
		if id != "" {
			if _, err := uuid.Parse(id); err != nil {
				c.Status(400)
				return
			}
		}
		var request struct {
			Title   string                         `json:"title"`
			Members []repository.HongGuoGroupInput `json:"members"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(400, gin.H{"error": "聚合参数无效"})
			return
		}
		group, err := svc.Repo.HongGuo.SaveGroup(c.Request.Context(), id, request.Title, request.Members)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "聚合或源作品不存在，请先导入资料"})
			return
		}
		if err != nil {
			c.JSON(409, gin.H{"error": "聚合未保存，请检查季号重复、电影成员或作品已在其他聚合"})
			return
		}
		c.JSON(200, group)
	}
	group.POST("/groups", middleware.AdminRequired(), saveGroup)
	group.PUT("/groups/:id", middleware.AdminRequired(), saveGroup)
	group.DELETE("/groups/:id", middleware.AdminRequired(), func(c *gin.Context) {
		if _, err := uuid.Parse(c.Param("id")); err != nil {
			c.Status(400)
			return
		}
		if err := svc.Repo.HongGuo.DeleteGroup(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(500, gin.H{"error": "解除聚合失败"})
			return
		}
		c.Status(204)
	})
}
