// [INPUT]: context/errors/fmt/net·url/os/path·filepath/sync + gopkg.in/yaml.v3 + wails runtime；internal/{auth,deploy,deployflow,pipeline,recommender,types,webstudio}
// [OUTPUT]: App（Wails bound-method 宿主 + 登录/项目态）+ StudioOptions（注入 webstudio 数据与桌面回调）
// [POS]: cmd/askdao-studio 应用层 —— 桌面壳业务逻辑：登录(device flow)/扫描(选文件夹→pipeline.Run)/保存(写 yaml)/部署(deployflow.PackageSkills 单源 + deploy.Client)，全复用 internal 核心包
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/auth"
	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/deployflow"
	"github.com/askdao/askdao-cli/internal/pipeline"
	"github.com/askdao/askdao-cli/internal/recommender"
	"github.com/askdao/askdao-cli/internal/types"
	"github.com/askdao/askdao-cli/internal/webstudio"
)

const agentYAMLName = "askdao-agent.yml"

// App hosts the desktop session context, the in-flight device-login state, the
// scanned project dir + its StudioData, and the Wails-bound methods.
type App struct {
	ctx context.Context

	mu          sync.Mutex
	df          *auth.DeviceFlow
	deviceCode  string
	server      string
	dir         string                // scanned project dir ("" until a folder is scanned)
	currentData *webstudio.StudioData // served by OnSpec; placeholder until scan
}

// NewApp constructs the desktop App with a placeholder StudioData so the
// workbench renders before any folder is scanned.
func NewApp() *App { return &App{currentData: placeholderData()} }

// startup captures the Wails runtime context.
func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// StudioOptions builds the webstudio Options the desktop serves through the Wails
// AssetServer. OnSpec/OnScan drive the dynamic data (placeholder → scanned draft);
// OnSave/OnDeploy are the real disk-write + conductor-deploy; the auth callbacks
// register /api/auth/*. Desktop-only routes exist because these callbacks are set.
func (a *App) StudioOptions() webstudio.Options {
	return webstudio.Options{
		OnSpec:      a.spec,
		OnScan:      a.scan,
		OnSave:      a.save,
		OnDeploy:    a.deploy,
		OnAuthState: a.authState,
		OnLogin:     a.startLogin,
		OnLoginPoll: a.loginPoll,
		OnLogout:    a.logout,
	}
}

// spec returns the current StudioData (placeholder until a folder is scanned).
func (a *App) spec() *webstudio.StudioData {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.currentData
}

// scan opens a folder picker, runs the scan pipeline (LLM draft when logged in,
// else offline mock — same as the CLI), and swaps the placeholder for the
// scanned StudioData. A cancelled picker keeps the current data.
func (a *App) scan() (*webstudio.StudioData, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.reqCtx(), wailsruntime.OpenDialogOptions{
		Title: "Choose your project folder",
	})
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return a.spec(), nil
	}
	home, _ := os.UserHomeDir()
	res, err := pipeline.Run(a.reqCtx(), pipeline.Options{
		Root:      dir,
		AgentName: filepath.Base(dir),
		LLM:       a.llmClient(),
		HomeDir:   home,
	})
	if err != nil {
		return nil, err
	}
	if res.Recommendation == nil {
		return nil, errors.New("scan produced no draft")
	}
	spec := &res.Recommendation.Spec
	spec.Skills = res.AgentSkills // deterministic skills override (hard field, §9.13)
	spec.Capabilities = recommender.DefaultCapabilities(riskHints(res.Detection))
	spec.VaultHints = recommender.BuildVaultHints(res.Detection) // credential-only (hard field, §9.13)
	if spec.Metadata.ThemeColor == "" {
		spec.Metadata.ThemeColor = webstudio.DefaultThemeForCategory(spec.Metadata.Category)
	}
	if spec.Metadata.Avatar == "" {
		spec.Metadata.Avatar = webstudio.DefaultAvatarForCategory(spec.Metadata.Category)
	}
	data := webstudio.BuildStudioData(spec, res.Detection, "Anthropic Managed Agents", false)
	data.Desktop = true
	a.mu.Lock()
	a.dir, a.currentData = dir, data
	a.mu.Unlock()
	return data, nil
}

// save writes the edited spec to <dir>/askdao-agent.yml (syncing networking from
// mcp_servers, same as the CLI's writeAgentSpec).
func (a *App) save(spec *types.AgentSpec) error {
	a.mu.Lock()
	dir := a.dir
	a.mu.Unlock()
	if dir == "" {
		return errors.New("no project scanned yet — choose a folder first")
	}
	syncNetworking(spec)
	yml, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, agentYAMLName), yml, 0o644)
}

