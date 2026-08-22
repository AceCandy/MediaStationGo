package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func embyPlaybackInfoHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		selection, requestedUserID, err := embyPlaybackSelection(c)
		if err != nil {
			if svc != nil && svc.Log != nil {
				svc.Log.Info("emby playback info request rejected",
					zap.String("item_id", c.Param("id")),
					zap.Error(err),
					zap.String("request_method", c.Request.Method),
				)
			}
			embyError(c, http.StatusBadRequest, err.Error())
			return
		}
		if requestedUserID != "" && requestedUserID != embyUserID(c) && !middleware.IsAdmin(c) {
			embyError(c, http.StatusForbidden, "forbidden")
			return
		}
		if c.Param("userId") == "" && requestedUserID != "" {
			uid = requestedUserID
		}
		out, err := svc.Emby.PlaybackInfoWithOptions(c.Request.Context(), c.Param("id"), uid, selection)
		if errors.Is(err, service.ErrInvalidStreamIndex) {
			if svc != nil && svc.Log != nil {
				svc.Log.Info("emby playback info selection rejected",
					zap.String("item_id", c.Param("id")),
					zap.String("user_id", uid),
					zap.String("media_source_id", selection.MediaSourceID),
					zap.Intp("audio_stream_index", selection.AudioStreamIndex),
					zap.Intp("subtitle_stream_index", selection.SubtitleStreamIndex),
					zap.Error(err),
				)
			}
			embyError(c, http.StatusBadRequest, err.Error())
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if out == nil {
			embyError(c, http.StatusNotFound, "not found")
			return
		}
		embyAttachRequestTokenToMediaSources(c, out)
		c.JSON(http.StatusOK, out)
	}
}

func embyPlaybackSelection(c *gin.Context) (service.PlaybackSelection, string, error) {
	selection, err := service.PlaybackSelectionFromQuery(c.Request.URL.Query())
	if err != nil {
		return service.PlaybackSelection{}, "", err
	}
	var req model.EmbyPlaybackInfoRequest
	if c.Request.Method == http.MethodPost && c.Request.Body != nil {
		if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			return service.PlaybackSelection{}, "", err
		}
		if req.AudioStreamIndex != nil {
			selection.AudioStreamIndex = req.AudioStreamIndex
		}
		if req.SubtitleStreamIndex != nil {
			selection.SubtitleStreamIndex = req.SubtitleStreamIndex
		}
		if strings.TrimSpace(req.MediaSourceId) != "" {
			selection.MediaSourceID = strings.TrimSpace(req.MediaSourceId)
		}
	}
	return selection, strings.TrimSpace(req.UserId), nil
}

func embyAttachRequestTokenToMediaSources(c *gin.Context, out any) {
	token := embyRequestToken(c)
	if token == "" || out == nil {
		return
	}
	embyAttachTokenToMediaSourcesValue(out, token)
}

func embyAttachTokenToMediaSourcesValue(value any, token string) {
	switch typed := value.(type) {
	case map[string]any:
		embyAttachTokenToMediaSourcesMap(typed, token)
	case gin.H:
		embyAttachTokenToMediaSourcesMap(map[string]any(typed), token)
	case []map[string]any:
		for _, item := range typed {
			embyAttachTokenToMediaSourcesMap(item, token)
		}
	case []any:
		for _, item := range typed {
			embyAttachTokenToMediaSourcesValue(item, token)
		}
	}
}

func embyAttachTokenToMediaSourcesMap(out map[string]any, token string) {
	if out == nil {
		return
	}
	if sources, ok := out["MediaSources"].([]map[string]any); ok {
		embyAttachTokenToMediaSources(sources, token)
	} else if sources, ok := out["MediaSources"].([]any); ok {
		for _, source := range sources {
			if sourceMap, ok := source.(map[string]any); ok {
				embyAttachTokenToMediaSources([]map[string]any{sourceMap}, token)
			}
		}
	}
	if items, ok := out["Items"]; ok {
		embyAttachTokenToMediaSourcesValue(items, token)
	}
}

func embyAttachTokenToMediaSources(sources []map[string]any, token string) {
	for _, source := range sources {
		if raw, ok := source["DirectStreamUrl"].(string); ok {
			source["DirectStreamUrl"] = embyAppendAPIKey(raw, token)
		}
		if streams, ok := source["MediaStreams"].([]map[string]any); ok {
			embyAttachTokenToSubtitleStreams(streams, token)
		} else if streams, ok := source["MediaStreams"].([]any); ok {
			for _, stream := range streams {
				if mapped, ok := stream.(map[string]any); ok {
					embyAttachTokenToSubtitleStreams([]map[string]any{mapped}, token)
				}
			}
		}
	}
}

