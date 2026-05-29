package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newAuthTestServices(t *testing.T) (*repository.Container, *AuthService, *ProfileService, *PermissionService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserPermission{}, &model.RefreshToken{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.JWTSecret = "test-secret"
	log := zap.NewNop()
	permissions := NewPermissionService(log, repos)
	tokenSvc := NewTokenService(cfg, log, repos)
	auth := NewAuthService(cfg, log, repos, tokenSvc, permissions)
	profile := NewProfileService(log, repos)
	return repos, auth, profile, permissions
}

func TestRegisterRejectsMoreThanTwentyUsers(t *testing.T) {
	ctx := context.Background()
	repos, auth, _, _ := newAuthTestServices(t)
	for i := 0; i < MaxUsers; i++ {
		if err := repos.User.Create(ctx, &model.User{
			Username:     fmt.Sprintf("user-%02d", i),
			PasswordHash: "hash",
			Role:         "user",
			Tier:         "free",
		}); err != nil {
			t.Fatal(err)
		}
	}

	_, _, err := auth.Register(ctx, "overflow", "password")
	if !errors.Is(err, ErrUserLimitReached) {
		t.Fatalf("expected ErrUserLimitReached, got %v", err)
	}
}

func TestRegisterDefaultsAdultLibrariesHidden(t *testing.T) {
	_, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(context.Background(), "viewer", "password")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if !user.HideAdult {
		t.Fatal("new users should hide adult libraries by default")
	}
}

func TestDefaultPermissionsAreViewerOnly(t *testing.T) {
	perms := DefaultPermissions("user-1")
	if !perms.CanViewDashboard || !perms.CanPlayMedia || !perms.CanExternalPlayer {
		t.Fatal("viewer defaults must allow library viewing, playback, and external players")
	}
	if perms.CanManageDownloads || perms.CanManageSubscriptions || perms.CanManageFiles ||
		perms.CanEditMedia || perms.CanRescrape || perms.CanCaptureFrames ||
		perms.CanManageSites || perms.CanManageUsers || perms.CanManageStrm {
		t.Fatal("viewer defaults must not allow downloads, scraping, media edits, or file management")
	}
}

func TestAdminEffectivePermissionsAreAllGranted(t *testing.T) {
	ctx := context.Background()
	repos, _, _, permissions := newAuthTestServices(t)
	admin := &model.User{Username: "admin", PasswordHash: "hash", Role: "admin", Tier: "plus"}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}

	perms, err := permissions.Effective(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !perms.CanEditMedia || !perms.CanRescrape || !perms.CanUseAI ||
		!perms.CanCaptureFrames || !perms.CanManageUsers || !perms.CanAccessSettings {
		t.Fatal("admin effective permissions must grant every advanced capability")
	}
}

func TestDefaultAdminCannotBeDemoted(t *testing.T) {
	ctx := context.Background()
	repos, _, profile, _ := newAuthTestServices(t)
	admin := &model.User{Username: "admin", PasswordHash: "hash", Role: "admin", Tier: "plus"}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}

	_, err := profile.AdminUpdateRole(ctx, admin.ID, "user")
	if err == nil {
		t.Fatal("expected default admin demotion to be rejected")
	}
}
