package service

import (
	"errors"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestPlayProfileVerifyPIN(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	service := NewPlayProfileService(zap.NewNop(), repository.New(db))
	profile, err := service.Create(t.Context(), PlayProfileInput{
		UserID:     "user-1",
		Name:       "成人模式",
		AllowAdult: true,
		RequirePIN: true,
		PIN:        "1234",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.VerifyPIN(t.Context(), profile.ID, "user-1", "0000"); !errors.Is(err, ErrPlayProfilePINInvalid) {
		t.Fatalf("wrong PIN error = %v", err)
	}
	if _, err := service.VerifyPIN(t.Context(), profile.ID, "user-2", "1234"); !errors.Is(err, ErrPlayProfileForbidden) {
		t.Fatalf("wrong owner error = %v", err)
	}
	if verified, err := service.VerifyPIN(t.Context(), profile.ID, "user-1", "1234"); err != nil || verified.ID != profile.ID {
		t.Fatalf("verify PIN got profile=%v err=%v", verified, err)
	}
}

func TestPlayProfileCreateRequiresPINWhenEnabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.PlayProfile{}); err != nil {
		t.Fatal(err)
	}
	service := NewPlayProfileService(zap.NewNop(), repository.New(db))
	if _, err := service.Create(t.Context(), PlayProfileInput{
		UserID:     "user-1",
		Name:       "锁定模式",
		RequirePIN: true,
	}); err == nil {
		t.Fatal("expected PIN-required profile create to fail without PIN")
	}
}
