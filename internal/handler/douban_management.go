package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func listDoubanSubjectsHandler(svc *service.Container, admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		page, pageErr := positiveQueryInt(c.DefaultQuery("page", "1"), 1, 1_000_000)
		pageSize, sizeErr := positiveQueryInt(c.DefaultQuery("page_size", "30"), 1, 100)
		keyword, kind := strings.TrimSpace(c.Query("keyword")), c.Query("type")
		genre := strings.TrimSpace(c.Query("genre"))
		if pageErr != nil || sizeErr != nil || utf8.RuneCountInString(keyword) > 200 || utf8.RuneCountInString(genre) > 100 || (kind != "" && kind != "movie" && kind != "series") {
			Error(c, http.StatusBadRequest, ErrInvalidParams, "资料查询参数无效")
			return
		}
		if svc == nil || svc.Douban == nil {
			Error(c, http.StatusServiceUnavailable, ErrInternal, "豆瓣资料中心暂不可用")
			return
		}
		items, total, err := svc.Douban.ListSubjects(c.Request.Context(), keyword, kind, page, pageSize, genre)
		if err != nil {
			Error(c, http.StatusInternalServerError, ErrInternal, "豆瓣资料列表读取失败")
			return
		}
		if !admin {
			for i := range items {
				items[i].LastAttemptAt, items[i].NextRetryAt = nil, nil
			}
		}
		Paginated(c, items, total, page, pageSize)
	}
}

func doubanCategoriesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		kind := c.DefaultQuery("type", "movie")
		if kind != "movie" && kind != "series" {
			Error(c, http.StatusBadRequest, ErrInvalidParams, "资料分类参数无效")
			return
		}
		if svc == nil || svc.Douban == nil {
			Error(c, http.StatusServiceUnavailable, ErrInternal, "豆瓣资料中心暂不可用")
			return
		}
		rows, err := svc.Douban.Categories(c.Request.Context(), kind)
		if err != nil {
			Error(c, http.StatusInternalServerError, ErrInternal, "资料分类读取失败")
			return
		}
		Success(c, rows)
	}
}

func doubanSubjectHandler(svc *service.Container, refresh, admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		id := c.Param("id")
		if _, err := uuid.Parse(id); err != nil {
			Error(c, http.StatusBadRequest, ErrInvalidParams, "资料标识无效")
			return
		}
		if svc == nil || svc.Douban == nil {
			Error(c, http.StatusServiceUnavailable, ErrInternal, "豆瓣资料中心暂不可用")
			return
		}
		var result any
		var err error
		if refresh {
			mode := c.DefaultQuery("mode", "full")
			if len(c.QueryArray("mode")) > 1 || (mode != "full" && mode != "base") {
				Error(c, http.StatusBadRequest, ErrInvalidParams, "资料刷新模式无效")
				return
			}
			if mode == "base" {
				result, err = svc.Douban.RefreshSubjectBase(c.Request.Context(), id)
			} else {
				result, err = svc.Douban.RefreshSubject(c.Request.Context(), id)
			}
		} else {
			detail, detailErr := svc.Douban.SubjectDetail(c.Request.Context(), id)
			if detail != nil && !admin {
				detail.RawJSON = nil
				detail.LastAttemptAt, detail.NextRetryAt = nil, nil
			}
			result, err = detail, detailErr
		}
		if err != nil {
			switch {
			case errors.Is(err, service.ErrDoubanSubjectMissing):
				Error(c, http.StatusNotFound, ErrNotFound, "共享资料不存在")
			case errors.Is(err, service.ErrDoubanSubjectKind):
				Error(c, http.StatusBadRequest, ErrInvalidParams, "豆瓣资料类型必须是电影或电视剧")
			case errors.Is(err, service.ErrDoubanSubjectNotFound):
				Error(c, http.StatusBadGateway, ErrExternal, "豆瓣条目不存在，已有资料已保留")
			case errors.Is(err, service.ErrDoubanTemporarilyUnavailable):
				Error(c, http.StatusServiceUnavailable, ErrExternal, "豆瓣暂不可用，已有资料已保留，请稍后重试")
			default:
				Error(c, http.StatusInternalServerError, ErrInternal, "豆瓣资料操作失败")
			}
			return
		}
		Success(c, result)
	}
}

func registerDoubanCenterRoutes(group *gin.RouterGroup, svc *service.Container, admin bool) {
	group.POST("/subjects/ensure", ensureDoubanSubjectHandler(svc))
	group.GET("/subjects", listDoubanSubjectsHandler(svc, admin))
	group.GET("/categories", doubanCategoriesHandler(svc))
	group.GET("/subjects/:id", doubanSubjectHandler(svc, false, admin))
	group.GET("/subjects/:id/poster", doubanSubjectPosterHandler(svc))
	group.HEAD("/subjects/:id/poster", doubanSubjectPosterHandler(svc))
	group.GET("/subjects/:id/credits/:index/image", doubanSubjectCreditImageHandler(svc))
	group.HEAD("/subjects/:id/credits/:index/image", doubanSubjectCreditImageHandler(svc))
	if admin {
		group.POST("/subjects/:id/refresh", doubanSubjectHandler(svc, true, true))
	}
}

func ensureDoubanSubjectHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input struct {
			ID   string `json:"douban_id"`
			Kind string `json:"kind"`
		}
		if c.ShouldBindJSON(&input) != nil {
			Error(c, http.StatusBadRequest, ErrInvalidParams, "来源身份参数无效")
			return
		}
		if svc == nil || svc.Douban == nil {
			Error(c, http.StatusServiceUnavailable, ErrInternal, "豆瓣资料中心暂不可用")
			return
		}
		detail, err := svc.Douban.EnsureSubject(c.Request.Context(), input.ID, input.Kind, c.GetString(middleware.CtxUserID))
		if err != nil {
			if errors.Is(err, repository.ErrDoubanQueueLimit) {
				Error(c, 429, ErrInvalidParams, "待补全请求已达上限，请稍后重试")
			} else if errors.Is(err, service.ErrDoubanBindingInvalid) {
				Error(c, 400, ErrInvalidParams, "请选择有效的豆瓣电影或电视剧身份")
			} else {
				Error(c, 503, ErrExternal, "豆瓣资料暂不可用")
			}
			return
		}
		detail.RawJSON = nil
		detail.LastAttemptAt, detail.NextRetryAt = nil, nil
		c.Header("Cache-Control", "private, no-store")
		Success(c, detail)
	}
}

func doubanSubjectPosterHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		id := c.Param("id")
		if _, err := uuid.Parse(id); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		if svc == nil || svc.Douban == nil || svc.Artwork == nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		err := svc.Douban.ServeSubjectPoster(c.Request.Context(), c.Writer, c.Request, id, svc.Artwork)
		if err == nil {
			return
		}
		if errors.Is(err, service.ErrDoubanSubjectMissing) || errors.Is(err, service.ErrArtworkNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusInternalServerError)
	}
}

func doubanSubjectCreditImageHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		id := c.Param("id")
		index, err := strconv.Atoi(c.Param("index"))
		if _, idErr := uuid.Parse(id); idErr != nil || err != nil || index < 0 || index > 1000 {
			c.Status(http.StatusBadRequest)
			return
		}
		if svc == nil || svc.Douban == nil || svc.PeopleImages == nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		err = svc.Douban.ServeSubjectCreditImage(c.Request.Context(), c.Writer, c.Request, id, index, svc.PeopleImages)
		if err == nil {
			return
		}
		if errors.Is(err, service.ErrDoubanSubjectMissing) || errors.Is(err, service.ErrPeopleImageNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusInternalServerError)
	}
}
