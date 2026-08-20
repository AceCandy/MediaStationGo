// Package handler — system config + scheduler trigger + events ticket.
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// listSystemConfigHandler is the non-admin alias for /admin/settings.
// It returns the same key/value rows so the Vue UI's `system.getConfig`
// helper keeps working.
func listSystemConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := svc.Repo.Setting.All(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Hide secret-flavoured keys for non-admins.
		role, _ := c.Get(middleware.CtxUserRole)
		out := make([]model.Setting, 0, len(rows))
		for _, s := range rows {
			if role != "admin" && isSecretKey(s.Key) {
				s.Value = "********"
			}
			out = append(out, s)
		}
		c.JSON(http.StatusOK, gin.H{"items": out})
	}
}

func isSecretKey(k string) bool {
	for _, suffix := range []string{".token", ".secret", ".password", ".api_key", ".cookie"} {
		if endsWith(k, suffix) {
			return true
		}
	}
	return false
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// schemaHandler returns the curated settings schema (used by the
// `getSchema()` Vue helper). It mirrors the SettingsPage groupings but
// in JSON so the upstream UI can render its dynamic form.
func schemaHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"groups": []gin.H{
				{
					"key":   "general",
					"label": "常规",
					"items": []gin.H{
						{"key": "tmdb.language", "type": "select", "label": "TMDb 元数据语言"},
						{"key": "app.server_url", "type": "text", "label": "公开访问域名 / STRM 域名"},
						{"key": "playback.path_mappings", "type": "textarea", "label": "本地播放路径 302 映射"},
						{"key": "playback.redirect_resolve_prefixes", "type": "textarea", "label": "播放直链 302 预解析前缀"},
						{"key": "ffprobe.path_mappings", "type": "textarea", "label": "提取轨道路径映射"},
						{"key": "ffprobe.path", "type": "text", "label": "FFprobe 路径"},
						{"key": "ffprobe.max_concurrent", "type": "number", "label": "FFprobe 最大并发"},
					},
				},
				{
					"key":   "adult",
					"label": "Adult / NSFW",
					"items": []gin.H{
						{"key": "adult.enabled", "type": "toggle"},
						{"key": "adult.require_pin", "type": "toggle"},
						{"key": "adult.pin", "type": "text"},
					},
				},
			},
		})
	}
}

// schedulerTriggerHandler is the alternate path for /admin/scheduler/:name/run.
func schedulerTriggerHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !triggerSchedulerJob(c, svc, c.Param("name")) {
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"ok": true, "message": "任务已在后台触发"})
	}
}

// ─── SSE ticket store ───────────────────────────────────────────────────────
//
// The Vue UI's SSE event stream wants a one-time signed ticket so the
// EventSource (which can't set Authorization headers) can authenticate.
// We don't expose the SSE stream itself yet, but we persist short-lived
// tickets keyed to the user so the upstream consumer keeps working.

type ticket struct {
	userID  string
	expires time.Time
}

var (
	ticketStore   = map[string]ticket{}
	ticketStoreMu sync.Mutex
)

func newTicket(userID string) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	t := hex.EncodeToString(buf)
	ticketStoreMu.Lock()
	defer ticketStoreMu.Unlock()
	ticketStore[t] = ticket{userID: userID, expires: time.Now().Add(60 * time.Second)}
	// GC expired tickets opportunistically.
	for k, v := range ticketStore {
		if time.Now().After(v.expires) {
			delete(ticketStore, k)
		}
	}
	return t
}

func systemEventsTicketHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		c.JSON(http.StatusOK, gin.H{"ticket": newTicket(toString(uid))})
	}
}
