package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

// 扩展协议只挂版本化前缀，沿用播放器令牌，内容路由另行校验发现权限。
func registerEmbyExtensionRoutes(r *gin.Engine, secret string, svc *service.Container) {
	g := r.Group("/emby-ext/v1", middleware.MarkPlayerAPIRequest(), embyNoStoreHeaders(), embyAuthRequiredWithSessionFallback(secret), activeEmbyUserRequired(svc), embyTargetUserRequired())
	g.GET("/capabilities", func(c *gin.Context) {
		role, _ := c.Get(middleware.CtxUserRole)
		discover := role == "admin"
		if !discover {
			row, err := svc.Permissions.Effective(c.Request.Context(), c.GetString(middleware.CtxUserID))
			if err != nil {
				c.JSON(500, gin.H{"error": "无法读取账号权限"})
				return
			}
			discover = row != nil && row.PermissionMap()["can_view_discover"]
		}
		sources := []string{}
		if discover {
			sources = []string{"tmdb", "douban", "bangumi"}
		}
		c.JSON(200, gin.H{"version": "1", "rankings": discover, "danmaku": gin.H{"enabled": svc.HongGuoDanmu != nil, "format": "bilibili_xml"}, "calendar": gin.H{"enabled": discover && extProviderEnabled(c.Request.Context(), svc, "bangumi"), "source": "bangumi", "mode": "weekly", "timezone": "Asia/Tokyo", "exact_airtime": false}, "library_matching": gin.H{"sources": sources, "max_items": 100, "identity": "external_id_and_media_type", "scope": "metadata"}})
	})
	g.Use(requirePermission(svc, "can_view_discover"))
	g.GET("/rankings", func(c *gin.Context) {
		items := []gin.H{}
		for _, s := range discoverSectionCatalog {
			if s.Provider == "bangumi" {
				continue
			}
			items = append(items, gin.H{"id": s.Key, "name": s.Label, "source": s.Provider, "enabled": extProviderEnabled(c.Request.Context(), svc, s.Provider), "page_size": extPageSize(s.Provider)})
		}
		c.JSON(200, gin.H{"items": items})
	})
	g.GET("/rankings/:id/items", extRankingItems(svc))
	g.GET("/calendar", extCalendar(svc))
	g.POST("/library/matches", extLibraryMatches(svc))
	g.GET("/images/tmdb/:kind/:tmdbID/:type", extTMDbImage(svc))
	g.HEAD("/images/tmdb/:kind/:tmdbID/:type", extTMDbImage(svc))
}

func extProviderEnabled(ctx context.Context, svc *service.Container, source string) bool {
	if !discoverProviderEnabled(ctx, svc, source) {
		return false
	}
	switch source {
	case "tmdb":
		return svc.Discover.RankingsAvailable(ctx)
	case "douban":
		return svc.Douban != nil
	case "bangumi":
		return svc.Bangumi != nil
	}
	return false
}
func extPageSize(source string) int {
	if source == "douban" {
		return 24
	}
	return 20
}

func extRankingItems(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var section *discoverSectionDef
		for i := range discoverSectionCatalog {
			s := &discoverSectionCatalog[i]
			if s.Key == c.Param("id") && s.Provider != "bangumi" {
				section = s
				break
			}
		}
		if section == nil {
			c.JSON(404, gin.H{"error": "榜单不存在"})
			return
		}
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 || page > 500 {
			c.JSON(400, gin.H{"error": "page 必须为 1 至 500"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), discoverFeedSectionTimeout)
		defer cancel()
		if !extProviderEnabled(ctx, svc, section.Provider) {
			c.JSON(503, gin.H{"error": "榜单来源未启用或未配置"})
			return
		}
		items, err := discoverSectionItems(ctx, svc, section.Key, page)
		if err != nil {
			c.JSON(502, gin.H{"error": "榜单来源暂时不可用"})
			return
		}
		if items == nil {
			items = []service.ExternalMediaResult{}
		}
		for i := range items {
			items[i].PosterURL = extArtworkURL(items[i].PosterURL)
			items[i].BackdropURL = extArtworkURL(items[i].BackdropURL)
		}
		// 来源未提供总页数；不把过滤后的条目数冒充准确的下一页判断。
		c.JSON(200, gin.H{"id": section.Key, "source": section.Provider, "page": page, "page_size": extPageSize(section.Provider), "items": items, "total": nil, "has_more": nil})
	}
}

