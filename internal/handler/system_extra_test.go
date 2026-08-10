package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSettingsSchemaIncludesRedirectResolvePrefixes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	schemaHandler(nil)(c)

	var response struct {
		Groups []struct {
			Key   string `json:"key"`
			Items []struct {
				Key  string `json:"key"`
				Type string `json:"type"`
			} `json:"items"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode settings schema: %v", err)
	}

	for _, group := range response.Groups {
		if group.Key != "general" {
			continue
		}
		for _, item := range group.Items {
			if item.Key == "playback.redirect_resolve_prefixes" {
				if item.Type != "textarea" {
					t.Fatalf("redirect resolve prefixes type = %q, want textarea", item.Type)
				}
				return
			}
		}
	}
	t.Fatal("redirect resolve prefixes setting is missing from the general schema")
}
