package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func artworkHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc.Artwork == nil {
			c.Status(http.StatusNotFound)
			return
		}
		err := svc.Artwork.Serve(c.Request.Context(), c.Writer, c.Request, c.Param("id"))
		if err == nil {
			return
		}
		if errors.Is(err, service.ErrArtworkNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
