package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
)

func embyProgressSnapshotHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		itemID := c.Param("id")
		var out service.EmbyProgressSnapshot
		var err error
		if c.Request.Method == http.MethodGet {
			out, err = svc.Emby.ReadProgressSnapshot(c.Request.Context(), uid, itemID, c.Query("MediaSourceId"))
		} else {
			var req struct {
				MediaSourceID    string `json:"MediaSourceId"`
				ExpectedRevision string `json:"ExpectedRevision"`
				PositionTicks    *int64 `json:"PositionTicks"`
				RunTimeTicks     *int64 `json:"RunTimeTicks"`
				Played           *bool  `json:"Played"`
			}
			if c.ShouldBindJSON(&req) != nil || req.PositionTicks == nil || req.RunTimeTicks == nil || req.Played == nil || req.MediaSourceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "准确同步必须提供版本、进度、时长和已看状态"})
				return
			}
			revision, parseErr := strconv.ParseInt(req.ExpectedRevision, 10, 64)
			if parseErr != nil || revision < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "进度版本无效"})
				return
			}
			out, err = svc.Emby.SyncProgressSnapshot(c.Request.Context(), uid, itemID, req.MediaSourceID, revision, *req.PositionTicks, *req.RunTimeTicks, *req.Played)
		}
		if err != nil {
			switch {
			case errors.Is(err, repository.ErrPlaybackProgressConflict):
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case errors.Is(err, service.ErrProgressSyncTarget):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				writePlaybackProgressError(c, err)
			}
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
