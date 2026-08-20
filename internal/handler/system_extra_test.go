package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSettingsSchemaIncludesPathMappingTextareas(t *testing.T) {
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

	wanted := map[string]bool{
		"playback.redirect_resolve_prefixes": false,
		"ffprobe.path_mappings":              false,
	}
	for _, group := range response.Groups {
		if group.Key != "general" {
			continue
		}
		for _, item := range group.Items {
			if _, ok := wanted[item.Key]; !ok {
				continue
			}
			if item.Type != "textarea" {
				t.Fatalf("%s type = %q, want textarea", item.Key, item.Type)
			}
			wanted[item.Key] = true
		}
	}
	for key, found := range wanted {
		if !found {
			t.Fatalf("%s setting is missing from the general schema", key)
		}
	}
}
