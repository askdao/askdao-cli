// [INPUT]: 依赖 encoding/json + net/http + context + time + 标准库 fmt/io/errors
// [OUTPUT]: 对外提供
//   - DeviceFlow{} client struct + NewDeviceFlow(serverURL, clientName)
//   - DeviceFlow.Start(ctx) (*DeviceCodeResponse, error)
//   - DeviceFlow.Poll(ctx, deviceCode) (*TokenResponse, error)
//   - DeviceFlow.PollUntilApproved(ctx, deviceCode, interval, deadline) (*TokenResponse, error)
//   - ErrAuthorizationPending / ErrExpiredToken / ErrAccessDenied / ErrAlreadyConsumed
//     sentinel errors so callers (cmd/askdao/auth.go) can branch on them.
//
// [POS]: internal/auth — askdao-cli 侧的 OAuth 2.0 Device Code Flow 客户端
//
//	(RFC 8628)；只懂 conductor 这一个对端，HTTP shape 与 conductor
//	app/api/cli_auth.py 严格镜像。命令层 cmd/askdao/auth.go 编排：
//	Start → 打印 user_code + 打开浏览器 → PollUntilApproved → Save.
//	不持久化任何状态（credentials.go 才负责落盘）；purely a stateless
//	HTTP client + 错误码翻译器。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DeviceCodeResponse mirrors conductor's _StartDeviceResponse (cli_auth.py).
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// TokenResponse mirrors the success body of POST /device/token.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	UserID      string `json:"user_id"`
	UserEmail   string `json:"user_email"`
}

// OAuth error codes the polling loop must branch on. Stable sentinels so
// cmd/askdao/auth.go does string-free comparisons.
var (
	ErrAuthorizationPending = errors.New("auth: authorization_pending")
	ErrExpiredToken         = errors.New("auth: expired_token")
	ErrAccessDenied         = errors.New("auth: access_denied")
	ErrAlreadyConsumed      = errors.New("auth: already_consumed")
	ErrInvalidDeviceCode    = errors.New("auth: invalid_device_code")
)

// DeviceFlow drives the device authorization flow against a single conductor.
// Construct one per `askdao auth login` invocation.
type DeviceFlow struct {
	ServerURL  string
	ClientName string
	HTTPClient *http.Client
}

// NewDeviceFlow constructs a flow ready to Start. serverURL is the conductor
// API host (e.g. "https://api.askdao.ai"); clientName is the User-Agent-ish
// string sent to /device so the web approval page can show "askdao-cli/0.1.0
// darwin/arm64" alongside the user_code.
func NewDeviceFlow(serverURL, clientName string) *DeviceFlow {
	return &DeviceFlow{
		ServerURL:  strings.TrimRight(serverURL, "/"),
		ClientName: clientName,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Start opens a new device flow and returns the codes + verification URI for
// the user to visit. Conductor responds with 201; anything else is a wrapped
// error so the caller can surface it verbatim.
func (d *DeviceFlow) Start(ctx context.Context) (*DeviceCodeResponse, error) {
	body, _ := json.Marshal(map[string]any{"client_name": d.ClientName})
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, d.ServerURL+"/api/v1/cli/auth/device", bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: start device flow: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf(
			"auth: start device flow: HTTP %d %s",
			resp.StatusCode, truncate(string(raw), 240),
		)
	}
	var out DeviceCodeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("auth: parse device response: %w", err)
	}
	if out.DeviceCode == "" || out.UserCode == "" {
		return nil, fmt.Errorf("auth: malformed device response: %s", truncate(string(raw), 200))
	}
	return &out, nil
}

// Poll redeems the device code if the user has approved. Returns
// ErrAuthorizationPending when the user has not approved yet (caller waits +
// retries); other typed errors when the flow has terminated unsuccessfully.
func (d *DeviceFlow) Poll(ctx context.Context, deviceCode string) (*TokenResponse, error) {
	body, _ := json.Marshal(map[string]string{"device_code": deviceCode})
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, d.ServerURL+"/api/v1/cli/auth/device/token", bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: poll device token: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		var ok TokenResponse
		if err := json.Unmarshal(raw, &ok); err != nil {
			return nil, fmt.Errorf("auth: parse token response: %w", err)
		}
		if ok.AccessToken == "" {
			return nil, fmt.Errorf("auth: malformed token response: %s", truncate(string(raw), 200))
		}
		return &ok, nil
	}

	// Map RFC 6749 §5.2 error bodies to typed sentinels so the caller's
	// polling loop can branch without string-matching.
	switch translateOAuthError(raw) {
	case "authorization_pending":
		return nil, ErrAuthorizationPending
	case "expired_token":
		return nil, ErrExpiredToken
	case "access_denied":
		return nil, ErrAccessDenied
	case "already_consumed":
		return nil, ErrAlreadyConsumed
	case "invalid_device_code":
		return nil, ErrInvalidDeviceCode
	}

	return nil, fmt.Errorf(
		"auth: poll device token: HTTP %d %s",
		resp.StatusCode, truncate(string(raw), 240),
	)
}

// PollUntilApproved loops Poll at the given interval until success or until
// the deadline passes. The deadline is computed by the caller from the
// DeviceCodeResponse.ExpiresIn so we never poll past server-side expiry.
//
// Returns ErrExpiredToken on timeout *only* if the server has not yet flipped
// to expired by the time we last polled (defensive). Otherwise propagates
// whatever Poll returned.
func (d *DeviceFlow) PollUntilApproved(
	ctx context.Context,
	deviceCode string,
	interval, deadline time.Duration,
) (*TokenResponse, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if deadline <= 0 {
		deadline = 15 * time.Minute
	}
	overall, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// First poll fires immediately so the user is not held back by the
	// initial tick; subsequent polls run on the ticker.
	for {
		tok, err := d.Poll(overall, deviceCode)
		if err == nil {
			return tok, nil
		}
		// Whether the deadline fired between polls (caught in the select
		// below) or *during* an in-flight Poll (which surfaces as a wrapped
		// context.DeadlineExceeded), the user-facing meaning is the same:
		// the login window has elapsed. Normalize both to ErrExpiredToken.
		if overall.Err() != nil && errors.Is(overall.Err(), context.DeadlineExceeded) {
			return nil, ErrExpiredToken
		}
		if !errors.Is(err, ErrAuthorizationPending) {
			return nil, err
		}
		select {
		case <-ticker.C:
			continue
		case <-overall.Done():
			if errors.Is(overall.Err(), context.DeadlineExceeded) {
				return nil, ErrExpiredToken
			}
			return nil, overall.Err()
		}
	}
}

// translateOAuthError parses an RFC 6749 §5.2 body shape:
//
//	{"detail": {"error": "<code>", "error_description": "..."}}
//
// (FastAPI wraps custom error dicts under .detail; we unwrap.) Returns
// "" when the body is not in that shape so the caller falls through to the
// generic HTTP error path.
func translateOAuthError(raw []byte) string {
	var wrapped struct {
		Detail struct {
			Error string `json:"error"`
		} `json:"detail"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Detail.Error != "" {
		return wrapped.Detail.Error
	}
	// Plain shape (some endpoints may emit without the FastAPI wrap):
	var plain struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &plain)
	return plain.Error
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
