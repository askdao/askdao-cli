// [INPUT]: context/errors/fmt/net·url/os/path·filepath/sync + gopkg.in/yaml.v3 + wails runtime；internal/{auth,chat,deploy,deployflow,pipeline,recommender,scanner,types,webstudio}
// [OUTPUT]: App（Wails bound-method 宿主 + 登录/项目态）+ StudioOptions（注入 webstudio 数据与桌面回调）
// [POS]: cmd/askdao-studio 应用层 —— 桌面壳业务逻辑：登录(device flow)/扫描(pick 选文件夹→run 跑管线,Stop 可 cancel)/保存(写 yaml)/部署(deployflow.PackageSkills 单源 + deploy.Client)/测试聊天(chat 流式转发 conductor /chat)/内嵌助手(assistant 流式转发官方 Studio 助手 agent,resolveOfficialAssistant 从 conductor /cli/config 读回 agent id + 缓存 + env override)/SKILL 校验+补全(skillValidate/skillFix)/外链桥接(openExternal)，全复用 internal 核心包
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
	"github.com/askdao/askdao-cli/internal/chat"
	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/deployflow"
	"github.com/askdao/askdao-cli/internal/pipeline"
	"github.com/askdao/askdao-cli/internal/recommender"
	"github.com/askdao/askdao-cli/internal/scanner"
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
	pendingDir  string                // folder picked but not yet scanned (pick → run)
	scanCancel  context.CancelFunc    // cancels the in-flight runScan; nil when idle

	studioAssistantID string // cached assistant agent id from /cli/config; "" until first successful fetch
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
		OnSpec:          a.spec,
		OnScanPick:      a.pickFolder,
		OnScanRun:       a.runScan,
		OnScanCancel:    a.cancelScan,
		OnSave:          a.save,
		OnDeploy:        a.deploy,
		OnAuthState:     a.authState,
		OnLogin:         a.startLogin,
		OnLoginPoll:     a.loginPoll,
		OnLogout:        a.logout,
		OnChat:          a.chat,
		OnAssistant:     a.assistant,
		OnOpenExternal:  a.openExternal,
		OnSkillValidate: a.skillValidate,
		OnSkillFix:      a.skillFix,
	}
}

// spec returns the current StudioData (placeholder until a folder is scanned).
func (a *App) spec() *webstudio.StudioData {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.currentData
}

// pickFolder opens the native folder picker and stores the choice as pending —
// the frontend shows the path + Stop button before runScan does the actual work.
// An empty dir means the user cancelled the dialog (runScan is then not called).
func (a *App) pickFolder() (string, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.reqCtx(), wailsruntime.OpenDialogOptions{
		Title: "Choose your project folder",
	})
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.pendingDir = dir
	a.mu.Unlock()
	return dir, nil
}

