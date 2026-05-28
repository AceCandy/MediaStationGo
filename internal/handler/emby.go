// Package handler — Emby/Jellyfin compatibility shim.
//
// 路由挂在 /emby/* 和根路径下双前缀。Infuse / Yamby / Hills /
// Senplayer / Kodi 这类客户端会自动尝试 /System/Info 与 /emby/System/Info
// 两种 URL，我们都接住。
package handler

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// embyError 返回 Emby 风格的错误（顶层 Code/Message）。
func embyError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"Code": status, "Message": msg})
}

// embyUserID 从中间件中获取 user id。Emby auth middleware 写入 CtxUserID。
func embyUserID(c *gin.Context) string {
	if uid, ok := c.Get(middleware.CtxUserID); ok {
		if s, ok := uid.(string); ok {
			return s
		}
	}
	return ""
}

// ─── System ──────────────────────────────────────────────────────────────────

func embySystemInfoHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, embyWithRequestAddress(c, svc.Emby.SystemInfo()))
	}
}

func embySystemInfoPublicHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, embyWithRequestAddress(c, svc.Emby.SystemInfoPublic()))
	}
}

func embyRequestBaseURL(c *gin.Context) string {
	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		if c.Request != nil && c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	if comma := strings.Index(proto, ","); comma >= 0 {
		proto = strings.TrimSpace(proto[:comma])
	}

	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" && c.Request != nil {
		host = strings.TrimSpace(c.Request.Host)
	}
	if host == "" {
		return ""
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func embyWithRequestAddress(c *gin.Context, payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload)+2)
	for key, value := range payload {
		out[key] = value
	}
	if address := embyRequestBaseURL(c); address != "" {
		out["LocalAddress"] = address
		out["WanAddress"] = address
	}
	return out
}

func embySystemEndpointHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"IsLocal":     true,
			"IsInNetwork": true,
		})
	}
}

func embyPingHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Emby/Jellyfin 期望 plain text "Emby Server"
		c.String(http.StatusOK, "Emby Server")
	}
}

// ─── Users / Auth ────────────────────────────────────────────────────────────

type embyAuthByNameReq struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
	Password string `json:"Password"`
}

func parseEmbyAuthByNameReq(c *gin.Context) (embyAuthByNameReq, error) {
	req := embyAuthByNameReq{}
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "json") {
		var body map[string]any
		if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
			return req, err
		}
		req.Username = firstStringFromMap(body, "Username", "username", "Name", "name")
		req.Pw = firstStringFromMap(body, "Pw", "pw")
		req.Password = firstStringFromMap(body, "Password", "password")
	}

	if req.Username == "" || (req.Pw == "" && req.Password == "") {
		_ = c.Request.ParseForm()
		if req.Username == "" {
			req.Username = firstFormValue(c, "Username", "username", "Name", "name")
		}
		if req.Pw == "" {
			req.Pw = firstFormValue(c, "Pw", "pw")
		}
		if req.Password == "" {
			req.Password = firstFormValue(c, "Password", "password")
		}
	}

	if req.Username == "" {
		req.Username = firstQueryValue(c, "Username", "username", "Name", "name")
	}
	if req.Pw == "" {
		req.Pw = firstQueryValue(c, "Pw", "pw")
	}
	if req.Password == "" {
		req.Password = firstQueryValue(c, "Password", "password")
	}
	return req, nil
}

func firstStringFromMap(body map[string]any, keys ...string) string {
	if len(body) == 0 {
		return ""
	}
	for _, key := range keys {
		if value, ok := body[key]; ok {
			if s, ok := value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func firstFormValue(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if values, ok := c.Request.PostForm[key]; ok && len(values) > 0 {
			if value := strings.TrimSpace(values[0]); value != "" {
				return value
			}
		}
	}
	return ""
}

func firstQueryValue(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			return value
		}
	}
	return ""
}