func embyAttachTokenToSubtitleStreams(streams []map[string]any, token string) {
	for _, stream := range streams {
		if raw, ok := stream["DeliveryUrl"].(string); ok {
			stream["DeliveryUrl"] = embyAppendAPIKey(raw, token)
		}
	}
}

func embySubtitleHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc.Subtitle == nil {
			c.Status(http.StatusNotFound)
			return
		}
		uid := embyUserID(c)
		view, err := svc.Repo.MediaView.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		if view != nil {
			if !mediaViewVisibleForRequest(c, svc, view) {
				c.Status(http.StatusNotFound)
				return
			}
		} else {
			mediaID, err := svc.Emby.PlayableMediaID(c.Request.Context(), c.Param("id"), uid)
			if err != nil || mediaID == "" {
				c.Status(http.StatusNotFound)
				return
			}
			view, err = svc.Repo.MediaView.FindByID(c.Request.Context(), mediaID)
			if err != nil || view == nil || !mediaViewVisibleForRequest(c, svc, view) {
				c.Status(http.StatusNotFound)
				return
			}
		}
		index, err := strconv.Atoi(c.Param("index"))
		if err != nil || index < 0 {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Type", "text/vtt; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		if err := svc.Subtitle.ServeMediaByIndex(c.Request.Context(), &view.Media, index, c.Writer); err != nil && !c.Writer.Written() {
			c.Status(http.StatusNotFound)
		}
	}
}

func embyRequestToken(c *gin.Context) string {
	if c == nil {
		return ""
	}
	for _, key := range []string{"api_key", "apiKey", "ApiKey", "token", "X-Emby-Token", "X-MediaBrowser-Token"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			return value
		}
	}
	for _, header := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			return value
		}
	}
	for _, header := range []string{"Authorization", "X-Emby-Authorization", "X-MediaBrowser-Authorization"} {
		if token := embyTokenFromAuthHeader(c.GetHeader(header)); token != "" {
			return token
		}
	}
	return ""
}

func embyTokenFromAuthHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, prefix := range []string{"Bearer ", "Emby "} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
	}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "MediaBrowser "))
		if !strings.HasPrefix(part, "Token=") {
			continue
		}
		token := strings.TrimSpace(strings.TrimPrefix(part, "Token="))
		return strings.Trim(token, `"`)
	}
	if strings.Contains(value, "Token=") {
		return ""
	}
	return value
}

func embyAppendAPIKey(raw, token string) string {
	raw = strings.TrimSpace(raw)
	token = strings.TrimSpace(token)
	if raw == "" || token == "" {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() {
		return raw
	}
	q := u.Query()
	if q.Get("api_key") == "" && q.Get("apiKey") == "" && q.Get("token") == "" {
		q.Set("api_key", token)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// embyVideoStreamHandler 是 GET /Videos/{id}/stream 的入口，路径 ID 为具体媒体 ID。
func embyVideoStreamHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetRawMedia(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeInternalOrCanceled(c, err)
			return
		}
		if m == nil || !mediaVisibleForRequest(c, svc, m) {
			c.Status(http.StatusNotFound)
			return
		}
		if !enforceScopedPlaybackToken(c, m.ID) {
			return
		}
		err = svc.Stream.ServeMedia(c.Writer, c.Request, m)
		switch {
		case err == nil:
		case errors.Is(err, service.ErrMediaNotFound):
			c.Status(http.StatusNotFound)
		case requestContextCanceled(c, err):
			c.AbortWithStatus(statusClientClosedRequest)
		default:
			if !c.Writer.Written() {
				c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			}
		}
	}
}

func embyPlaybackRedirectToken(c *gin.Context, svc *service.Container) string {
	if token := embyRequestToken(c); token != "" {
		return token
	}
	if c == nil || svc == nil || svc.Auth == nil || svc.Repo == nil || svc.Repo.User == nil {
		return ""
	}
	uid := embyUserID(c)
	if uid == "" {
		return ""
	}
	u, err := svc.Repo.User.FindByID(c.Request.Context(), uid)
	if err != nil || u == nil {
		return ""
	}
	token, err := svc.Auth.IssueEmbyToken(u)
	if err != nil {
		return ""
	}
	return token
}
