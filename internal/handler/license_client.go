package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type licenseClient struct {
	baseURL    string
	hmacSecret string
	httpClient *http.Client
}

func newLicenseClient(ctx context.Context, svc *service.Container) (*licenseClient, error) {
	baseURL, _ := svc.Repo.Setting.Get(ctx, licenseServerURLSetting)
	if strings.TrimSpace(baseURL) == "" {
		baseURL = svc.Cfg.License.ServerURL
	}
	secret, _ := svc.Repo.Setting.Get(ctx, licenseHMACSecretSetting)
	if strings.TrimSpace(secret) == "" {
		secret = svc.Cfg.License.HMACSecret
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("license server url not configured")
	}
	return &licenseClient{
		baseURL:    baseURL,
		hmacSecret: strings.TrimSpace(secret),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (c *licenseClient) post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *licenseClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *licenseClient) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var er struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &er)
		if er.Message != "" {
			return fmt.Errorf("license server: %s", er.Message)
		}
		if er.Error != "" {
			return fmt.Errorf("license server: %s", er.Error)
		}
		return fmt.Errorf("license server http %d", resp.StatusCode)
	}
	return json.Unmarshal(data, out)
}

func (c *licenseClient) verifySigned(resp *licenseServerSignedResp) error {
	if c.hmacSecret == "" {
		return nil
	}
	unsigned := struct {
		Valid         bool    `json:"valid"`
		LicenseType   string  `json:"license_type"`
		ExpiryDate    *string `json:"expiry_date"`
		MaxDevices    int     `json:"max_devices"`
		MaxUsers      *int    `json:"max_users"`
		DaysRemaining *int    `json:"days_remaining"`
		NextHeartbeat string  `json:"next_heartbeat"`
	}{
		Valid:         resp.Valid,
		LicenseType:   resp.LicenseType,
		ExpiryDate:    resp.ExpiryDate,
		MaxDevices:    resp.MaxDevices,
		MaxUsers:      resp.MaxUsers,
		DaysRemaining: resp.DaysRemaining,
		NextHeartbeat: resp.NextHeartbeat,
	}
	payload, err := json.Marshal(unsigned)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(c.hmacSecret))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(resp.Signature)) {
		if c.verifyLegacySigned(*resp) {
			resp.LegacySignature = true
			return nil
		}
		return errors.New("license server signature verification failed")
	}
	return nil
}

func (c *licenseClient) verifyLegacySigned(resp licenseServerSignedResp) bool {
	unsigned := struct {
		Valid         bool    `json:"valid"`
		LicenseType   string  `json:"license_type"`
		ExpiryDate    *string `json:"expiry_date"`
		MaxDevices    int     `json:"max_devices"`
		DaysRemaining *int    `json:"days_remaining"`
		NextHeartbeat string  `json:"next_heartbeat"`
	}{
		Valid:         resp.Valid,
		LicenseType:   resp.LicenseType,
		ExpiryDate:    resp.ExpiryDate,
		MaxDevices:    resp.MaxDevices,
		DaysRemaining: resp.DaysRemaining,
		NextHeartbeat: resp.NextHeartbeat,
	}
	payload, err := json.Marshal(unsigned)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.hmacSecret))
	_, _ = mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(resp.Signature))
}
