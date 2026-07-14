// [INPUT]: context/errors/fmt/net·url/os/path·filepath/sync/time + gopkg.in/yaml.v3 + wails runtime；internal/{auth,chat,deploy,deployflow,pipeline,recents,recommender,scanner,types,webstudio}
// [OUTPUT]: App（Wails bound-method 宿主 + 登录 + 多项目列表态 projects[]/currentIndex）+ Project（单项目内存态 dir/Data/Deploy/Full）+ StudioOptions（注入 webstudio 数据与桌面回调）
// [POS]: cmd/askdao-studio 应用层 —— 桌面壳业务逻辑：登录(device flow)/扫描(pick 选文件夹→run 跑管线,Stop 可 cancel)/多项目(projects[] MRU + switchProject 轻量 yaml 载入·rescanCurrent full scan·removeProject,NewApp 从 recents 播种 + touchRecents/recordDeploy 持久化)/保存(写 yaml)/部署(deployflow.PackageSkills 单源 + deploy.Client)/测试聊天(chat 流式转发 conductor /chat)/内嵌助手(assistant 流式转发官方 Studio 助手 agent,resolveOfficialAssistant 从 conductor /cli/config 读回 agent id + 缓存 + env override)/SKILL 校验+补全(skillValidate/skillFix)/外链桥接(openExternal)，全复用 internal 核心包
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
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/auth"
	"github.com/askdao/askdao-cli/internal/chat"
	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/deployflow"
	"github.com/askdao/askdao-cli/internal/pipeline"
	"github.com/askdao/askdao-cli/internal/recents"
	"github.com/askdao/askdao-cli/internal/recommender"
	"github.com/askdao/askdao-cli/internal/scanner"
	"github.com/askdao/askdao-cli/internal/types"
	"github.com/askdao/askdao-cli/internal/webstudio"
)

const agentYAMLName = "askdao-agent.yml"

// App hosts the desktop session context, the in-flight device-login state, the
// open project list + which one is current, and the Wails-bound methods.
type App struct {
	ctx context.Context

	mu           sync.Mutex
	df           *auth.DeviceFlow
	deviceCode   string
	server       string
	projects     []*Project         // MRU; front = most recently opened. Seeded from recents (Data=nil) at startup.
	currentIndex int                // index into projects; -1 = placeholder (no project open)
	pendingDir   string             // folder picked but not yet scanned (pick → run)
	scanCancel   context.CancelFunc // cancels the in-flight runScan; nil when idle

	studioAssistantID string // cached assistant agent id from /cli/config; "" until first successful fetch
}

// Project is one open project's in-memory state: its dir, the StudioData served
// to the workbench (nil until materialized — lazily via full scan or lightweight
// yaml parse), and its last-deploy record mirrored from recents.
type Project struct {
	Dir    string
	Data   *webstudio.StudioData
	Deploy *recents.DeployRecord
	Full   bool // Data came from a full pipeline scan (true) vs lightweight yaml parse (false)
}

// NewApp constructs the desktop App, seeding the project list from recents (paths
// only — each project's StudioData is materialized lazily on first open/switch).
// currentIndex = -1 means the workbench shows the placeholder until a project is
// scanned or switched to.
func NewApp() *App {
	a := &App{currentIndex: -1}
	if f, err := recents.Load(); err == nil {
		for _, e := range f.Projects {
			p := &Project{Dir: e.Dir}
			if e.Deploy != nil {
				d := *e.Deploy
				p.Deploy = &d
			}
			a.projects = append(a.projects, p)
		}
	}
	return a
}

// currentProjectLocked returns the current project, or nil at the placeholder.
// Caller must hold a.mu.
func (a *App) currentProjectLocked() *Project {
	if a.currentIndex < 0 || a.currentIndex >= len(a.projects) {
		return nil
	}
	return a.projects[a.currentIndex]
}

// findProjectLocked returns the project whose dir normalizes equal, or nil.
// Caller must hold a.mu.
func (a *App) findProjectLocked(dir string) *Project {
	nd := recents.Normalize(dir)
	for _, p := range a.projects {
		if recents.Normalize(p.Dir) == nd {
			return p
		}
	}
	return nil
}

// upsertProjectLocked makes dir the current project (front of MRU), replacing its
// Data if already present. Caller must hold a.mu.
func (a *App) upsertProjectLocked(dir string, data *webstudio.StudioData, full bool) {
	nd := recents.Normalize(dir)
	for i, p := range a.projects {
		if recents.Normalize(p.Dir) == nd {
			p.Data, p.Full = data, full
			a.projects = append(a.projects[:i], a.projects[i+1:]...)
			a.projects = append([]*Project{p}, a.projects...)
			a.currentIndex = 0
			return
		}
	}
	a.projects = append([]*Project{{Dir: nd, Data: data, Full: full}}, a.projects...)
	a.currentIndex = 0
}

