// Package handler — stats / dashboard endpoints.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func statsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		snap, err := svc.Stats.Compute(c.Request.Context(), svc.Cfg.App.DataDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := applyStatsVisibility(c, svc, snap); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, snap)
	}
}

func applyStatsVisibility(c *gin.Context, svc *service.Container, snap *service.Snapshot) error {
	visibility := mediaVisibilityForRequest(c, svc)
	libs, err := svc.Repo.Library.List(c.Request.Context())
	if err != nil {
		return err
	}
	var visibleLibraries int64
	for _, lib := range libs {
		if service.LibraryVisibleForUser(c.Request.Context(), svc.Repo, lib, visibility) {
			visibleLibraries++
		}
	}
	snap.Libraries = visibleLibraries

	q := applyMediaVisibilityQuery(svc.Repo.DB.WithContext(c.Request.Context()).Model(&model.Media{}), visibility)
	if err := q.Count(&snap.MediaCount).Error; err != nil {
		return err
	}
	type sumRow struct {
		Size    int64
		Seconds int64
	}
	var sum sumRow
	if err := applyMediaVisibilityQuery(svc.Repo.DB.WithContext(c.Request.Context()).Model(&model.Media{}), visibility).
		Select("COALESCE(SUM(size_bytes),0) as size, COALESCE(SUM(duration_sec),0) as seconds").
		Scan(&sum).Error; err != nil {
		return err
	}
	snap.TotalSizeBytes = sum.Size
	snap.TotalSeconds = sum.Seconds

	var recent []model.Media
	if err := applyMediaVisibilityQuery(svc.Repo.DB.WithContext(c.Request.Context()).Model(&model.Media{}), visibility).
		Order("created_at desc").
		Limit(12).
		Find(&recent).Error; err != nil {
		return err
	}
	snap.RecentlyAdded = recent
	return nil
}

func applyMediaVisibilityQuery(q *gorm.DB, visibility service.MediaVisibility) *gorm.DB {
	if !visibility.IncludeNSFW {
		q = q.Where("nsfw = ?", false)
	}
	if len(visibility.HiddenLibraryIDs) > 0 {
		q = q.Where("library_id NOT IN ?", visibility.HiddenLibraryIDs)
	}
	if len(visibility.AllowedLibraryIDs) > 0 {
		q = q.Where("library_id IN ?", visibility.AllowedLibraryIDs)
	}
	return q
}
