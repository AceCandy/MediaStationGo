package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestRequestLoggerSanitizesPlayerAPIRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.InfoLevel)
	router := gin.New()
	var rows []*model.PlayerRequestLog
	router.Use(RequestLogger(zap.New(core), func(_ context.Context, row *model.PlayerRequestLog) error {
		rows = append(rows, row)
		return nil
	}))
	for _, prefix := range []string{"/emby", ""} {
		group := router.Group(prefix, MarkPlayerAPIRequest())
		group.GET("/Users/:id/Items", func(c *gin.Context) { c.Status(http.StatusOK) })
	}

	for _, path := range []string{"/emby/Users/user-1/Items", "/Users/user-1/Items"} {
		req := httptest.NewRequest(http.MethodGet, path+"?Fields=People,ProviderIds&IncludeItemTypes=Movie&api_key=query-secret&X-Emby-Token=query-token", nil)
		req.Header.Set("User-Agent", "Infuse/8")
		req.Header.Set("X-Emby-Client", "Infuse")
		req.Header.Set("X-Emby-Token", "header-secret")
		req.Header.Set("Authorization", "Bearer bearer-secret")
		req.Header.Set("X-Api-Key", "custom-api-secret")
		req.Header.Set("Cookie", "msgo_access_token=cookie-secret")
		req.Header.Set("X-Emby-Device-Id", "device-secret")
		router.ServeHTTP(httptest.NewRecorder(), req)
	}

	if logs.Len() != 2 {
		t.Fatalf("logs = %d, want 2", logs.Len())
	}
	if len(rows) != 2 {
		t.Fatalf("persisted rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row.Route != "/Users/:id/Items" && row.Route != "/emby/Users/:id/Items" {
			t.Fatalf("route = %q", row.Route)
		}
		if !reflect.DeepEqual(row.PathParams["id"], []string{"user-1"}) || row.Status != http.StatusOK {
			t.Fatalf("row = %#v", row)
		}
	}
	for _, entry := range logs.All() {
		context := entry.ContextMap()
		if context["player_api"] != true {
			t.Fatalf("player_api = %#v, want true", context["player_api"])
		}
		query := context["query"].(map[string][]string)
		if !reflect.DeepEqual(query["Fields"], []string{"People,ProviderIds"}) ||
			!reflect.DeepEqual(query["IncludeItemTypes"], []string{"Movie"}) ||
			!reflect.DeepEqual(query["api_key"], []string{"[redacted]"}) ||
			!reflect.DeepEqual(query["X-Emby-Token"], []string{"[redacted]"}) {
			t.Fatalf("query = %#v", query)
		}
		headers := context["headers"].(map[string][]string)
		if !reflect.DeepEqual(headers["User-Agent"], []string{"Infuse/8"}) ||
			!reflect.DeepEqual(headers["X-Emby-Client"], []string{"Infuse"}) {
			t.Fatalf("headers = %#v", headers)
		}
		for _, key := range []string{"X-Emby-Token", "Authorization", "X-Api-Key", "Cookie", "X-Emby-Device-Id"} {
			if !reflect.DeepEqual(headers[key], []string{"[redacted]"}) {
				t.Fatalf("header %s = %#v, want redacted", key, headers[key])
			}
		}
	}
}

func TestRequestLoggerCapturesPlayerBodyWithoutChangingRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		storedBody string
	}{
		{
			name:       "sanitized json",
			body:       `{"ItemId":"item-1","PositionTicks":300000000,"DeviceId":"device-secret","Nested":{"Password":"password-secret"}}`,
			storedBody: `{"DeviceId":"[redacted]","ItemId":"item-1","Nested":{"Password":"[redacted]"},"PositionTicks":300000000}`,
		},
		{name: "plain text", body: "plain body", storedBody: "plain body"},
		{name: "invalid utf8", body: string([]byte{0xff}), storedBody: "�"},
		{name: "json encoding expansion", body: `"` + strings.Repeat("&", 11_000) + `"`, storedBody: playerRequestBodyTruncated},
		{name: "oversized", body: strings.Repeat("x", maxPlayerRequestJSONBytes+1), storedBody: playerRequestBodyTruncated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			var row *model.PlayerRequestLog
			router.Use(RequestLogger(zap.NewNop(), func(_ context.Context, recorded *model.PlayerRequestLog) error {
				row = recorded
				return nil
			}))
			group := router.Group("/emby", MarkPlayerAPIRequest())
			group.POST("/Sessions/Playing/Progress", func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				if err != nil || string(body) != tt.body {
					t.Fatalf("handler body = %q, err = %v", body, err)
				}
				c.Status(http.StatusNoContent)
			})

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/emby/Sessions/Playing/Progress", strings.NewReader(tt.body))
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
			}
			if row == nil || row.Body != tt.storedBody {
				t.Fatalf("stored body = %q, want %q", row.Body, tt.storedBody)
			}
		})
	}
}