// deploy saves the spec, packages its skills via the single-source
// deployflow.PackageSkills, and POSTs to conductor. Assembly (read yaml + auth +
// deploy.Client) mirrors cmd/askdao's deployFromDir; typed deploy errors surface
// to the frontend dialog verbatim.
func (a *App) deploy(spec *types.AgentSpec) (*webstudio.DeployResult, error) {
	if err := a.save(spec); err != nil {
		return nil, err
	}
	a.mu.Lock()
	dir := a.dir
	a.mu.Unlock()
	agentYAML, err := os.ReadFile(filepath.Join(dir, agentYAMLName))
	if err != nil {
		return nil, err
	}
	skillZips, err := deployflow.PackageSkills(dir, spec)
	if err != nil {
		return nil, err
	}
	creds, err := auth.Load()
	if err != nil {
		return nil, errors.New("not logged in — sign in to AskDAO first")
	}
	cl := deploy.NewClient(creds.Server)
	cl.AuthToken = creds.AccessToken
	resp, err := cl.Deploy(a.reqCtx(), deploy.DeployInput{
		AgentYAML: agentYAML,
		HarnessID: "anthropic_managed_agents",
		SkillZips: skillZips,
	})
	if err != nil {
		return nil, err
	}
	verb := "Updated"
	if resp.Created {
		verb = "Created"
	}
	return &webstudio.DeployResult{
		Message:   fmt.Sprintf("%s agent %s", verb, resp.AgentID),
		GroupLink: resp.GroupLink,
		AgentID:   resp.AgentID,
		Created:   resp.Created,
	}, nil
}

// llmClient wires the recommend client: conductor when logged in, offline mock
// otherwise (so scanning works before login, same policy as the CLI).
func (a *App) llmClient() recommender.LLMClient {
	creds, err := auth.Load()
	if err != nil {
		return &recommender.MockClient{}
	}
	cl := recommender.NewConductorClient(creds.Server)
	cl.AuthToken = creds.AccessToken
	return cl
}

// --- login (device flow, 阶段3) -------------------------------------------------

// authState reports whether a credentials file is present (logged in) + the email.
func (a *App) authState() webstudio.AuthState {
	c, err := auth.Load()
	if err != nil {
		return webstudio.AuthState{LoggedIn: false}
	}
	return webstudio.AuthState{LoggedIn: true, Email: c.UserEmail}
}

// startLogin opens a device flow, opens the browser to the verification URL, and
// returns the user code for the panel; the frontend then polls loginPoll.
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
	openBrowser(resp.VerificationURIComplete)
	return webstudio.LoginChallenge{
		UserCode:        resp.UserCode,
		VerificationURL: resp.VerificationURIComplete,
	}, nil
}

// loginPoll does a single device-token poll. Pending → not-logged-in (frontend
// keeps polling); approved → persist credentials; terminal error clears the flow.
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

// logout deletes the local credentials.
func (a *App) logout() error { return auth.Delete() }

func (a *App) clearFlow() {
	a.mu.Lock()
	a.df, a.deviceCode = nil, ""
	a.mu.Unlock()
}

// reqCtx returns the Wails runtime context, or background if startup hasn't fired.
func (a *App) reqCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// --- helpers -------------------------------------------------------------------

// syncNetworking derives workspace.networking from mcp_servers (mirrors
// cmd/askdao/edit.go): allow_mcp_servers when any MCP is configured, allowed_hosts
// from each MCP server URL hostname (deduped, preserving pre-existing hosts).
func syncNetworking(spec *types.AgentSpec) {
	hasMCP := len(spec.MCPServers) > 0
	spec.Workspace.Networking.AllowMCPServers = hasMCP
	if !hasMCP {
		return
	}
	existing := map[string]bool{}
	for _, h := range spec.Workspace.Networking.AllowedHosts {
		existing[h] = true
	}
	for _, srv := range spec.MCPServers {
		if srv.URL == "" {
			continue
		}
		u, err := url.Parse(srv.URL)
		if err != nil || u.Hostname() == "" {
			continue
		}
		if !existing[u.Hostname()] {
			spec.Workspace.Networking.AllowedHosts = append(spec.Workspace.Networking.AllowedHosts, u.Hostname())
			existing[u.Hostname()] = true
		}
	}
}

func riskHints(det *types.Detection) types.DetectedToolRiskHints {
	if det == nil {
		return types.DetectedToolRiskHints{}
	}
	return det.DetectedToolRiskHints
}

// placeholderData is the pre-scan StudioData so the workbench renders (empty
// draft) with the folder-picker prompt; replaced by scan().
func placeholderData() *webstudio.StudioData {
	s := &types.AgentSpec{}
	s.Metadata.Name = "askdao-studio"
	d := webstudio.BuildStudioData(s, nil, "Anthropic Managed Agents", false)
	d.Desktop = true
	d.NeedsScan = true
	return d
}

// openBrowser best-effort opens url in the default browser (macOS open / Windows
// start / Linux xdg-open). Failure is non-fatal — the login panel also shows the URL.
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
