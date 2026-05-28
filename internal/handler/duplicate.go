// Package handler — duplicate-file finder.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func listDuplicatesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libraryID := c.Query("library_id")
		report, err := svc.Duplicate.Current(c.Request.Context(), libraryID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, report)
	}
}

func detectDuplicatesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libraryID := c.Query("library_id")
		report, err := svc.Duplicate.Detect(c.Request.Context(), libraryID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, report)
	}
}

func unmarkDuplicatesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libraryID := c.Query("library_id")
		n, err := svc.Duplicate.Unmark(c.Request.Context(), libraryID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"unmarked": n})
	}
}
