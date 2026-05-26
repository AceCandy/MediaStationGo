// Package handler — download manager endpoints.
package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type addDownloadReq struct {
	URL      string `json:"url" binding:"required"`
	SavePath string `json:"save_path"`
}

// resolvePTDownloadURL 把站点搜索结果里的"详情/获取签名"URL 解析成 qb 能直接
// 拉到 .torrent 文件的真实下载 URL。
//
// 链路：
//
//	1. 拿 URL 的 host，到 sites 表里找 base_url 同源的站点。
//	2. 如果站点的 type 是已知 PT 框架（mteam/nexusphp/unit3d/...），
//	   就用对应适配器的 GetDownloadURL，传入从 URL 里 parse 出来的 id。
//	3. 任一步失败都直接返回原 URL，让 qb 自己去拉（保持向后兼容）。
//
// 这一步存在的意义：M-Team 等站点的搜索结果里 download_url 是
// /api/torrent/genDlToken?id=xxx，需要带 x-api-key 才能调用，qb 自己
// 是没法识别这种 PT 专属端点的。
func resolvePTDownloadURL(ctx context.Context, svc *service.Container, raw string, log *zap.Logger) string {
	if raw == "" || svc == nil || svc.Site == nil {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	host := strings.ToLower(u.Host)

	// 找一个 host 匹配的已配置站点。
	sites, err := svc.Site.List(ctx)
	if err != nil || len(sites) == 0 {
		return raw
	}
	var matched *model.Site
	for i := range sites {
		su, err := url.Parse(sites[i].URL)
		if err != nil || su.Host == "" {
			continue
		}
		if strings.EqualFold(su.Host, host) || strings.HasSuffix(host, "."+strings.ToLower(su.Host)) {
			matched = &sites[i]
			break
		}
	}
	if matched == nil {
		return raw
	}

	// 抽取 torrent id：?id=xxx 是 PT 站普遍写法。
	id := u.Query().Get("id")
	if id == "" {
		return raw
	}

	adapter := service.GetAdapterForType(matched.Type)
	if adapter == nil {
		return raw
	}

	cfg := service.SiteConfig{
		Name:       matched.Name,
		Type:       matched.Type,
		URL:        strings.TrimRight(matched.URL, "/"),
		AuthType:   matched.AuthType,
		Cookie:     matched.Cookie,
		APIKey:     matched.APIKey,
		AuthHeader: matched.AuthHeader,
		UserAgent:  matched.UserAgent,
		Timeout:    time.Duration(matched.Timeout) * time.Second,
		UseProxy:   matched.UseProxy,
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}

	resolveCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	resolved, err := adapter.GetDownloadURL(resolveCtx, cfg, id)
	if err != nil || resolved == "" {
		log.Warn("resolve PT download URL failed (using raw URL)",
			zap.String("site", matched.Name),
			zap.String("type", matched.Type),
			zap.String("raw", raw),
			zap.Error(err))
		return raw
	}
	log.Info("resolved PT download URL",
		zap.String("site", matched.Name),
		zap.String("from", raw),
		zap.String("to", resolved))
	return resolved
}

func addDownloadHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req addDownloadReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uid, _ := c.Get(middleware.CtxUserID)
		// 把站点搜索 URL 转换成真实可下载 URL（M-Team 走 genDlToken 等）。
		realURL := resolvePTDownloadURL(c.Request.Context(), svc, req.URL, svc.Log)
		t, err := svc.Downloads.AddDownload(c.Request.Context(), uid.(string), realURL, req.SavePath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		svc.Audit.Record(c.Request.Context(), uid.(string), "download.add", realURL, c.ClientIP(), "")
		c.JSON(http.StatusOK, t)
	}
}

func listDownloadsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, live, err := svc.Downloads.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"tasks":    rows,
			"torrents": live,
		})
	}
}

func deleteDownloadHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		hash := c.Param("hash")
		withFiles := c.Query("delete_files") == "true"
		if err := svc.Downloads.Delete(c.Request.Context(), hash, withFiles); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func reloadDownloadConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Downloads.ReloadConfig(c.Request.Context()); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}
