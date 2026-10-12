package handler

import (
	"errors"
	"net/http"
	"strconv"
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
		if pageErr != nil || sizeErr != nil || utf8.RuneCountInString(keyword) > 200 || (kind != "" && kind != "movie" && kind != "series") {
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
			result, err = svc.Douban.SubjectDetail(c.Request.Context(), id)
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
