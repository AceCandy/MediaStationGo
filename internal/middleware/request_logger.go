package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const (
	playerAPIRequestContextKey      = "player_api_request"
	playerAPIRequestBodyContextKey  = "player_api_request_body"
	playerAPIResponseBodyContextKey = "player_api_response_body"
)

const (
	maxPlayerRequestValueRunes  = 4096
	maxPlayerRequestJSONBytes   = 64 * 1024
	playerRequestBodyTruncated  = "[truncated: request body exceeds 64 KiB]"
	playerResponseBodyTruncated = "[truncated: response body exceeds 64 KiB]"
	playerPlainTextRedacted     = "[redacted: non-JSON body contains sensitive data]"
)

type PlayerRequestRecorder func(context.Context, *model.PlayerRequestLog) error

// MarkPlayerAPIRequest 标记播放器兼容 API，供全局请求日志补充脱敏后的请求信息。
func MarkPlayerAPIRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(playerAPIRequestContextKey, true)
		writer := &playerErrorResponseWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		if c.Request.Body != nil {
			original := c.Request.Body
			body, _ := io.ReadAll(io.LimitReader(original, maxPlayerRequestJSONBytes+1))
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), original))
			defer original.Close()
			c.Set(playerAPIRequestBodyContextKey, sanitizedPlayerBody(body))
		}
		c.Next()
		c.Set(playerAPIResponseBodyContextKey, writer.sanitizedBody())
	}
}

// playerErrorResponseWriter 只保留失败响应的有限正文，不缓冲正常媒体数据。
type playerErrorResponseWriter struct {
	gin.ResponseWriter
	body      bytes.Buffer
	capturing bool
	truncated bool
}

func (w *playerErrorResponseWriter) WriteHeader(statusCode int) {
	if !w.ResponseWriter.Written() {
		w.capturing = statusCode >= http.StatusBadRequest
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *playerErrorResponseWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *playerErrorResponseWriter) WriteString(data string) (int, error) {
	w.capture([]byte(data))
	return w.ResponseWriter.WriteString(data)
}

func (w *playerErrorResponseWriter) capture(data []byte) {
	if !w.capturing || w.truncated || len(data) == 0 {
		return
	}
	remaining := maxPlayerRequestJSONBytes - w.body.Len()
	if len(data) > remaining {
		w.truncated = true
		return
	}
	_, _ = w.body.Write(data)
}

func (w *playerErrorResponseWriter) sanitizedBody() string {
	if w.truncated {
		return playerResponseBodyTruncated
	}
	return sanitizedPlayerPayload(w.body.Bytes(), playerResponseBodyTruncated)
}

// RequestLogger logs one structured line per request.
//
// 健康检查与静态资源的成功请求被跳过：healthcheck 每 30s 一次、SPA 静态
// 文件每页几十个请求，全部记 INFO 会让日志在几小时内膨胀到几十 MB，
// 在 Docker json-file 日志驱动下白白消耗磁盘 IO。
func RequestLogger(log *zap.Logger, recordPlayerRequest PlayerRequestRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now().UTC()
		c.Next()
		path := c.Request.URL.Path
		status := c.Writer.Status()
		if status < 400 {
			if path == "/api/health" || strings.HasPrefix(path, "/assets/") ||
				path == "/favicon.ico" || path == "/favicon.svg" {
				return
			}
		}
		duration := time.Since(start)
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("dur", duration),
			zap.String("ip", c.ClientIP()),
		}
		if c.GetBool(playerAPIRequestContextKey) {
			headers := sanitizedPlayerHeaders(c.Request.Header)
			query := sanitizedPlayerValues(c.Request.URL.Query())
			fields = append(fields,
				zap.Bool("player_api", true),
				zap.Any("headers", headers),
				zap.Any("query", query),
			)
			if recordPlayerRequest != nil {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 3*time.Second)
				err := recordPlayerRequest(ctx, &model.PlayerRequestLog{
					RequestedAt: start, Method: c.Request.Method, Route: c.FullPath(), Status: status,
					DurationMS: duration.Milliseconds(), IP: c.ClientIP(), Body: c.GetString(playerAPIRequestBodyContextKey),
					ResponseBody: c.GetString(playerAPIResponseBodyContextKey),
					PathParams:   sanitizedPlayerParams(c.Params), Headers: headers, Query: query,
				})
				cancel()
				if err != nil {
					log.Warn("persist player request log failed", zap.Error(err))
				}
			}
		}
		log.Info("http", fields...)
	}
}

func sanitizedPlayerBody(body []byte) string {
	return sanitizedPlayerPayload(body, playerRequestBodyTruncated)
}

func sanitizedPlayerPayload(body []byte, truncatedMarker string) string {
	if len(body) > maxPlayerRequestJSONBytes {
		return truncatedMarker
	}
	if len(body) == 0 {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return sanitizedPlayerPlainText(body)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return sanitizedPlayerPlainText(body)
	}
	encoded, _ := json.Marshal(sanitizedPlayerJSON(value))
	if len(encoded) > maxPlayerRequestJSONBytes {
		return truncatedMarker
	}
	return string(encoded)
}

func sanitizedPlayerPlainText(body []byte) string {
	text := strings.ToValidUTF8(string(body), "�")
	normalized := strings.NewReplacer("-", "", "_", "", " ", "", "\t", "").Replace(strings.ToLower(text))
	for _, marker := range []string{"token", "authorization", "cookie", "password", "secret", "signature", "credential", "deviceid", "devicename", "apikey", "referer"} {
		if strings.Contains(normalized, marker) {
			return playerPlainTextRedacted
		}
	}
	return text
}

func sanitizedPlayerJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if sensitivePlayerRequestField(key) {
				typed[key] = "[redacted]"
			} else {
				typed[key] = sanitizedPlayerJSON(item)
			}
		}
	case []any:
		for i, item := range typed {
			typed[i] = sanitizedPlayerJSON(item)
		}
	}
	return value
}

func sanitizedPlayerHeaders(header http.Header) map[string][]string {
	return sanitizedPlayerMap(map[string][]string(header))
}

func sanitizedPlayerValues(values url.Values) map[string][]string {
	return sanitizedPlayerMap(map[string][]string(values))
}

func sanitizedPlayerParams(params gin.Params) map[string][]string {
	values := make(map[string][]string, len(params))
	for _, param := range params {
		values[param.Key] = append(values[param.Key], param.Value)
	}
	return sanitizedPlayerMap(values)
}

func sanitizedPlayerMap(values map[string][]string) map[string][]string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string][]string, len(values))
	truncated := false
	for _, key := range keys {
		items := values[key]
		if sensitivePlayerRequestField(key) {
			out[key] = []string{"[redacted]"}
		} else {
			clean := make([]string, len(items))
			for i, item := range items {
				clean[i], truncated = truncatePlayerRequestValue(item, truncated)
			}
			out[key] = clean
		}
		encoded, _ := json.Marshal(out)
		if len(encoded) > maxPlayerRequestJSONBytes-64 {
			delete(out, key)
			truncated = true
			break
		}
	}
	if truncated {
		out["_truncated"] = []string{"true"}
	}
	return out
}

func truncatePlayerRequestValue(value string, alreadyTruncated bool) (string, bool) {
	runes := []rune(value)
	if len(runes) <= maxPlayerRequestValueRunes {
		return value, alreadyTruncated
	}
	const marker = "...[truncated]"
	return string(runes[:maxPlayerRequestValueRunes-len([]rune(marker))]) + marker, true
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
