// [INPUT]: 依赖 context/errors/os-exec/runtime/sync；internal/auth（device flow + credentials）、internal/webstudio（Options/BuildStudioData/AuthState/LoginChallenge）、internal/types
// [OUTPUT]: App（Wails bound-method 宿主 + 登录态）+ StudioOptions（注入 webstudio 数据与桌面回调）
// [POS]: cmd/askdao-studio 应用层 —— 阶段2 骨架 + 阶段3 device flow 登录接入；扫描/部署真实接线在后续阶段
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"

	"github.com/askdao/askdao-cli/internal/auth"
	"github.com/askdao/askdao-cli/internal/types"
	"github.com/askdao/askdao-cli/internal/webstudio"
)

// App hosts the desktop session context, the in-flight device-login state, and
// the Wails-bound methods.
type App struct {
	ctx context.Context

	mu         sync.Mutex
	df         *auth.DeviceFlow
	deviceCode string
	server     string
}

// NewApp constructs the desktop App.
func NewApp() *App { return &App{} }

// startup captures the Wails runtime context.
func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// StudioOptions builds the webstudio Options the desktop serves through the Wails
// AssetServer. Desktop=true flips on the desktop-only frontend blocks; the auth
// callbacks register /api/auth/* so the login panel works. 阶段2 skeleton still
// stubs OnSave/OnDeploy (scan/deploy wiring lands in later stages).
func (a *App) StudioOptions() webstudio.Options {
	data := webstudio.BuildStudioData(placeholderSpec(), nil, "Anthropic Managed Agents", false)
	data.Desktop = true
	return webstudio.Options{
		Data:   data,
		OnSave: func(*types.AgentSpec) error { return nil },
		OnDeploy: func(*types.AgentSpec) (*webstudio.DeployResult, error) {
			return &webstudio.DeployResult{Message: "Deploy not wired yet (阶段2 skeleton)."}, nil
		},
		OnAuthState: a.authState,
		OnLogin:     a.startLogin,
		OnLoginPoll: a.loginPoll,
		OnLogout:    a.logout,
	}
}

// authState reports whether a credentials file is present (logged in) + the email.
// Reuses internal/auth so a desktop login and a CLI login are interchangeable.
func (a *App) authState() webstudio.AuthState {
	c, err := auth.Load()
	if err != nil {
		return webstudio.AuthState{LoggedIn: false}
	}
	return webstudio.AuthState{LoggedIn: true, Email: c.UserEmail}
}

// startLogin opens a device flow, opens the browser to the verification URL, and
// returns the user code for the panel to display. The frontend then polls
// loginPoll until approved.
func (a *App) startLogin() (webstudio.LoginChallenge, error) {
	server := auth.DefaultServerURL
	df := auth.NewDeviceFlow(server, "AskDAO Studio")
	resp, err := df.Start(a.reqCtx())
	if err != nil {
		return webstudio.LoginChallenge{}, err
	}
	a.mu.Lock()
	a.df, a.deviceCode, a.server = df, resp.DeviceCode, server
	a.mu.Unlock()
	openBrowser(resp.VerificationURIComplete) // best-effort; panel also shows the URL
	return webstudio.LoginChallenge{
		UserCode:        resp.UserCode,
		VerificationURL: resp.VerificationURIComplete,
	}, nil
}

// loginPoll does a single device-token poll. Pending → {logged_in:false} so the
// frontend keeps polling; approved → persist credentials + {logged_in:true}; a
// terminal error (expired/denied) clears the flow and surfaces the error.
func (a *App) loginPoll() (webstudio.AuthState, error) {
	a.mu.Lock()
	df, dc, server := a.df, a.deviceCode, a.server
	a.mu.Unlock()
	if df == nil {
		return a.authState(), nil
	}
	tok, err := df.Poll(a.reqCtx(), dc)
	if errors.Is(err, auth.ErrAuthorizationPending) {
		return webstudio.AuthState{LoggedIn: false}, nil
	}
	if err != nil {
		a.clearFlow()
		return webstudio.AuthState{}, err
	}
	if serr := auth.Save(&auth.Credentials{
		Server:      server,
		UserID:      tok.UserID,
		UserEmail:   tok.UserEmail,
		AccessToken: tok.AccessToken,
	}); serr != nil {
		return webstudio.AuthState{}, serr
	}
	a.clearFlow()
	return webstudio.AuthState{LoggedIn: true, Email: tok.UserEmail}, nil
}

// logout deletes the local credentials (server-side revoke is a web-UI concern,
// same as the CLI's `auth logout`).
func (a *App) logout() error { return auth.Delete() }

func (a *App) clearFlow() {
	a.mu.Lock()
	a.df, a.deviceCode = nil, ""
	a.mu.Unlock()
}

// reqCtx returns the Wails runtime context, or a background context if startup
// has not fired yet (defensive — HTTP requests only arrive after the window is up).
func (a *App) reqCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// placeholderSpec is a minimal non-nil spec so BuildStudioData/studio.html render
// without a real scan. Replaced by the scanned draft once folder-drop lands.
func placeholderSpec() *types.AgentSpec {
	s := &types.AgentSpec{}
	s.Metadata.Name = "askdao-studio"
	return s
}

// openBrowser best-effort opens url in the default browser (macOS open / Windows
// start / Linux xdg-open). Failure is non-fatal — the panel also shows the URL.
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
