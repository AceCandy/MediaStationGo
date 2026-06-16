package handler

import (
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestLicenseStatusMaxUsersUsesLicensedLimit(t *testing.T) {
	maxUsers := 25
	state := service.LicenseActivationState{Valid: true, MaxUsers: &maxUsers}

	if got := licenseStatusMaxUsers(state); got != maxUsers {
		t.Fatalf("expected licensed max users %d, got %#v", maxUsers, got)
	}
}

func TestLicenseStatusMaxUsersAllowsUnlimited(t *testing.T) {
	state := service.LicenseActivationState{Valid: true, UnlimitedUsers: true}

	if got := licenseStatusMaxUsers(state); got != nil {
		t.Fatalf("expected unlimited max users to be nil, got %#v", got)
	}
}

func TestLicenseStatusMaxUsersFallsBackToOpenSourceLimit(t *testing.T) {
	state := service.LicenseActivationState{}

	if got := licenseStatusMaxUsers(state); got != service.OpenSourceUserLimit {
		t.Fatalf("expected open-source max users %d, got %#v", service.OpenSourceUserLimit, got)
	}
}

func TestApplyLicenseStatusReflectsEditedLimitAndClearsExpiry(t *testing.T) {
	maxUsers := 60
	licenseType := "subscription"
	state := service.LicenseActivationState{
		Valid:          true,
		LicenseType:    "enterprise",
		ExpiryDate:     "2026-01-01",
		MaxDevices:     2,
		UnlimitedUsers: true,
	}

	applyLicenseStatus(&state, licenseServerStatusResp{
		Valid:          true,
		LicenseType:    &licenseType,
		ExpiryDate:     nil,
		MaxDevices:     5,
		MaxUsers:       &maxUsers,
		UnlimitedUsers: false,
		DeviceName:     "Edited Device",
	}, "device-1")

	if !state.Valid || state.LicenseType != "subscription" || state.ExpiryDate != "" || state.MaxDevices != 5 {
		t.Fatalf("status fields were not fully refreshed: %+v", state)
	}
	if state.MaxUsers == nil || *state.MaxUsers != 60 || state.UnlimitedUsers {
		t.Fatalf("user limit was not refreshed from status: %+v", state)
	}
	if state.DeviceID != "device-1" || state.DeviceName != "Edited Device" {
		t.Fatalf("device fields were not refreshed: %+v", state)
	}
}

func TestApplyLicenseStatusReflectsUnlimitedUsers(t *testing.T) {
	maxUsers := 30
	state := service.LicenseActivationState{Valid: true, MaxUsers: &maxUsers}

	applyLicenseStatus(&state, licenseServerStatusResp{
		Valid:          true,
		MaxUsers:       nil,
		UnlimitedUsers: true,
	}, "device-1")

	if state.MaxUsers != nil || !state.UnlimitedUsers {
		t.Fatalf("unlimited status should clear previous finite user limit: %+v", state)
	}
}
