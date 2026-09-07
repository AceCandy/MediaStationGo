package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type embyPlayingReq struct {
	ItemId        string `json:"ItemId"`
	MediaSourceId string `json:"MediaSourceId"`
	PlaySessionId string `json:"PlaySessionId"`
	PositionTicks int64  `json:"PositionTicks"`
	RunTimeTicks  int64  `json:"RunTimeTicks"`
}

func embyPlayingProgressHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := embyUserID(c)
		if uid == "" {
			c.Status(http.StatusUnauthorized)
			return
		}
		var req embyPlayingReq
		_ = c.ShouldBindJSON(&req)
		if req.ItemId == "" {
			req.ItemId = c.Query("ItemId")
		}
		if req.MediaSourceId == "" {
			req.MediaSourceId = c.Query("MediaSourceId")
		}
		if req.PlaySessionId == "" {
			req.PlaySessionId = c.Query("PlaySessionId")
		}
		if req.PositionTicks == 0 {
			req.PositionTicks, _ = strconv.ParseInt(c.Query("PositionTicks"), 10, 64)
		}
		if req.RunTimeTicks == 0 {
			req.RunTimeTicks, _ = strconv.ParseInt(c.Query("RunTimeTicks"), 10, 64)
		}
		if req.ItemId == "" {
			c.Status(http.StatusOK)
			return
		}
		clientInfo := embyClientInfoFromRequest(c)
		if svc.Device != nil && svc.Device.IsTerminalKicked(c.Request.Context(), uid, clientInfo.DeviceID, clientInfo.DeviceName, clientInfo.Client) {
			c.Status(http.StatusUnauthorized)
			return
		}
		if err := svc.Emby.RecordProgress(c.Request.Context(), uid, req.ItemId, req.MediaSourceId, req.PlaySessionId, req.PositionTicks, req.RunTimeTicks); err != nil {
			writePlaybackProgressError(c, err)
			return
		}
		stopped := strings.Contains(strings.ToLower(c.FullPath()+" "+c.Request.URL.Path), "stopped")
		if svc.Sessions != nil {
			svc.Sessions.RecordPlayback(c.Request.Context(), uid, req.PlaySessionId,
				clientInfo.DeviceID,
				clientInfo.DeviceName,
				clientInfo.Client,
				c.ClientIP(),
				req.ItemId,
				req.PositionTicks,
				req.RunTimeTicks,
				stopped)
		}
		if svc.Device != nil && !stopped {
			svc.Device.RecordPlayback(c.Request.Context(), uid,
				clientInfo.DeviceID,
				clientInfo.DeviceName,
				clientInfo.Client)
		}
		c.Status(http.StatusNoContent)
	}
}

func embyFavoriteHandler(svc *service.Container, fav bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		mid := c.Param("itemId")
		if uid == "" || mid == "" {
			c.Status(http.StatusBadRequest)
			return
		}
		if err := svc.Emby.SetFavorite(c.Request.Context(), uid, mid, fav); err != nil {
			writeFavoriteError(c, err)
			return
		}
		out, _ := svc.Emby.Item(c.Request.Context(), mid, uid)
		if out != nil {
			c.JSON(http.StatusOK, out["UserData"])
			return
		}
		c.JSON(http.StatusOK, gin.H{"IsFavorite": fav})
	}
}

func embyMarkPlayedHandler(svc *service.Container, played bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		mid := c.Param("itemId")
		if uid == "" || mid == "" {
			c.Status(http.StatusBadRequest)
			return
		}
		if err := svc.Emby.MarkPlayed(c.Request.Context(), uid, mid, played); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if played && svc.Device != nil {
			clientInfo := embyClientInfoFromRequest(c)
			svc.Device.RecordPlayback(c.Request.Context(), uid, clientInfo.DeviceID, clientInfo.DeviceName, clientInfo.Client)
		}
		out, _ := svc.Emby.Item(c.Request.Context(), mid, uid)
		if out != nil {
			c.JSON(http.StatusOK, out["UserData"])
			return
		}
		c.JSON(http.StatusOK, gin.H{"Played": played})
	}
}
