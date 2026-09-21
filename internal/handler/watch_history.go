// Package handler — watch history endpoints.
//
// The base /history GET / POST routes already exist; these add the three
// auxiliary surfaces the React WatchHistoryPage needs:
//
//	GET  /api/watch-history          paginated list (admin sees every user)
//	GET  /api/watch-history/stats    aggregate watch time + completion
//	GET  /api/watch-history/continue resume and next-episode rail
//	DELETE /api/watch-history        clear (?media_item_id= optional)
//	DELETE /api/watch-history/:id    remove one row
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// historyListHandler returns the caller's history rows joined with the
// matching media in a single response.
func historyListHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		if limit <= 0 || limit > 500 {
			limit = 50
		}
		items, err := svc.Playback.RecentHistory(c.Request.Context(), toString(uid), limit, mediaVisibilityForRequest(c, svc))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, items)
	}
}

// historyStatsHandler returns aggregate watch time + completion counts
// for the caller. Used by the WatchHistoryPage hero card.
func historyStatsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		userID := toString(uid)

		var total int64
		_ = svc.Repo.DB.Model(&model.PlaybackHistory{}).
			Where("user_id = ?", userID).Count(&total).Error

		var completed int64
		_ = svc.Repo.DB.Model(&model.PlaybackHistory{}).
			Where("user_id = ? AND completed = ?", userID, true).Count(&completed).Error

		var watchedMs int64
		_ = svc.Repo.DB.Model(&model.PlaybackHistory{}).
			Where("user_id = ?", userID).
			Select("COALESCE(SUM(position_ms), 0)").
			Row().Scan(&watchedMs)

		var last *time.Time
		row := svc.Repo.DB.Model(&model.PlaybackHistory{}).
			Where("user_id = ?", userID).
			Select("MAX(watched_at)").Row()
		var lastT time.Time
		if err := row.Scan(&lastT); err == nil && !lastT.IsZero() {
			last = &lastT
		}

		c.JSON(http.StatusOK, gin.H{
			"total":         total,
			"completed":     completed,
			"watched_ms":    watchedMs,
			"watched_hours": float64(watchedMs) / 1000.0 / 3600.0,
			"last_watched":  last,
		})
	}
}

// historyContinueHandler 返回断点和只读下一集候选，按关联剧的观看时间排序。
func historyContinueHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		if limit <= 0 || limit > 50 {
			limit = 10
		}
		items, err := svc.Playback.ContinueHistory(c.Request.Context(), toString(uid), limit, mediaVisibilityForRequest(c, svc))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]gin.H, 0, len(items))
		for _, item := range items {
			if item.Media == nil {
				continue
			}
			history := item
			history.Media = nil
			out = append(out, gin.H{
				"history": history,
				"media":   item.Media,
			})
		}
		c.JSON(http.StatusOK, out)
	}
}

// historyDeleteHandler removes one or all history rows for the caller.
//
//	DELETE /api/watch-history?media_id=xxx  → delete just that media's row
//	DELETE /api/watch-history?status=completed   → delete completed rows
//	DELETE /api/watch-history?status=incomplete  → delete unfinished rows
//	DELETE /api/watch-history               → clear all rows for the user
func historyDeleteHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		userID := toString(uid)
		mediaID := c.Query("media_id")
		status := c.Query("status")
		var completed *bool
		switch status {
		case "completed", "watched":
			value := true
			completed = &value
		case "incomplete", "unfinished", "unwatched":
			value := false
			completed = &value
		case "":
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "status must be completed or incomplete"})
			return
		}

		q := svc.Repo.DB.Where("user_id = ?", userID)
		if mediaID != "" {
			removed, err := svc.Playback.DeleteHistoryForMedia(c.Request.Context(), userID, mediaID, completed)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"removed": removed})
			return
		}
		if completed != nil {
			q = q.Where("completed = ?", *completed)
		}
		res := q.Delete(&model.PlaybackHistory{})
		if err := res.Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"removed": res.RowsAffected})
	}
}

func historyDeleteOneHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		res := svc.Repo.DB.
			Where("user_id = ? AND id = ?", toString(uid), c.Param("id")).
			Delete(&model.PlaybackHistory{})
		if err := res.Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"removed": res.RowsAffected})
	}
}