func extArtworkURL(raw string) string {
	const prefix = "/api/catalogs/tmdb/images/"
	if strings.HasPrefix(raw, prefix) {
		return "/emby-ext/v1/images/tmdb/" + strings.TrimPrefix(raw, prefix)
	}
	return raw
}
func extTMDbImage(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc.TMDbCenter == nil {
			c.Status(503)
			return
		}
		tmdbCenterImage(svc, false)(c)
	}
}

func extCalendar(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		weekday := 0
		if raw, ok := c.GetQuery("weekday"); ok {
			var err error
			weekday, err = strconv.Atoi(raw)
			if err != nil || weekday < 1 || weekday > 7 {
				c.JSON(400, gin.H{"error": "weekday 必须为 1 至 7"})
				return
			}
		}
		if !extProviderEnabled(c.Request.Context(), svc, "bangumi") {
			c.JSON(503, gin.H{"error": "日历来源未启用"})
			return
		}
		calendar, err := svc.Bangumi.WeeklyCalendar(c.Request.Context())
		if err != nil {
			c.JSON(502, gin.H{"error": "日历来源暂时不可用"})
			return
		}
		if weekday != 0 {
			items := []service.CalendarEntry{}
			for _, item := range calendar.Items {
				if item.Weekday == weekday {
					items = append(items, item)
				}
			}
			calendar.Items = items
		}
		c.JSON(200, gin.H{"source": "bangumi", "mode": "weekly", "timezone": "Asia/Tokyo", "items": calendar.Items, "updated_at": calendar.UpdatedAt, "stale": calendar.Stale})
	}
}

func extLibraryMatches(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
		var body struct {
			Items []repository.ExtensionIdentity `json:"items"`
		}
		raw, readErr := io.ReadAll(c.Request.Body)
		if readErr != nil || json.Unmarshal(raw, &body) != nil || len(body.Items) == 0 || len(body.Items) > 100 {
			c.JSON(400, gin.H{"error": "请提供 1 至 100 个作品"})
			return
		}
		seen := map[string]bool{}
		for i := range body.Items {
			item := &body.Items[i]
			if !item.Normalize() || seen[item.Key] {
				c.JSON(400, gin.H{"error": "作品身份或匹配键无效"})
				return
			}
			seen[item.Key] = true
		}
		visibility := mediaVisibilityForRequest(c, svc)
		matches := []repository.ExtensionLibraryMatch{}
		var err error
		if !visibility.LibraryRestricted || len(visibility.AllowedLibraryIDs) > 0 {
			matches, err = svc.Repo.MediaView.ExtensionLibraryMatches(c.Request.Context(), body.Items, repository.MediaQueryFilter{AllowedLibraryIDs: visibility.AllowedLibraryIDs, HiddenLibraryIDs: visibility.HiddenLibraryIDs})
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "读取媒体匹配失败"})
			return
		}
		indexed := map[string][]repository.ExtensionLibraryMatch{}
		for _, m := range matches {
			indexed[m.Key] = append(indexed[m.Key], m)
		}
		out := []gin.H{}
		for _, item := range body.Items {
			list := indexed[item.Key]
			if list == nil {
				list = []repository.ExtensionLibraryMatch{}
			}
			out = append(out, gin.H{"key": item.Key, "matches": list})
		}
		c.JSON(200, gin.H{"items": out})
	}
}
