package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseEmbyAuthByNameReqAcceptsLowercaseJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", strings.NewReader(`{"username":"alice","password":"secret"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	req, err := parseEmbyAuthByNameReq(c)
	if err != nil {
		t.Fatalf("parseEmbyAuthByNameReq returned error: %v", err)
	}
	if req.Username != "alice" || req.Password != "secret" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestParseEmbyAuthByNameReqAcceptsFormBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", strings.NewReader("Username=bob&Pw=secret"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseEmbyAuthByNameReq(c)
	if err != nil {
		t.Fatalf("parseEmbyAuthByNameReq returned error: %v", err)
	}
	if req.Username != "bob" || req.Pw != "secret" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestEmbyWithRequestAddressUsesHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://192.168.1.4:18080/System/Info/Public", nil)

	payload := embyWithRequestAddress(c, map[string]any{"Id": "mediastation-go-001"})

	if payload["LocalAddress"] != "http://192.168.1.4:18080" {
		t.Fatalf("unexpected LocalAddress: %#v", payload["LocalAddress"])
	}
	if payload["WanAddress"] != "http://192.168.1.4:18080" {
		t.Fatalf("unexpected WanAddress: %#v", payload["WanAddress"])
	}
}

func TestEmbyWithRequestAddressHonorsForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/System/Info/Public", nil)
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	c.Request.Header.Set("X-Forwarded-Host", "media.example.test")

	payload := embyWithRequestAddress(c, map[string]any{"Id": "mediastation-go-001"})

	if payload["LocalAddress"] != "https://media.example.test" {
		t.Fatalf("unexpected LocalAddress: %#v", payload["LocalAddress"])
	}
}
