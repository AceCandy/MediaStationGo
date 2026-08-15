package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func playerRequestLogsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || svc.PlayerLogs == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "player request logs unavailable"})
			return
		}
		monthText := strings.TrimSpace(c.DefaultQuery("month", time.Now().UTC().Format("2006-01")))
		month, err := time.Parse("2006-01", monthText)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "month must use YYYY-MM"})
			return
		}
		page, err := positiveQueryInt(c.DefaultQuery("page", "1"), 1, 1_000_000)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page"})
			return
		}
		pageSize, err := positiveQueryInt(c.DefaultQuery("page_size", "50"), 1, 100)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_size"})
			return
		}
		method := strings.ToUpper(strings.TrimSpace(c.Query("method")))
		if method != "" && !validHTTPMethod(method) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid method"})
			return
		}
		var status *int
		if raw := strings.TrimSpace(c.Query("status")); raw != "" {
			value, parseErr := strconv.Atoi(raw)
			if parseErr != nil || value < 100 || value > 599 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
				return
			}
			status = &value
		}
		route := strings.TrimSpace(c.Query("path"))
		if len(route) > 512 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path is too long"})
			return
		}
		result, err := svc.PlayerLogs.List(c.Request.Context(), service.PlayerRequestLogFilter{
			Month: month, Route: route, Method: method, Status: status, Page: page, PageSize: pageSize,
		})
		if err != nil {
			if svc.Log != nil {
				svc.Log.Error("list player request logs failed", zap.Error(err))
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list player request logs"})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func positiveQueryInt(raw string, minValue, maxValue int) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return 0, strconv.ErrSyntax
	}
	return value, nil
}

func validHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}
