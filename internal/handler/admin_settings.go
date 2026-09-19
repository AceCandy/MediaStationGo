package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type settingReq struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value"`
}

func listSettingsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		settings, err := svc.Repo.Setting.All(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, settings)
	}
}

func updateSettingHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req settingReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		oldValue := ""
		if req.Key == service.EmbyLibraryDisplaySettingKey {
			entries, err := service.DecodeEmbyLibraryDisplay(req.Value)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			libs, err := svc.Repo.Library.List(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			known := make(map[string]bool, len(libs))
			for _, lib := range libs {
				known[lib.ID] = true
			}
			for _, entry := range entries {
				if !known[entry.ID] {
					c.JSON(http.StatusBadRequest, gin.H{"error": "媒体库已变更，请重新打开弹窗"})
					return
				}
			}
			if req.Value != "" {
				canonical, _ := json.Marshal(entries)
				req.Value = string(canonical)
			}
		}
		if req.Key == service.AdultLibraryIDsSettingKey {
			oldValue, _ = svc.Repo.Setting.Get(c.Request.Context(), req.Key)
		}
		if err := svc.Repo.Setting.Set(c.Request.Context(), req.Key, req.Value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		oldAdultLibraryIDs := service.DecodeAllowedLibraryIDs(oldValue)
		newAdultLibraryIDs := service.DecodeAllowedLibraryIDs(req.Value)
		if req.Key == service.AdultLibraryIDsSettingKey && len(oldAdultLibraryIDs) == 0 && len(newAdultLibraryIDs) > 0 {
			_ = svc.Repo.DB.WithContext(c.Request.Context()).Model(&model.User{}).Where("hide_adult = ?", false).Update("hide_adult", true).Error
		}
		service.ApplyRuntimeSetting(svc.Cfg, req.Key, req.Value)
		if svc.FFprobe != nil && (req.Key == "ffprobe.max_concurrent" || req.Key == "app.ffprobe_max_concurrent") {
			svc.FFprobe.SetMaxConcurrent(svc.Cfg.App.FFprobeMaxConcurrent)
		}
		c.Status(http.StatusNoContent)
	}
}

func recentLogsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := svc.Repo.Log.Recent(c.Request.Context(), 200)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, rows)
	}
}
