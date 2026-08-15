package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseEmbyItemsParamsFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/emby/Users/user-1/Items?Fields=People,ProviderIds,MediaSources", nil)
	c.Params = gin.Params{{Key: "userId", Value: "user-1"}}

	params := parseEmbyItemsParams(c)
	want := []string{"People", "ProviderIds", "MediaSources"}
	if !reflect.DeepEqual(params.Fields, want) {
		t.Fatalf("Fields = %#v, want %#v", params.Fields, want)
	}
}
