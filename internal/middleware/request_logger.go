package middleware

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const playerAPIRequestContextKey = "player_api_request"

// MarkPlayerAPIRequest 标记播放器兼容 API，供全局请求日志补充脱敏后的请求信息。
func MarkPlayerAPIRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(playerAPIRequestContextKey, true)
		c.Next()
	}
}

// RequestLogger logs one structured line per request.
//
// 健康检查与静态资源的成功请求被跳过：healthcheck 每 30s 一次、SPA 静态
// 文件每页几十个请求，全部记 INFO 会让日志在几小时内膨胀到几十 MB，
// 在 Docker json-file 日志驱动下白白消耗磁盘 IO。
func RequestLogger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.Request.URL.Path
		status := c.Writer.Status()
		if status < 400 {
			if path == "/api/health" || strings.HasPrefix(path, "/assets/") ||
				path == "/favicon.ico" || path == "/favicon.svg" {
				return
			}
		}
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("dur", time.Since(start)),
			zap.String("ip", c.ClientIP()),
		}
		if c.GetBool(playerAPIRequestContextKey) {
			fields = append(fields,
				zap.Bool("player_api", true),
				zap.Any("headers", sanitizedPlayerHeaders(c.Request.Header)),
				zap.Any("query", sanitizedPlayerValues(c.Request.URL.Query())),
			)
		}
		log.Info("http", fields...)
	}
}

func sanitizedPlayerHeaders(header http.Header) map[string][]string {
	out := make(map[string][]string, len(header))
	for key, values := range header {
		if sensitivePlayerRequestField(key) {
			out[key] = []string{"[redacted]"}
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	return out
}

func sanitizedPlayerValues(values url.Values) map[string][]string {
	out := make(map[string][]string, len(values))
	for key, items := range values {
		if sensitivePlayerRequestField(key) {
			out[key] = []string{"[redacted]"}
			continue
		}
		out[key] = append([]string(nil), items...)
	}
	return out
}

func sensitivePlayerRequestField(name string) bool {
	normalized := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name))
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "cookie") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "signature") ||
		strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "deviceid") ||
		strings.Contains(normalized, "devicename") ||
		strings.HasSuffix(normalized, "apikey") || normalized == "referer"
}
