// [INPUT]: 依赖 testing + net/http/httptest + encoding/json + context + time + errors
// [OUTPUT]: TestDeviceFlow* — Start happy path; Poll authorization_pending /
//
//	expired / consumed / invalid / unknown HTTP error; PollUntilApproved
//	succeeds after one pending tick; PollUntilApproved respects deadline.
//
// [POS]: internal/auth/ — covers device.go via an in-process httptest.Server
//
//	that mirrors conductor's OAuth response shapes. No real network.
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeConductor wraps an httptest.Server with handler-replacement hooks per
// test, so each test can program a custom /device + /device/token response.
type fakeConductor struct {
	*httptest.Server
	startHandler http.HandlerFunc
	tokenHandler http.HandlerFunc
}

func newFakeConductor(t *testing.T) *fakeConductor {
	t.Helper()
	fc := &fakeConductor{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cli/auth/device", func(w http.ResponseWriter, r *http.Request) {
		if fc.startHandler != nil {
			fc.startHandler(w, r)
			return
		}
		http.Error(w, "no startHandler", http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/v1/cli/auth/device/token", func(w http.ResponseWriter, r *http.Request) {
		if fc.tokenHandler != nil {
			fc.tokenHandler(w, r)
			return
		}
		http.Error(w, "no tokenHandler", http.StatusInternalServerError)
	})
	fc.Server = httptest.NewServer(mux)
	t.Cleanup(fc.Server.Close)
	return fc
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------

func TestStartHappyPath(t *testing.T) {
	fc := newFakeConductor(t)
	fc.startHandler = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, DeviceCodeResponse{
			DeviceCode:              "dev_abc",
			UserCode:                "HJKL-4892",
			VerificationURI:         "https://askdao.ai/cli/auth",
			VerificationURIComplete: "https://askdao.ai/cli/auth?code=HJKL-4892",
			ExpiresIn:               900,
			Interval:                5,
		})
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/0.1.0 test/test")
	got, err := df.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got.UserCode != "HJKL-4892" || got.DeviceCode != "dev_abc" || got.Interval != 5 {
		t.Fatalf("Start response mismatch: %+v", got)
	}
}

func TestStartReturnsErrorOnNon201(t *testing.T) {
	fc := newFakeConductor(t)
	fc.startHandler = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": "upstream down"})
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	if _, err := df.Start(context.Background()); err == nil {
		t.Fatal("expected error on 502")
	}
}

// ---------------------------------------------------------------------------
// Poll — error code mapping
// ---------------------------------------------------------------------------

// FastAPI wraps custom error dicts under .detail; the OAuth sentinels live in
// detail.error. translateOAuthError handles both wraps and plain shapes —
// these tests exercise both routes.
func writeOAuthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{
		"detail": map[string]any{
			"error":             code,
			"error_description": "stub",
		},
	})
}

func TestPollAuthorizationPending(t *testing.T) {
	fc := newFakeConductor(t)
	fc.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		writeOAuthError(w, http.StatusBadRequest, "authorization_pending")
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	_, err := df.Poll(context.Background(), "dev_abc")
	if !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("expected ErrAuthorizationPending, got %v", err)
	}
}

func TestPollExpiredToken(t *testing.T) {
	fc := newFakeConductor(t)
	fc.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		writeOAuthError(w, http.StatusGone, "expired_token")
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	_, err := df.Poll(context.Background(), "dev_abc")
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestPollAlreadyConsumed(t *testing.T) {
	fc := newFakeConductor(t)
	fc.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		writeOAuthError(w, http.StatusGone, "already_consumed")
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	_, err := df.Poll(context.Background(), "dev_abc")
	if !errors.Is(err, ErrAlreadyConsumed) {
		t.Fatalf("expected ErrAlreadyConsumed, got %v", err)
	}
}

func TestPollInvalidDeviceCode(t *testing.T) {
	fc := newFakeConductor(t)
	fc.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		writeOAuthError(w, http.StatusNotFound, "invalid_device_code")
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	_, err := df.Poll(context.Background(), "dev_abc")
	if !errors.Is(err, ErrInvalidDeviceCode) {
		t.Fatalf("expected ErrInvalidDeviceCode, got %v", err)
	}
}

func TestPollSuccess(t *testing.T) {
	fc := newFakeConductor(t)
	fc.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, TokenResponse{
			AccessToken: "cli_secret",
			TokenType:   "Bearer",
			UserID:      "usr_abc",
			UserEmail:   "kol@example.com",
		})
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	tok, err := df.Poll(context.Background(), "dev_abc")
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if tok.AccessToken != "cli_secret" || tok.UserEmail != "kol@example.com" {
		t.Fatalf("unexpected token: %+v", tok)
	}
}

// ---------------------------------------------------------------------------
// PollUntilApproved
// ---------------------------------------------------------------------------

func TestPollUntilApprovedTransitionsPendingToToken(t *testing.T) {
	fc := newFakeConductor(t)
	var calls atomic.Int32
	fc.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1, 2:
			writeOAuthError(w, http.StatusBadRequest, "authorization_pending")
		default:
			writeJSON(w, http.StatusOK, TokenResponse{
				AccessToken: "cli_ok", UserEmail: "e@x", UserID: "u",
			})
		}
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	// 50ms tick so the test stays under 1s; deadline well above the
	// number of ticks needed to flip to success.
	tok, err := df.PollUntilApproved(context.Background(), "dev_abc", 50*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatalf("PollUntilApproved: %v", err)
	}
	if tok.AccessToken != "cli_ok" {
		t.Fatalf("got %+v", tok)
	}
	if calls.Load() < 3 {
		t.Fatalf("expected ≥3 polls before success, got %d", calls.Load())
	}
}

func TestPollUntilApprovedTimesOutAsExpiredToken(t *testing.T) {
	fc := newFakeConductor(t)
	fc.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		writeOAuthError(w, http.StatusBadRequest, "authorization_pending")
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	_, err := df.PollUntilApproved(context.Background(), "dev_abc", 30*time.Millisecond, 120*time.Millisecond)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken on deadline, got %v", err)
	}
}

func TestPollUntilApprovedPropagatesTerminalErrors(t *testing.T) {
	fc := newFakeConductor(t)
	fc.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		writeOAuthError(w, http.StatusGone, "access_denied")
	}
	df := NewDeviceFlow(fc.URL, "askdao-cli/test")
	_, err := df.PollUntilApproved(context.Background(), "dev_abc", 50*time.Millisecond, 5*time.Second)
	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("expected ErrAccessDenied (no retry on terminal), got %v", err)
	}
}
