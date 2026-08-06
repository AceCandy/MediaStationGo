package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

func TestProxyCloudResolvedLinkUsesHEADWithoutSyntheticRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamMethod, upstreamRange string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamMethod = r.Method
		upstreamRange = r.Header.Get("Range")
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "123456")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodHead, "/api/cloud/play/openlist?ref=movie", nil)
	core, observed := observer.New(zap.InfoLevel)

	proxyCloudResolvedLink(cloudPlaybackRequest{
		svc:           &service.Container{Log: zap.New(core)},
		c:             c,
		typ:           "openlist",
		ref:           "movie",
		resolveSource: "cache",
		link: &cloud.DirectLink{
			URL:   upstream.URL + "/movie.mp4?token=secret",
			Proxy: true,
		},
	})

	if upstreamMethod != http.MethodHead {
		t.Fatalf("upstream method = %q, want HEAD", upstreamMethod)
	}
	if upstreamRange != "" {
		t.Fatalf("upstream Range = %q, want empty", upstreamRange)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Length"); got != "123456" {
		t.Fatalf("Content-Length = %q, want full upstream length", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD response body length = %d, want 0", rec.Body.Len())
	}
	entries := observed.FilterMessage("cloud playback proxy finished").All()
	if len(entries) != 1 {
		t.Fatalf("proxy log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["playback_source"] != "remote_proxy" || fields["resolve_source"] != "cache" ||
		fields["target_path"] != "/movie.mp4" || fmt.Sprint(fields["target_query_keys"]) != "[token]" {
		t.Fatalf("unexpected proxy log fields: %#v", fields)
	}
	loggedTarget := fmt.Sprint(fields["target_scheme"], fields["target_host"], fields["target_path"], fields["target_query_keys"])
	if strings.Contains(loggedTarget, "secret") || fields["target_hash"] == "" {
		t.Fatalf("proxy log should hide query values and include target hash: %#v", fields)
	}
}
