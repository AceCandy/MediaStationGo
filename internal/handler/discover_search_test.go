package handler

import (
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverSearchRejectsInvalidInput(t *testing.T) {
	r := gin.New()
	r.POST("/search", discoverSearchHandler(&service.Container{}))
	for _, body := range []string{`{`, `{}`, `{"query":"test","kind":"person","page":1}`, `{"query":"test","kind":"movie","page":501}`, `{"query":"` + strings.Repeat("中", 101) + `","kind":"tv","page":1}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/search", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("status=%d want=400", w.Code)
		}
	}
}
