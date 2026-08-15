package middleware

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestLoggerSanitizesPlayerAPIRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.InfoLevel)
	router := gin.New()
	router.Use(RequestLogger(zap.New(core)))
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
	router.Use(RequestLogger(zap.New(core)))
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
