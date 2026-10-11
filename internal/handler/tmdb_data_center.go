package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func tmdbCenterError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrTMDbQueueLimit):
		Error(c, 429, ErrInvalidParams, "待补全请求已达上限，请稍后重试")
	case errors.Is(err, service.ErrTMDbCenterInput):
		Error(c, 400, ErrInvalidParams, "资料参数无效")
	case service.IsTMDbCenterMissing(err):
		Error(c, 404, ErrNotFound, "来源资料尚未登记")
	default:
		Error(c, 503, ErrExternal, "来源资料暂不可用，已有数据已保留")
	}
}
func tmdbCenterList(svc *service.Container, person bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, e := positiveQueryInt(c.DefaultQuery("page", "1"), 1, 1000000)
		keyword := strings.TrimSpace(c.Query("keyword"))
		if e != nil || len([]rune(keyword)) > 200 {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		quality := -1
		if v := c.Query("quality"); v != "" {
			quality, e = strconv.Atoi(v)
			if e != nil || quality < 0 || quality > 3 {
				tmdbCenterError(c, service.ErrTMDbCenterInput)
				return
			}
		}
		state := c.Query("state")
		if state != "" && state != "pending" && state != "error" {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		if person {
			rows, n, e := svc.TMDbCenter.ListPeople(c.Request.Context(), keyword, page, c.Query("quality"), state)
			if e != nil {
				tmdbCenterError(c, e)
				return
			}
			Paginated(c, rows, n, page, 30)
			return
		}
		kind := c.DefaultQuery("kind", "movie")
		if kind == "tv" {
			kind = "series"
		}
		if kind != "movie" && kind != "series" {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}

		rows, n, e := svc.TMDbCenter.ListSubjects(c.Request.Context(), kind, "", keyword, quality, page, state)
		if e != nil {
			tmdbCenterError(c, e)
			return
		}
		Paginated(c, rows, n, page, 30)
	}
}
func tmdbCenterDetail(svc *service.Container, person, admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, e := uuid.Parse(c.Param("id")); e != nil {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		v, e := svc.TMDbCenter.Detail(c.Request.Context(), c.Param("id"), person, admin)
		if e != nil {
			tmdbCenterError(c, e)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		Success(c, v)
	}
}
func tmdbCenterChildren(svc *service.Container, kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if _, e := uuid.Parse(id); e != nil {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		p, e := svc.Repo.TMDb.Subject(c.Request.Context(), id)
		if e != nil {
			tmdbCenterError(c, e)
			return
		}
		if (kind == "season" && p.Kind != "series") || (kind == "episode" && p.Kind != "season") {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		page, e := positiveQueryInt(c.DefaultQuery("page", "1"), 1, 1000000)
		if e != nil {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		rows, n, e := svc.TMDbCenter.ListSubjects(c.Request.Context(), kind, id, "", -1, page)
		if e != nil {
			tmdbCenterError(c, e)
			return
		}
		Paginated(c, rows, n, page, 30)
	}
}
func tmdbCenterCredits(svc *service.Container, person bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, e := uuid.Parse(c.Param("id")); e != nil {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		page, e := positiveQueryInt(c.DefaultQuery("page", "1"), 1, 1000000)
		if e != nil {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		kind, category, order := c.Query("kind"), c.Query("category"), c.DefaultQuery("order", "newest")
		if kind == "tv" {
			kind = "series"
		}
		if (kind != "" && kind != "movie" && kind != "series") || (category != "" && category != "cast" && category != "crew") || (order != "newest" && order != "oldest") {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		rows, n, e := svc.TMDbCenter.Credits(c.Request.Context(), c.Param("id"), person, page, kind, category, order)
		if e != nil {
			tmdbCenterError(c, e)
			return
		}
		Paginated(c, rows, n, page, 60)
	}
}
func tmdbCenterEnsure(svc *service.Container, person, force bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, e := uuid.Parse(c.Param("id")); e != nil {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		user := ""
		if !force {
			user = c.GetString(middleware.CtxUserID)
		}
		j, e := svc.TMDbCenter.Queue(c.Request.Context(), c.Param("id"), c.DefaultQuery("scope", "detail"), user, person, force)
		if e != nil {
			tmdbCenterError(c, e)
			return
		}
		status := http.StatusAccepted
		if j.Status == "ready" {
			status = http.StatusOK
		}
		c.JSON(status, APIResponse{Code: 0, Message: "queued", Data: j})
	}
}
func tmdbCenterImport(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Kind string `json:"kind"`
			ID   int    `json:"tmdb_id"`
		}
		if c.ShouldBindJSON(&input) != nil {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		id, e := svc.TMDbCenter.Import(c.Request.Context(), input.Kind, input.ID)
		if e != nil {
			tmdbCenterError(c, e)
			return
		}
		Success(c, gin.H{"id": id})
	}
}
func tmdbCenterSearch(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			Kind    string `json:"kind"`
			Keyword string `json:"keyword"`
			Page    int    `json:"page"`
		}
		if c.ShouldBindJSON(&input) != nil {
			tmdbCenterError(c, service.ErrTMDbCenterInput)
			return
		}
		if input.Page == 0 {
			input.Page = 1
		}
		result, e := svc.TMDbCenter.Search(c.Request.Context(), input.Kind, strings.TrimSpace(input.Keyword), input.Page)
		if e != nil {
			tmdbCenterError(c, e)
			return
		}
		Success(c, result)
	}
}
func tmdbCenterImage(svc *service.Container, force bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, e := strconv.Atoi(c.Param("tmdbID"))
		kind, typ := c.Param("kind"), c.Param("type")
		if e != nil || id <= 0 || (kind != "movie" && kind != "series" && kind != "tv" && kind != "season" && kind != "episode" && kind != "person") || (typ != "poster" && typ != "backdrop" && typ != "still" && typ != "profile") {
			c.Status(400)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		e = svc.TMDbCenter.ServeImage(c.Request.Context(), c.Writer, c.Request, kind, id, typ, force)
		if e != nil {
			if service.IsTMDbCenterMissing(e) {
				c.Status(404)
			} else {
				c.Status(503)
			}
		}
	}
}
func registerTMDbCenterRoutes(group *gin.RouterGroup, svc *service.Container, admin bool) {
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if svc.TMDbCenter == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		c.Next()
	})
	group.GET("/subjects/:id", tmdbCenterDetail(svc, false, admin))
	group.GET("/subjects/:id/seasons", tmdbCenterChildren(svc, "season"))
	group.GET("/subjects/:id/episodes", tmdbCenterChildren(svc, "episode"))
	group.GET("/subjects/:id/credits", tmdbCenterCredits(svc, false))
	group.GET("/people/:id", tmdbCenterDetail(svc, true, admin))
	group.GET("/people/:id/works", tmdbCenterCredits(svc, true))
	if admin {
		group.POST("/subjects/:id/ensure", tmdbCenterEnsure(svc, false, false))
		group.POST("/people/:id/ensure", tmdbCenterEnsure(svc, true, false))
		group.GET("/subjects", tmdbCenterList(svc, false))
		group.GET("/people", tmdbCenterList(svc, true))
		group.POST("/subjects/:id/refresh", tmdbCenterEnsure(svc, false, true))
		group.POST("/people/:id/refresh", tmdbCenterEnsure(svc, true, true))
		group.POST("/import", tmdbCenterImport(svc))
		group.POST("/search", tmdbCenterSearch(svc))
		group.POST("/images/:kind/:tmdbID/:type/refresh", tmdbCenterImage(svc, true))
	} else {
		group.POST("/subjects/:id/ensure", tmdbCenterEnsure(svc, false, false))
		group.POST("/people/:id/ensure", tmdbCenterEnsure(svc, true, false))
		group.GET("/images/:kind/:tmdbID/:type", tmdbCenterImage(svc, false))
		group.HEAD("/images/:kind/:tmdbID/:type", tmdbCenterImage(svc, false))
	}
}
