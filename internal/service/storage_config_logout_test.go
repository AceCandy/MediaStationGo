package service

import "testing"

func TestStorageConfigLogoutClearsCloudCredentialsOnly(t *testing.T) {
	_, storage := newStorageUploadTestService(t)
	enabled := true
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server":          "http://openlist.test",
			"url":             "http://openlist.test/dav/",
			"username":        "user",
			"password":        "pass",
			"token":           "token",
			"timeout_seconds": "120",
			"force_302":       "true",
		},
		Enabled: &enabled,
	}); err != nil {
		t.Fatalf("save storage: %v", err)
	}

	view, err := storage.Logout(t.Context(), "openlist")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if view.Enabled {
		t.Fatal("storage should be disabled after logout")
	}
	for _, key := range []string{"username", "password", "token", "force_302", "force_proxy"} {
		if _, ok := view.Config[key]; ok {
			t.Fatalf("logout should clear %s, config = %#v", key, view.Config)
		}
	}
	if view.Config["server"] != "http://openlist.test" || view.Config["url"] != "http://openlist.test/dav/" || view.Config["timeout_seconds"] != "120" {
		t.Fatalf("logout should keep non-secret connection hints, config = %#v", view.Config)
	}
}
