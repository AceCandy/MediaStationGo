package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	OpenSourceUserLimit = 20
	LicensedUserLimit   = 100

	LicenseSettingActivation = "license.activation"
)

type LicenseActivationState struct {
	Valid         bool   `json:"valid"`
	LicenseType   string `json:"license_type,omitempty"`
	ExpiryDate    string `json:"expiry_date,omitempty"`
	MaxDevices    int    `json:"max_devices,omitempty"`
	DaysRemaining *int   `json:"days_remaining,omitempty"`
	NextHeartbeat string `json:"next_heartbeat,omitempty"`
	DeviceID      string `json:"device_id,omitempty"`
	DeviceName    string `json:"device_name,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

func LicensedMaxUsers(ctx context.Context, repos *repository.Container) int64 {
	if LicenseActive(ctx, repos) {
		return LicensedUserLimit
	}
	return OpenSourceUserLimit
}

func LicenseActive(ctx context.Context, repos *repository.Container) bool {
	if repos == nil || repos.Setting == nil {
		return false
	}
	raw, err := repos.Setting.Get(ctx, LicenseSettingActivation)
	if err != nil || raw == "" {
		return false
	}
	var state LicenseActivationState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return false
	}
	return state.Valid && !licenseExpired(state.ExpiryDate)
}

func licenseExpired(expiry string) bool {
	if expiry == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, expiry); err == nil {
			return time.Now().After(t)
		}
	}
	return false
}