// runScan scans the pending folder under a per-scan cancellable context, so the
// frontend Stop button can abort syft / the conductor call mid-flight. On success
// it swaps in the scanned StudioData; a cancelled scan surfaces context.Canceled.
func (a *App) runScan() (*webstudio.StudioData, error) {
	a.mu.Lock()
	dir := a.pendingDir
	ctx, cancel := context.WithCancel(a.reqCtx())
	a.scanCancel = cancel
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.scanCancel = nil; a.mu.Unlock(); cancel() }()

	if dir == "" {
		return nil, errors.New("no folder picked yet")
	}
	home, _ := os.UserHomeDir()
	res, err := pipeline.Run(ctx, pipeline.Options{
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
	data := webstudio.BuildStudioData(spec, res.Detection, spec.PreferredHarness, false)
	data.Desktop = true
	data.ModelCatalog = a.modelCatalog(ctx, spec.PreferredHarness)
	data.Models = a.modelWhitelist(ctx)
	a.mu.Lock()
	a.dir, a.currentData = dir, data
	a.mu.Unlock()
	return data, nil
}

// cancelScan aborts the in-flight runScan (if any). Idempotent — a no-op when idle.
func (a *App) cancelScan() error {
	a.mu.Lock()
	c := a.scanCancel
	a.mu.Unlock()
	if c != nil {
		c()
	}
	return nil
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
	// harness 跟着 Studio 里选的模型走（spec.preferred_harness，cloud#84）；空 = conductor 默认
	resp, err := cl.Deploy(a.reqCtx(), deploy.DeployInput{
		AgentYAML: agentYAML,
		HarnessID: spec.PreferredHarness,
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
		Message:         fmt.Sprintf("%s agent %s", verb, resp.AgentID),
		GroupLink:       resp.GroupLink,
		AgentID:         resp.AgentID,
		Created:         resp.Created,
		ScheduleWarning: resp.ScheduleWarning,
	}, nil
}

// chat forwards one test-chat turn to conductor's /chat and streams each raw SSE
// frame to emit (webstudio's /api/chat handler re-wraps them for the frontend).
// Auth + base URL come from auth.Load(), same as deploy — so the KOL tests the
// agent they just deployed with the same cli_ token; agent_id is
// DeployResult.AgentID (ACL: owner==caller). No request timeout: the turn streams
// as long as the agent runs; ctx is webstudio's /api/chat request context, so the
// stream stops when the webview closes that fetch (e.g. the chat panel is dismissed).
func (a *App) chat(ctx context.Context, req webstudio.ChatRequest, emit func(raw []byte) error) error {
	creds, err := auth.Load()
	if err != nil {
		return errors.New("not logged in — sign in to AskDAO first")
	}
	cl := chat.NewClient(creds.Server)
	cl.AuthToken = creds.AccessToken
	return cl.Stream(ctx, chat.Request{
		Message:   req.Message,
		AgentID:   req.AgentID,
		SessionID: req.SessionID,
	}, emit)
}

// assistant forwards one desktop-assistant turn to the official Studio assistant
// agent and streams each raw SSE frame to emit — the same dumb-pipe shape as chat,
// only the agent differs: it's resolved Go-side (resolveOfficialAssistant), not
// chosen by the WebView, so the KOL always talks to the platform assistant. When
// not logged in or no official assistant is configured, it emits a single
// structured "unavailable" frame so the sidebar degrades to static help instead of
// silently chatting with the wrong agent (conductor's DM fallback picks the
// caller's own agent when the target isn't reachable).
func (a *App) assistant(ctx context.Context, req webstudio.AssistantRequest, emit func(raw []byte) error) error {
	creds, err := auth.Load()
	if err != nil {
		return emit([]byte(`{"type":"unavailable","message":"Sign in to AskDAO to use the assistant."}`))
	}
	agentID := a.resolveOfficialAssistant()
	if agentID == "" {
		return emit([]byte(`{"type":"unavailable","message":"The AI assistant isn't set up yet."}`))
	}
	msg := req.Message
	if req.Context != "" {
		msg = req.Context + "\n\n" + req.Message
	}
	cl := chat.NewClient(creds.Server)
	cl.AuthToken = creds.AccessToken
	return cl.Stream(ctx, chat.Request{
		Message:   msg,
		AgentID:   agentID,
		SessionID: req.SessionID,
	}, emit)
}

// resolveOfficialAssistant returns the agent_id of the platform's Studio
// assistant agent — the "brain" the sidebar chats with. The id lives server-side
// (conductor GET /cli/config), so swapping the assistant is a conductor redeploy,
// not a client re-release. Resolution order: ASKDAO_STUDIO_ASSISTANT_ID env (a
// dev override) → cached value → one lazy fetch from /cli/config (cached on
// success). An empty return degrades the sidebar to static help — never a
// wrong-agent chat (conductor's DM fallback would otherwise pick the caller's own
// agent). Fetch failures aren't cached, so a transient outage retries next turn.
func (a *App) resolveOfficialAssistant() string {
	if v := os.Getenv("ASKDAO_STUDIO_ASSISTANT_ID"); v != "" {
		return v
	}
	a.mu.Lock()
	cached := a.studioAssistantID
	a.mu.Unlock()
	if cached != "" {
		return cached
	}
	creds, err := auth.Load()
	if err != nil {
		return "" // not logged in → degrade to static help
	}
	id := recommender.FetchStudioAssistantID(a.reqCtx(), creds.Server, creds.AccessToken)
	if id != "" {
		a.mu.Lock()
		a.studioAssistantID = id
		a.mu.Unlock()
	}
	return id
}

// openExternal opens url in the system browser. The desktop webview ignores
// target=_blank / window.open, so studio.html routes external-link clicks (the
// group page) here via /api/open-external. Reuses openBrowser (same as login).
func (a *App) openExternal(url string) error {
	openBrowser(url)
	return nil
}

// skillValidate checks every custom_local skill in the current draft for a
// complete SKILL.md frontmatter (name + description — the fields PackageSkills
// requires at deploy time) and returns the per-skill result so the Skills step
// flags the incomplete ones before deploy, not after. It reads live from disk,
// so a KOL who edits a SKILL.md and re-validates sees the fresh state.
func (a *App) skillValidate() ([]webstudio.SkillValidation, error) {
	a.mu.Lock()
	dir, data := a.dir, a.currentData
	a.mu.Unlock()
	if dir == "" || data == nil {
		return nil, errors.New("no project scanned yet — choose a folder first")
	}
	var out []webstudio.SkillValidation
	for _, c := range data.SkillCandidates {
		if c.Builtin || c.Path == "" {
			continue // builtins carry no SKILL.md; only custom_local has one
		}
		skillDir, err := deployflow.ResolveSkillDir(dir, types.Skill{Path: c.Path, Scope: c.Scope})
		if err != nil {
			return nil, err
		}
		name, desc := scanner.ParseSkillFrontmatter(filepath.Join(skillDir, "SKILL.md"))
		out = append(out, webstudio.SkillValidation{
			Path:           c.Path,
			DirName:        filepath.Base(filepath.Clean(c.Path)),
			HasName:        name != "",
			HasDescription: desc != "",
		})
	}
	return out, nil
}

// skillFix writes name/description into a skill's SKILL.md frontmatter so the KOL
// clears a validation flag in place. The path MUST belong to a scanned skill
// candidate — never an arbitrary path from the request — so the write stays
// confined to the project (or its declared user-scope skills), the same boundary
// the assistant's file writes will honor.
func (a *App) skillFix(fix webstudio.SkillFix) error {
	a.mu.Lock()
	dir, data := a.dir, a.currentData
	a.mu.Unlock()
	if dir == "" || data == nil {
		return errors.New("no project scanned yet — choose a folder first")
	}
	var cand *webstudio.SkillCandidate
	for i := range data.SkillCandidates {
		if c := &data.SkillCandidates[i]; !c.Builtin && c.Path == fix.Path {
			cand = c
			break
		}
	}
	if cand == nil {
		return fmt.Errorf("unknown skill %q", fix.Path)
	}
	skillDir, err := deployflow.ResolveSkillDir(dir, types.Skill{Path: cand.Path, Scope: cand.Scope})
	if err != nil {
		return err
	}
	return deployflow.UpsertSkillFrontmatter(filepath.Join(skillDir, "SKILL.md"), fix.Name, fix.Description)
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

// modelCatalog fetches the conductor model-class catalog for the step-2
// selector, degrading to the bundled minimal fallback when logged out / offline
// (so the binary carries no concrete model ids).
func (a *App) modelCatalog(ctx context.Context, harness string) []types.ModelClassEntry {
	var url, token string
	if creds, err := auth.Load(); err == nil {
		url, token = creds.Server, creds.AccessToken
	}
	return recommender.FetchModelClassesOrFallback(ctx, url, token, harness)
}

// modelWhitelist fetches the unified `models[]` whitelist (both harnesses) for
// the step-2 dropdown; empty when logged out / offline → frontend falls back to
// the three-tier picker.
func (a *App) modelWhitelist(ctx context.Context) []types.ModelEntry {
	var url, token string
	if creds, err := auth.Load(); err == nil {
		url, token = creds.Server, creds.AccessToken
	}
	return recommender.FetchModelCatalogOrEmpty(ctx, url, token)
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
	d := webstudio.BuildStudioData(s, nil, "", false)
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
