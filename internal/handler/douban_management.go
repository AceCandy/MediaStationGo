package handler

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func listDoubanSubjectsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, pageErr := positiveQueryInt(c.DefaultQuery("page", "1"), 1, 1_000_000)
		pageSize, sizeErr := positiveQueryInt(c.DefaultQuery("page_size", "30"), 1, 100)
		keyword, kind := strings.TrimSpace(c.Query("keyword")), c.Query("type")
		if pageErr != nil || sizeErr != nil || utf8.RuneCountInString(keyword) > 200 || (kind != "" && kind != "movie" && kind != "series" && kind != "season" && kind != "episode") {
			Error(c, http.StatusBadRequest, ErrInvalidParams, "资料查询参数无效")
			return
		}
		if svc == nil || svc.Douban == nil {
			Error(c, http.StatusServiceUnavailable, ErrInternal, "豆瓣资料中心暂不可用")
			return
		}
		items, total, err := svc.Douban.ListSubjects(c.Request.Context(), keyword, kind, page, pageSize)
		if err != nil {
			Error(c, http.StatusInternalServerError, ErrInternal, "豆瓣资料列表读取失败")
			return
		}
		Paginated(c, items, total, page, pageSize)
	}
}

func doubanSubjectHandler(svc *service.Container, refresh bool) gin.HandlerFunc {
	return func(c *gin.Context) {
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
			result, err = svc.Douban.RefreshSubject(c.Request.Context(), id)
		} else {
			result, err = svc.Douban.SubjectDetail(c.Request.Context(), id)
		}
		if err != nil {
			switch {
			case errors.Is(err, service.ErrDoubanSubjectMissing):
				Error(c, http.StatusNotFound, ErrNotFound, "共享资料不存在")
			case errors.Is(err, service.ErrDoubanSubjectReadOnly):
				Error(c, http.StatusBadRequest, ErrInvalidParams, "历史季和集资料仅支持查看")
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