// embyAuthByNameHandler 处理 POST /Users/AuthenticateByName。
//
// 这是 Emby 客户端登录的唯一入口（Infuse / Yamby / Hills 等都走这里）。
// 用户名+密码 → 调用我们已有的 AuthService.Login → 返回 AccessToken + User。
func embyAuthByNameHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		req, err := parseEmbyAuthByNameReq(c)
		if err != nil {
			embyError(c, http.StatusBadRequest, "invalid body")
			return
		}
		password := req.Pw
		if password == "" {
			password = req.Password
		}
		if strings.TrimSpace(req.Username) == "" || password == "" {
			embyError(c, http.StatusBadRequest, "missing username or password")
			return
		}
		resp, err := svc.Auth.Login(c.Request.Context(), req.Username, password)
		if err != nil {
			embyError(c, http.StatusUnauthorized, err.Error())
			return
		}
		userPayload, _ := svc.Emby.FindUser(c.Request.Context(), resp.User.ID)
		c.JSON(http.StatusOK, gin.H{
			"AccessToken": resp.Tokens.AccessToken,
			"ServerId":    "mediastation-go-001",
			"User":        userPayload,
			"SessionInfo": gin.H{
				"Id":         resp.User.ID,
				"UserId":     resp.User.ID,
				"UserName":   resp.User.Username,
				"Client":     c.GetHeader("X-Emby-Client"),
				"DeviceId":   c.GetHeader("X-Emby-Device-Id"),
				"DeviceName": c.GetHeader("X-Emby-Device-Name"),
			},
		})
	}
}

func embyPublicUsersHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 公开用户列表（Emby Web 客户端登录页拉这个，列出可见用户）。
		users, err := svc.Emby.ListUsers(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusOK, []any{})
			return
		}
		// 公开版本只暴露 Id + Name，不包含 Policy。
		out := make([]map[string]any, 0, len(users))
		for _, u := range users {
			out = append(out, map[string]any{
				"Id":          u["Id"],
				"Name":        u["Name"],
				"ServerId":    u["ServerId"],
				"HasPassword": true,
			})
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyListUsersHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		users, err := svc.Emby.ListUsers(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, users)
	}
}

func embyMeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := embyUserID(c)
		if uid == "" {
			embyError(c, http.StatusUnauthorized, "not authenticated")
			return
		}
		u, err := svc.Emby.FindUser(c.Request.Context(), uid)
		if err != nil || u == nil {
			embyError(c, http.StatusNotFound, "user not found")
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

func embyGetUserByIDHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, err := svc.Emby.FindUser(c.Request.Context(), c.Param("userId"))
		if err != nil || u == nil {
			embyError(c, http.StatusNotFound, "user not found")
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

// ─── Views / MediaFolders ────────────────────────────────────────────────────

func embyViewsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := svc.Emby.Views(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// ─── Items ───────────────────────────────────────────────────────────────────

func parseEmbyItemsParams(c *gin.Context) service.ItemsParams {
	limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("StartIndex", "0"))
	uid := c.Param("userId")
	if uid == "" {
		uid = embyUserID(c)
	}
	splitOpt := func(s string) []string {
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return service.ItemsParams{
		UserID:           uid,
		ParentID:         c.Query("ParentId"),
		IDs:              splitOpt(c.Query("Ids")),
		SearchTerm:       c.Query("SearchTerm"),
		IncludeItemTypes: splitOpt(c.Query("IncludeItemTypes")),
		Recursive:        strings.EqualFold(c.Query("Recursive"), "true"),
		SortBy:           c.Query("SortBy"),
		SortOrder:        c.Query("SortOrder"),
		Limit:            limit,
		StartIndex:       offset,
	}
}

func embyItemsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := svc.Emby.Items(c.Request.Context(), parseEmbyItemsParams(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyItemByIDHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		out, err := svc.Emby.Item(c.Request.Context(), id, uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if out == nil {
			embyError(c, http.StatusNotFound, "item not found")
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyLatestItemsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "20"))
		out, err := svc.Emby.LatestItems(c.Request.Context(), uid, c.Query("ParentId"), limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyResumeItemsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "20"))
		out, err := svc.Emby.ResumeItems(c.Request.Context(), uid, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// ─── Images ──────────────────────────────────────────────────────────────────

// embyItemImageHandler 把 /Items/{id}/Images/Primary 等请求重定向到
// 我们的 /api/img 代理。Emby 客户端会自动追加 ?api_key=... 或 ?tag=...
// 我们只关心 id+type，从 media row 拉到 PosterURL/BackdropURL 后转成
// /api/img?url=... 重定向。
func embyItemImageHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		imgType := strings.ToLower(c.Param("type"))
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), id)
		if err != nil || m == nil {
			c.Status(http.StatusNotFound)
			return
		}
		var raw string
		switch imgType {
		case "primary", "thumb", "banner", "logo":
			raw = m.PosterURL
		case "backdrop", "art":
			raw = m.BackdropURL
		default:
			raw = m.PosterURL
		}
		if raw == "" {
			c.Status(http.StatusNotFound)
			return
		}
		// 直接重定向到 /api/img；image proxy 自己缓存 + 兜底 1×1 PNG。
		c.Redirect(http.StatusFound, "/api/img?url="+url.QueryEscape(raw))
	}
}

// ─── Playback ────────────────────────────────────────────────────────────────

func embyPlaybackInfoHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := svc.Emby.PlaybackInfo(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if out == nil {
			embyError(c, http.StatusNotFound, "not found")
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// embyVideoStreamHandler 是 GET /Videos/{id}/stream 的入口，
// 直接代理到我们的 /api/stream/{id}（同一个 ServeFile）。
func embyVideoStreamHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 直接调用 Stream service 写入 response
		err := svc.Stream.ServeFile(c.Writer, c.Request, c.Param("id"))
		if err != nil {
			c.Status(http.StatusNotFound)
		}
	}
}

// ─── 播放进度 / 收藏 / 已看 ────────────────────────────────────────────────

type embyPlayingReq struct {
	ItemId        string `json:"ItemId"`
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
		// 兼容 query 形式（一些客户端在 /Sessions/Playing/* 用 query）
		if req.ItemId == "" {
			req.ItemId = c.Query("ItemId")
		}
		if req.PositionTicks == 0 {
			req.PositionTicks, _ = strconv.ParseInt(c.Query("PositionTicks"), 10, 64)
		}
		if req.RunTimeTicks == 0 {
			req.RunTimeTicks, _ = strconv.ParseInt(c.Query("RunTimeTicks"), 10, 64)
		}
		if req.ItemId == "" {
			c.Status(http.StatusOK) // Emby 期望 2xx；不是关键操作
			return
		}
		_ = svc.Emby.RecordProgress(c.Request.Context(), uid, req.ItemId, req.PositionTicks, req.RunTimeTicks)
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Emby 期望返回 UserItemDataDto；最小可工作版本：echo Item 即可。
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
		out, _ := svc.Emby.Item(c.Request.Context(), mid, uid)
		if out != nil {
			c.JSON(http.StatusOK, out["UserData"])
			return
		}
		c.JSON(http.StatusOK, gin.H{"Played": played})
	}
}

// ─── Sessions / Branding 占位 ────────────────────────────────────────────────

func embySessionsHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, []any{})
	}
}

func embyBrandingConfigHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"LoginDisclaimer":     "",
			"CustomCss":           "",
			"SplashscreenEnabled": false,
		})
	}
}

func embyLocalizationOptionsHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, []map[string]any{
			{"Name": "简体中文", "Value": "zh-CN"},
			{"Name": "English", "Value": "en-US"},
		})
	}
}

