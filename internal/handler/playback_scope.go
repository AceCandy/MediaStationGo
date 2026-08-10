package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func enforceScopedPlaybackToken(c *gin.Context, mediaID string) bool {
	mediaID = strings.TrimSpace(mediaID)
	purpose, _ := c.Get(middleware.CtxTokenPurpose)
	if strings.TrimSpace(toString(purpose)) == "" {
		return true
	}
	if strings.TrimSpace(toString(purpose)) != service.ExternalPlaybackTokenPurpose {
		c.JSON(http.StatusForbidden, gin.H{"error": "playback token scope denied"})
		return false
	}
	tokenMediaID, _ := c.Get(middleware.CtxTokenMediaID)
	if mediaID == "" || strings.TrimSpace(toString(tokenMediaID)) != mediaID {
		c.JSON(http.StatusForbidden, gin.H{"error": "playback token media mismatch"})
		return false
	}
	return true
}
