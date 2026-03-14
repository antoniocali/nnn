// Package cloud provides a typed HTTP client for the nnn.rocks API.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const DefaultAPIBase = "https://api.nnn.rocks"

// ── Sentinel errors ───────────────────────────────────────────────────────────

// Sentinel errors returned by PollToken.
var (
	ErrDevicePending = errors.New("authorization pending")
	ErrDeviceExpired = errors.New("device code expired")
)

// ErrUnauthorized is returned (wrapped) by any API call that receives HTTP
// 401 or 403. It signals that the stored token is missing or has expired.
var ErrUnauthorized = errors.New("unauthorized")

// ErrNetwork is returned (wrapped) by any API call that fails at the
// transport layer — no connection, DNS failure, timeout, etc.
var ErrNetwork = errors.New("network error")

// ClassifyError converts a cloud client error into a concise, user-facing
// message that differentiates between authentication and connectivity problems.
//
// Usage:
//
//	if err != nil {
//	    fmt.Fprintln(os.Stderr, cloud.ClassifyError(err))
//	}
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrUnauthorized) {
		return "sync failed: token expired — run `nnn auth login` to re-authenticate"
	}
	if errors.Is(err, ErrNetwork) {
		return "sync failed: network error — check your connection and try again"
	}
	return "sync failed: " + err.Error()
}

// Client is a minimal HTTP client for api.nnn.rocks.
type Client struct {
	base       string
	httpClient *http.Client
}

// New returns a Client targeting the nnn.rocks API.
func New() *Client {
	return &Client{
		base:       DefaultAPIBase,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// ── Response types ────────────────────────────────────────────────────────────

// DeviceCodeResponse is returned by POST /auth/device/code.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"` // seconds
	Interval        int    `json:"interval"`   // polling interval in seconds
}

// TokenResponse is returned by POST /auth/device/token once approved.
type TokenResponse struct {
	Token     string `json:"token"`
	UserEmail string `json:"user_email"`
}

// MeResponse is returned by GET /auth/me.
type MeResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// ── API calls ─────────────────────────────────────────────────────────────────

// DeviceCode initiates the device flow and returns the codes the user needs
// to authorize this CLI instance via the browser.
func (c *Client) DeviceCode(ctx context.Context) (DeviceCodeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/auth/device/code", http.NoBody)
	if err != nil {
		return DeviceCodeResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	var resp DeviceCodeResponse
	if err := c.do(req, &resp); err != nil {
		return DeviceCodeResponse{}, fmt.Errorf("device/code: %w", err)
	}
	return resp, nil
}

// PollToken asks the server whether the device code has been approved yet.
// Returns ErrDevicePending (HTTP 428) or ErrDeviceExpired (HTTP 410) as
// sentinel values so callers can implement the polling loop cleanly.
func (c *Client) PollToken(ctx context.Context, deviceCode string) (TokenResponse, error) {
	body, _ := json.Marshal(map[string]string{"device_code": deviceCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/auth/device/token", bytes.NewReader(body))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("device/token: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK: // 200
		var t TokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
			return TokenResponse{}, fmt.Errorf("device/token decode: %w", err)
		}
		return t, nil
	case http.StatusPreconditionRequired: // 428 — still pending user approval
		return TokenResponse{}, ErrDevicePending
	case http.StatusGone: // 410 — device code expired
		return TokenResponse{}, ErrDeviceExpired
	default:
		return TokenResponse{}, fmt.Errorf("device/token: unexpected status %d", resp.StatusCode)
	}
}

// Me calls GET /auth/me using the provided Bearer token and returns the
// authenticated user's id and email.
func (c *Client) Me(ctx context.Context, token string) (MeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/auth/me", http.NoBody)
	if err != nil {
		return MeResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	var me MeResponse
	if err := c.do(req, &me); err != nil {
		return MeResponse{}, fmt.Errorf("auth/me: %w", err)
	}
	return me, nil
}

// UserConfig mirrors models.UserConfig from nnn.rocks.
type UserConfig struct {
	Theme string `json:"theme"`
}

// GetConfig calls GET /auth/config and returns the user's stored config.
func (c *Client) GetConfig(ctx context.Context, token string) (UserConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/auth/config", http.NoBody)
	if err != nil {
		return UserConfig{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	var cfg UserConfig
	if err := c.do(req, &cfg); err != nil {
		return UserConfig{}, fmt.Errorf("auth/config get: %w", err)
	}
	return cfg, nil
}

// PatchConfig calls PATCH /auth/config with the provided fields and returns
// the updated config. Only non-nil fields are changed server-side.
func (c *Client) PatchConfig(ctx context.Context, token string, theme *string) (UserConfig, error) {
	body, err := json.Marshal(struct {
		Theme *string `json:"theme,omitempty"`
	}{Theme: theme})
	if err != nil {
		return UserConfig{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		c.base+"/auth/config", bytes.NewReader(body))
	if err != nil {
		return UserConfig{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	var cfg UserConfig
	if err := c.do(req, &cfg); err != nil {
		return UserConfig{}, fmt.Errorf("auth/config patch: %w", err)
	}
	return cfg, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

type apiError struct {
	Error string `json:"error"`
}

// do executes req, decodes a successful JSON body into dst, and returns a
// descriptive error for non-2xx responses.
// Transport-level failures are wrapped with ErrNetwork; HTTP 401/403 are
// wrapped with ErrUnauthorized so callers can distinguish them via errors.Is.
func (c *Client) do(req *http.Request, dst interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNetwork, err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: HTTP %d", ErrUnauthorized, resp.StatusCode)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiError
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error != "" {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiErr.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