// registerEmbyRoutes 在 r 上挂双前缀（"" + "/emby"）的 Emby 兼容路由。
func registerEmbyRoutes(r *gin.Engine, jwtSecret string, svc *service.Container) {
	for _, prefix := range []string{"/emby", ""} {
		grp := r.Group(prefix)

		// 公开端点
		for _, path := range []string{"/System/Info/Public", "/system/info/public"} {
			grp.GET(path, embySystemInfoPublicHandler(svc))
			grp.HEAD(path, embySystemInfoPublicHandler(svc))
		}
		for _, path := range []string{"/System/Info", "/system/info"} {
			grp.GET(path, embySystemInfoHandler(svc))
			grp.HEAD(path, embySystemInfoHandler(svc))
		}
		for _, path := range []string{"/System/Endpoint", "/system/endpoint"} {
			grp.GET(path, embySystemEndpointHandler(svc))
		}
		for _, path := range []string{"/System/Ping", "/system/ping"} {
			grp.GET(path, embyPingHandler(svc))
			grp.HEAD(path, embyPingHandler(svc))
			grp.POST(path, embyPingHandler(svc))
		}
		for _, path := range []string{"/Users/AuthenticateByName", "/users/authenticatebyname"} {
			grp.POST(path, embyAuthByNameHandler(svc))
		}
		for _, path := range []string{"/Users/Public", "/users/public"} {
			grp.GET(path, embyPublicUsersHandler(svc))
		}
		for _, path := range []string{"/Branding/Configuration", "/branding/configuration"} {
			grp.GET(path, embyBrandingConfigHandler(svc))
		}
		for _, path := range []string{"/Localization/Options", "/localization/options"} {
			grp.GET(path, embyLocalizationOptionsHandler(svc))
		}

		// 图片公开（Infuse 缓存 URL 时会丢 token）
		grp.GET("/Items/:id/Images/:type", embyItemImageHandler(svc))
		grp.GET("/Items/:id/Images/:type/:index", embyItemImageHandler(svc))
		grp.HEAD("/Items/:id/Images/:type", embyItemImageHandler(svc))

		// 鉴权后端点
		auth := grp.Group("", middleware.EmbyAuthRequired(jwtSecret))
		auth.GET("/Users/Me", embyMeHandler(svc))
		auth.GET("/Users", embyListUsersHandler(svc))
		auth.GET("/Users/:userId", embyGetUserByIDHandler(svc))
		auth.GET("/Users/:userId/Views", embyViewsHandler(svc))
		auth.GET("/Library/MediaFolders", embyViewsHandler(svc))

		auth.GET("/Items", embyItemsHandler(svc))
		auth.GET("/Users/:userId/Items", embyItemsHandler(svc))
		auth.GET("/Items/:id", embyItemByIDHandler(svc))
		auth.GET("/Users/:userId/Items/Latest", embyLatestItemsHandler(svc))
		auth.GET("/Users/:userId/Items/Resume", embyResumeItemsHandler(svc))

		auth.GET("/Items/:id/PlaybackInfo", embyPlaybackInfoHandler(svc))
		auth.POST("/Items/:id/PlaybackInfo", embyPlaybackInfoHandler(svc))

		auth.GET("/Videos/:id/stream", embyVideoStreamHandler(svc))
		auth.HEAD("/Videos/:id/stream", embyVideoStreamHandler(svc))
		auth.GET("/Videos/:id/stream.:container", embyVideoStreamHandler(svc))
		auth.HEAD("/Videos/:id/stream.:container", embyVideoStreamHandler(svc))
		auth.GET("/Videos/:id/original", embyVideoStreamHandler(svc))
		auth.GET("/Videos/:id/original.:container", embyVideoStreamHandler(svc))

		auth.POST("/Sessions/Playing", embyPlayingProgressHandler(svc))
		auth.POST("/Sessions/Playing/Progress", embyPlayingProgressHandler(svc))
		auth.POST("/Sessions/Playing/Stopped", embyPlayingProgressHandler(svc))

		auth.POST("/Users/:userId/FavoriteItems/:itemId", embyFavoriteHandler(svc, true))
		auth.DELETE("/Users/:userId/FavoriteItems/:itemId", embyFavoriteHandler(svc, false))
		auth.POST("/Users/:userId/PlayedItems/:itemId", embyMarkPlayedHandler(svc, true))
		auth.DELETE("/Users/:userId/PlayedItems/:itemId", embyMarkPlayedHandler(svc, false))

		auth.GET("/Sessions", embySessionsHandler(svc))
		auth.GET("/DisplayPreferences/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"Id": c.Param("id"), "CustomPrefs": gin.H{}})
		})
		auth.POST("/DisplayPreferences/:id", func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
	}
}