func TestRequestLoggerCapturesOnlySanitizedPlayerErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		storedBody string
	}{
		{
			name:       "sanitized error",
			status:     http.StatusInternalServerError,
			body:       `{"error":"database unavailable","token":"response-secret"}`,
			storedBody: `{"error":"database unavailable","token":"[redacted]"}`,
		},
		{name: "sanitized plain-text error", status: http.StatusBadRequest, body: "token=response-secret", storedBody: playerPlainTextRedacted},
		{name: "successful response", status: http.StatusOK, body: `{"token":"response-secret"}`},
		{name: "redirect response", status: http.StatusFound, body: "redirect body"},
		{
			name:       "oversized error",
			status:     http.StatusBadGateway,
			body:       strings.Repeat("x", maxPlayerRequestJSONBytes+1),
			storedBody: playerResponseBodyTruncated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			var row *model.PlayerRequestLog
			router.Use(RequestLogger(zap.NewNop(), func(_ context.Context, recorded *model.PlayerRequestLog) error {
				row = recorded
				return nil
			}))
			group := router.Group("/emby", MarkPlayerAPIRequest())
			group.GET("/error", func(c *gin.Context) {
				c.Data(tt.status, "application/json", []byte(tt.body))
			})

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/emby/error", nil))
			if response.Code != tt.status || response.Body.String() != tt.body {
				t.Fatalf("response changed: status=%d body=%q", response.Code, response.Body.String())
			}
			if row == nil || row.ResponseBody != tt.storedBody {
				t.Fatalf("stored response body = %q, want %q", row.ResponseBody, tt.storedBody)
			}
		})
	}
}

func TestSensitivePlayerRequestField(t *testing.T) {
	for _, name := range []string{
		"token", "api_key", "apiKey", "ApiKey", "X-Api-Key",
		"X-Emby-Token", "X-MediaBrowser-Token",
		"Authorization", "X-Emby-Authorization", "X-MediaBrowser-Authorization",
		"Cookie", "Referer", "X-Emby-Device-Id", "X-Emby-Device-Name",
	} {
		if !sensitivePlayerRequestField(name) {
			t.Errorf("%s should be sensitive", name)
		}
	}
	for _, name := range []string{"Fields", "IncludeItemTypes", "User-Agent", "X-Emby-Client"} {
		if sensitivePlayerRequestField(name) {
			t.Errorf("%s should remain visible", name)
		}
	}
}

func TestRequestLoggerDoesNotAttachDetailsToRegularAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.InfoLevel)
	router := gin.New()
	router.Use(RequestLogger(zap.New(core), nil))
	router.GET("/api/health/details", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/health/details?debug=true", nil))

	context := logs.All()[0].ContextMap()
	if _, ok := context["headers"]; ok {
		t.Fatalf("regular API log contains headers: %#v", context)
	}
	if _, ok := context["query"]; ok {
		t.Fatalf("regular API log contains query: %#v", context)
	}
}

func TestRequestLoggerPersistenceFailureDoesNotChangeResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestLogger(zap.NewNop(), func(context.Context, *model.PlayerRequestLog) error {
		return errors.New("database unavailable")
	}))
	group := router.Group("/emby", MarkPlayerAPIRequest())
	group.GET("/System/Info", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/emby/System/Info", nil))
	if response.Code != http.StatusOK || response.Body.String() != "{\"ok\":true}" {
		t.Fatalf("response changed: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSanitizedPlayerValuesTruncatesOversizedMetadata(t *testing.T) {
	values := url.Values{"Fields": {strings.Repeat("界", maxPlayerRequestValueRunes+10)}}
	out := sanitizedPlayerValues(values)
	if got := len([]rune(out["Fields"][0])); got != maxPlayerRequestValueRunes {
		t.Fatalf("value runes = %d, want %d", got, maxPlayerRequestValueRunes)
	}
	if !reflect.DeepEqual(out["_truncated"], []string{"true"}) {
		t.Fatalf("truncation marker = %#v", out["_truncated"])
	}
}
