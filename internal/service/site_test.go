package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestSiteUpdateKeepsSecretsWhenPatchIsBlank(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Site{}); err != nil {
		t.Fatal(err)
	}
	svc := NewSiteService(zap.NewNop(), &repository.Container{DB: db}, "")
	site := &model.Site{
		Name:     "M-Team",
		Type:     "mteam",
		URL:      "https://api.m-team.cc",
		AuthType: "api_key",
		APIKey:   "token-123",
		Enabled:  true,
	}
	if err := svc.Create(context.Background(), site); err != nil {
		t.Fatal(err)
	}

	if err := svc.Update(context.Background(), site.ID, map[string]any{
		"url":     "https://api.m-team.cc/",
		"api_key": "",
		"cookie":  "",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FindByID(context.Background(), site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "token-123" {
		t.Fatalf("APIKey = %q, want original token", got.APIKey)
	}
	if got.URL != "https://api.m-team.cc" {
		t.Fatalf("URL = %q, want trimmed URL", got.URL)
	}
}