// removeProjectLocked drops dir; if it was current, currentIndex becomes -1
// (placeholder) and the caller (removeProject) re-opens a sibling if any remain.
// Caller must hold a.mu.
func (a *App) removeProjectLocked(dir string) {
	nd := recents.Normalize(dir)
	for i, p := range a.projects {
		if recents.Normalize(p.Dir) == nd {
			a.projects = append(a.projects[:i], a.projects[i+1:]...)
			switch {
			case a.currentIndex == i:
				a.currentIndex = -1
			case a.currentIndex > i:
				a.currentIndex--
			}
			return
		}
	}
}

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
		OnProjectSwitch: a.switchProject,
		OnProjectRemove: a.removeProject,
		OnProjectRescan: a.rescanCurrent,
	}
}

// spec returns the StudioData for the current project (placeholder until one is
// scanned/switched to), with the fresh project-list summary layered on. It
// shallow-copies so the stored project.Data is never mutated by the summary set.
func (a *App) spec() *webstudio.StudioData {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := placeholderData()
	if p := a.currentProjectLocked(); p != nil && p.Data != nil {
		d = p.Data
	}
	dc := *d
	dc.Projects = a.projectSummariesLocked()
	return &dc
}

// projectSummariesLocked builds the switcher list from a.projects. Caller must
// hold a.mu. Name uses the materialized metadata.name when available, else the
// dir basename; deploy fields come from the recents-mirrored Deploy record.
func (a *App) projectSummariesLocked() []webstudio.ProjectSummary {
	if len(a.projects) == 0 {
		return nil
	}
	out := make([]webstudio.ProjectSummary, 0, len(a.projects))
	for i, p := range a.projects {
		s := webstudio.ProjectSummary{Dir: p.Dir, Current: i == a.currentIndex}
		if p.Data != nil && p.Data.Spec != nil && p.Data.Spec.Metadata.Name != "" {
			s.Name = p.Data.Spec.Metadata.Name
		} else {
			s.Name = filepath.Base(p.Dir)
		}
		if _, err := os.Stat(p.Dir); err != nil {
			s.Missing = true
		}
		if p.Deploy != nil {
			s.Deployed = true
			s.DeployedName = p.Deploy.MetadataName
			s.AgentID = p.Deploy.AgentID
			if p.Deploy.PreviousManagedVersion != nil {
				s.Version = fmt.Sprintf("v%d", *p.Deploy.PreviousManagedVersion+1)
			}
			if !p.Deploy.LastDeployedAt.IsZero() {
				s.LastDeployedAt = p.Deploy.LastDeployedAt.Format(time.RFC3339)
			}
		}
		out = append(out, s)
	}
	return out
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
	data := webstudio.BuildStudioData(spec, res.Detection, "Anthropic Managed Agents", false)
	data.Desktop = true
	data.ModelCatalog = a.modelCatalog(ctx)
	a.mu.Lock()
	a.upsertProjectLocked(dir, data, true /*full scan*/)
	a.mu.Unlock()
	a.touchRecents(dir, data.ProjectName)
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

// loadProjectLight parses <dir>/askdao-agent.yml into StudioData without running
// the pipeline (det=nil, Lightweight=true). The CLI's readSpec lives in another
// package main, so the desktop parses yaml itself. Lightweight signals collect()
// to preserve the loaded skills/mcp rather than rebuild them from empty candidates.
func (a *App) loadProjectLight(dir string) (*webstudio.StudioData, error) {
	raw, err := os.ReadFile(filepath.Join(dir, agentYAMLName))
	if err != nil {
		return nil, fmt.Errorf("no %s here — Re-scan to draft one", agentYAMLName)
	}
	var spec types.AgentSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", agentYAMLName, err)
	}
	if spec.Metadata.ThemeColor == "" {
		spec.Metadata.ThemeColor = webstudio.DefaultThemeForCategory(spec.Metadata.Category)
	}
	if spec.Metadata.Avatar == "" {
		spec.Metadata.Avatar = webstudio.DefaultAvatarForCategory(spec.Metadata.Category)
	}
	data := webstudio.BuildStudioData(&spec, nil, "Anthropic Managed Agents", true)
	data.Desktop = true
	data.Lightweight = true
	data.ModelCatalog = a.modelCatalog(a.reqCtx())
	return data, nil
}

// switchProject makes dir the current project. If already materialized its Data
// is reused; otherwise a lightweight yaml load. The frontend re-GETs /api/spec
// afterward to render the newly-current project.
func (a *App) switchProject(dir string) error {
	a.mu.Lock()
	p := a.findProjectLocked(dir)
	a.mu.Unlock()
	var (
		data *webstudio.StudioData
		full bool
	)
	if p != nil && p.Data != nil {
		data, full = p.Data, p.Full
	} else if d, err := a.loadProjectLight(dir); err != nil {
		return err
	} else {
		data = d
	}
	a.mu.Lock()
	a.upsertProjectLocked(dir, data, full)
	a.mu.Unlock()
	a.touchRecents(dir, data.ProjectName)
	return nil
}

// rescanCurrent runs a full pipeline scan on the current project (upgrading a
// lightweight load or refreshing candidates), reusing runScan via pendingDir.
func (a *App) rescanCurrent() (*webstudio.StudioData, error) {
	a.mu.Lock()
	p := a.currentProjectLocked()
	if p == nil {
		a.mu.Unlock()
		return nil, errors.New("no project open — choose a folder first")
	}
	a.pendingDir = p.Dir
	a.mu.Unlock()
	return a.runScan()
}

// removeProject drops dir from the project list + recents. Removing the current
// project falls back to the new MRU front (tab-close semantics) rather than the
// placeholder — only an emptied list drops to the "Open a project" folder picker.
// removeProjectLocked sets currentIndex=-1 when the removed project was current;
// when siblings remain we re-open the new front through switchProject so it gets
// materialized (loadProjectLight for a lazily-seeded sibling). A bare currentIndex
// reset is not enough: spec() renders the placeholder unless the project's Data is
// non-nil, so removing the current project would otherwise strand the user on the
// folder picker.
func (a *App) removeProject(dir string) error {
	a.mu.Lock()
	a.removeProjectLocked(dir)
	var fallbackDir string
	if a.currentIndex < 0 && len(a.projects) > 0 {
		fallbackDir = a.projects[0].Dir
	}
	a.mu.Unlock()

	if fallbackDir != "" {
		// Best-effort: if the sibling's yaml is gone/corrupt, switchProject errors
		// and we keep the placeholder — that project is unusable anyway, so the
		// folder picker is then the right prompt.
		if err := a.switchProject(fallbackDir); err != nil {
			fmt.Fprintf(os.Stderr, "askdao-studio: fall back to %s: %v\n", fallbackDir, err)
		}
	}

	f, _ := recents.Load()
	f.Remove(dir)
	if err := recents.Save(f); err != nil {
		fmt.Fprintf(os.Stderr, "askdao-studio: recents save: %v\n", err)
	}
	return nil
}

// save writes the edited spec to <dir>/askdao-agent.yml (syncing networking from
// mcp_servers, same as the CLI's writeAgentSpec).
func (a *App) save(spec *types.AgentSpec) error {
	a.mu.Lock()
	p := a.currentProjectLocked()
	a.mu.Unlock()
	if p == nil {
		return errors.New("no project open — choose a folder first")
	}
	dir := p.Dir
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
	p := a.currentProjectLocked()
	a.mu.Unlock()
	if p == nil {
		return nil, errors.New("no project open — choose a folder first")
	}
	dir := p.Dir
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
	rec := recents.DeployRecord{
		AgentID:                resp.AgentID,
		MetadataName:           spec.Metadata.Name,
		Created:                resp.Created,
		PreviousManagedVersion: resp.PreviousManagedVersion,
		GroupLink:              resp.GroupLink,
		AnthropicAgentID:       resp.AnthropicAgentID,
		LastDeployedAt:         time.Now().UTC(),
	}
	a.mu.Lock()
	p.Deploy = &rec
	a.mu.Unlock()
	a.recordDeploy(dir, rec)
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

// touchRecents records dir (with its display label) as the most-recently-opened
// project in recent-projects.json. Best-effort: a persistence failure is logged
// to stderr but never fails the scan/switch that triggered it — recents is a
// convenience cache, not authoritative state.
func (a *App) touchRecents(dir, label string) {
	f, _ := recents.Load()
	f.Touch(dir, label)
	if err := recents.Save(f); err != nil {
		fmt.Fprintf(os.Stderr, "askdao-studio: recents save: %v\n", err)
	}
}

// recordDeploy stamps the last-deploy record onto dir's recents entry (creating
// it if absent). Best-effort — a successful deploy must never fail because the
// convenience cache could not be written.
func (a *App) recordDeploy(dir string, rec recents.DeployRecord) {
	f, _ := recents.Load()
	f.SetDeploy(dir, rec)
	if err := recents.Save(f); err != nil {
		fmt.Fprintf(os.Stderr, "askdao-studio: recents save: %v\n", err)
	}
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
	p := a.currentProjectLocked()
	a.mu.Unlock()
	if p == nil || p.Data == nil {
		return nil, errors.New("no project scanned yet — choose a folder first")
	}
	dir, data := p.Dir, p.Data
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
	p := a.currentProjectLocked()
	a.mu.Unlock()
	if p == nil || p.Data == nil {
		return errors.New("no project scanned yet — choose a folder first")
	}
	dir, data := p.Dir, p.Data
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
func (a *App) modelCatalog(ctx context.Context) []types.ModelClassEntry {
	var url, token string
	if creds, err := auth.Load(); err == nil {
		url, token = creds.Server, creds.AccessToken
	}
	return recommender.FetchModelClassesOrFallback(ctx, url, token)
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
