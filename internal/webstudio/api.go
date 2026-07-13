// [INPUT]: 依赖 path/filepath；internal/types 的 AgentSpec / Detection / DetectedSkill / ReasoningDecision
// [OUTPUT]: 对外提供 StudioData / SkillCandidate / MCPCandidate / ObservedData / AuthState / LoginChallenge / ChatRequest / AssistantRequest / SkillValidation / SkillFix / BuildStudioData
// [POS]: webstudio 的数据契约层 —— 把 pipeline 产物（spec 草稿 + detection 候选）摊平成前端 JSON；
//
//	restorePrior=true（编辑已有 yaml）时勾选态以 spec.skills/mcp_servers 为唯一真相；
//	否则（全新草稿）用默认策略（project 全勾 / user opt-in / 仅兼容 MCP 默认勾）
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package webstudio

import (
	"path/filepath"

	"github.com/askdao/askdao-cli/internal/types"
)

// StudioData is the JSON payload sent to the browser on GET /api/spec. Spec is
// the editable draft; the candidate lists are the full discovery surface the KOL
// ticks through (project + user scope), with default selection pre-applied.
type StudioData struct {
	Spec            *types.AgentSpec          `json:"spec"`
	SkillCandidates []SkillCandidate          `json:"skill_candidates"`
	MCPCandidates   []MCPCandidate            `json:"mcp_candidates"`
	Reasoning       []types.ReasoningDecision `json:"reasoning"`
	Palette         []ThemeToken              `json:"palette"`
	Categories      []string                  `json:"categories"`
	ProjectName     string                    `json:"project_name"`
	Harness         string                    `json:"harness"`
	Observe         bool                      `json:"observe"`               // --observe session: frontend shows the observe panel + polls /api/observe
	Desktop         bool                      `json:"desktop"`               // desktop-app session: frontend shows desktop-only blocks (assistant sidebar / test chat); CLI edit leaves it false
	NeedsScan       bool                      `json:"needs_scan"`            // desktop pre-scan placeholder: frontend shows the folder-picker prompt until POST /api/scan returns a scanned draft
	Icons           []IconDef                 `json:"icons"`                 // avatar icon 网格的 lucide 子集（AvatarIcons）
	ModelCatalog    []types.ModelClassEntry   `json:"model_catalog"`         // step-2 模型档（档名/concrete model/成本）从 conductor /cli/model-classes 拉，前端零硬编码 id
	Projects        []ProjectSummary          `json:"projects,omitempty"`    // desktop multi-project switcher list; CLI leaves nil → omitempty keeps it invisible
	Lightweight     bool                      `json:"lightweight,omitempty"` // project opened via lightweight yaml parse (det=nil): collect() must NOT rebuild skills/mcp from (empty) candidates or it silently wipes the yaml's declared skills
}

// ProjectSummary is one row of the desktop multi-project switcher
// (StudioData.Projects). It carries just enough to render the switcher entry +
// its deploy-status glance; the full editable draft for the *current* project
// rides in StudioData.Spec/candidates. Desktop-only (CLI leaves Projects nil).
type ProjectSummary struct {
	Dir            string `json:"dir"`
	Name           string `json:"name"`                       // metadata.name (if materialized) or dir basename
	Current        bool   `json:"current"`                    // the project currently loaded in the workbench
	Deployed       bool   `json:"deployed"`                   // has a recorded deploy
	DeployedName   string `json:"deployed_name,omitempty"`    // metadata.name at deploy time — reveals (owner,name) drift if the draft was renamed
	AgentID        string `json:"agent_id,omitempty"`         // conductor agent id from the last deploy
	Version        string `json:"version,omitempty"`          // e.g. "v2" (PreviousManagedVersion+1); only known on an update deploy
	LastDeployedAt string `json:"last_deployed_at,omitempty"` // RFC3339
	Missing        bool   `json:"missing,omitempty"`          // dir has vanished from disk since it was recorded
}

// ObservedData is the GET /api/observe payload: the skills and MCP servers seen
// activated during an --observe session. The frontend polls this and overlays
// "actually used" evidence onto the candidate lists — additive, never auto-unticks.
type ObservedData struct {
	Skills     []string `json:"skills"`
	MCPServers []string `json:"mcp_servers"`
}

// DeployResult is the structured outcome of a deploy, surfaced to the success
// card. GroupLink travels as its own field (not buried in Message) so the card
// can render a clickable button — the subscriber entry point the KOL wants.
type DeployResult struct {
	Message   string `json:"message"`
	GroupLink string `json:"group_link,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Created   bool   `json:"created"`
}

// ChatRequest is the POST /api/chat body from the desktop test-chat panel — the
// private-chat subset forwarded verbatim to conductor's /chat. SessionID is empty
// on the first turn and carried back from the previous turn's done frame
// (sdk_session_id / ov_session_id) for multi-turn continuity.
type ChatRequest struct {
	Message   string `json:"message"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id,omitempty"`
}

// AssistantRequest is the POST /api/assistant body from the desktop assistant
// sidebar. Unlike ChatRequest it carries no AgentID — the Go side resolves the
// official Studio assistant agent (a shared is_official agent), so the WebView
// never sees or chooses it. SessionID carries multi-turn continuity (same as
// ChatRequest, from the previous turn's done frame). Context is an optional hint
// the frontend attaches (a summary of the current spec draft, or the last deploy
// error) so the assistant can explain fields / diagnose the failure.
type AssistantRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty"`
	Context   string `json:"context,omitempty"`
}

// SkillValidation is one entry of the /api/skill-validate result — the desktop
// Skills step reads it to flag a custom_local skill whose SKILL.md frontmatter is
// missing name or description (exactly the fields PackageSkills rejects at deploy
// time, surfaced here so the KOL fixes them before deploy, not after). Path
// matches the SkillCandidate.Path the frontend keys on; DirName is the folder
// basename offered as a one-tap name fill.
type SkillValidation struct {
	Path           string `json:"path"`
	DirName        string `json:"dir_name"`
	HasName        bool   `json:"has_name"`
	HasDescription bool   `json:"has_description"`
}

// SkillFix is the POST /api/skill-fix body — write name and/or description into a
// custom_local skill's SKILL.md frontmatter (an empty field is left untouched, so
// the KOL can fix just the missing one). Path is the SkillCandidate.Path; the Go
// side resolves it to the on-disk SKILL.md.
type SkillFix struct {
	Path        string `json:"path"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// AuthState is the desktop login status — the GET /api/auth/status payload and
// the result of each login poll. Desktop-only; the CLI web studio never registers
// /api/auth/* (its auth callbacks are nil), so `agent edit` never sees it.
type AuthState struct {
	LoggedIn bool   `json:"logged_in"`
	Email    string `json:"email,omitempty"`
}

// LoginChallenge is the device-flow user code + verification URL the desktop login
// panel shows after POST /api/auth/login. The Go side opens the browser to the
// URL; the frontend then polls GET /api/auth/poll until logged in.
type LoginChallenge struct {
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
}

// SkillCandidate is one selectable skill — a concrete custom_local skill (with a
// directory Path) or an implied builtin (Builtin=true, BuiltinID set).
type SkillCandidate struct {
	Name        string `json:"name"`
	Scope       string `json:"scope"`   // project | user
	Harness     string `json:"harness"` // claude | codex | ""
	Path        string `json:"path"`    // dir path: project-relative, or absolute for user scope
	Description string `json:"description"`
	Origin      string `json:"origin"` // repo-native | vendored: <source>
	Checked     bool   `json:"checked"`
	Builtin     bool   `json:"builtin"`
	BuiltinID   string `json:"builtin_id,omitempty"`
}

// MCPCandidate is one selectable MCP server. URL/Command travel so the frontend
// can reassemble spec.mcp_servers from the ticked entries.
type MCPCandidate struct {
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	Harness    string `json:"harness"`
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	Command    string `json:"command,omitempty"`
	Compatible bool   `json:"compatible"`
	Warning    string `json:"warning,omitempty"`
	Checked    bool   `json:"checked"`
}

// BuildStudioData flattens the spec draft + detection into the studio payload.
//
// restorePrior selects how the candidate tick-state is decided:
//   - true (editing an existing askdao-agent.yml): the KOL's prior selection is
//     the single source of truth — tick exactly what spec.skills/mcp_servers
//     declares, nothing else. This is what makes a re-edit faithfully reflect the
//     saved yaml (e.g. a stdio MCP the default policy would otherwise drop).
//   - false (a fresh draft): no authoritative selection exists yet, so apply the
//     default policy — project-scope skills on, user-scope (global) skills off
//     (opt-in), implied builtins on, Anthropic-compatible MCP on / incompatible off.
func BuildStudioData(spec *types.AgentSpec, det *types.Detection, harnessLabel string, restorePrior bool) *StudioData {
	d := &StudioData{
		Spec:       spec,
		Palette:    Palette,
		Categories: Categories,
		Icons:      AvatarIcons,
		Harness:    harnessLabel,
	}
	if spec != nil {
		d.ProjectName = spec.Metadata.Name
		if spec.Provenance != nil {
			d.Reasoning = spec.Provenance.ReasoningDecisions
		}
	}
	if det == nil {
		return d
	}

	// Prior-selection lookup tables. Non-nil only in restore mode; the keys mirror
	// how collect() (studio.html) serializes a ticked candidate back into the spec:
	// builtin→id, custom_local→path, mcp→name.
	var priorSkills, priorMCP map[string]bool
	if restorePrior && spec != nil {
		priorSkills = make(map[string]bool, len(spec.Skills))
		for _, s := range spec.Skills {
			switch s.Type {
			case "builtin":
				priorSkills["builtin:"+s.ID] = true
			case "custom_local":
				priorSkills["local:"+filepath.ToSlash(s.Path)] = true
			}
		}
		priorMCP = make(map[string]bool, len(spec.MCPServers))
		for _, m := range spec.MCPServers {
			priorMCP[m.Name] = true
		}
	}

	for _, s := range det.DetectedSkills {
		if s.SkillName == "" { // implied-builtin placeholder
			for _, b := range s.ImpliedAnthropicSkills {
				checked := true // default: implied builtins on
				if priorSkills != nil {
					checked = priorSkills["builtin:"+b.SkillID]
				}
				d.SkillCandidates = append(d.SkillCandidates, SkillCandidate{
					Name:      b.SkillID,
					Builtin:   true,
					BuiltinID: b.SkillID,
					Checked:   checked,
				})
			}
			continue
		}
		scope := s.Scope
		if scope == "" {
			scope = "project"
		}
		origin := "repo-native"
		if !s.IsLocalOriginal {
			origin = "vendored"
			if s.LockedSource != "" {
				origin = "vendored: " + s.LockedSource
			}
		}
		path := filepath.ToSlash(filepath.Dir(s.Source))
		checked := scope != "user" // default: project on, user opt-in
		if priorSkills != nil {
			checked = priorSkills["local:"+path]
		}
		d.SkillCandidates = append(d.SkillCandidates, SkillCandidate{
			Name:        s.SkillName,
			Scope:       scope,
			Harness:     s.Harness,
			Path:        path,
			Description: s.Description,
			Origin:      origin,
			Checked:     checked,
		})
	}

	for _, cfg := range det.DetectedMCPConfigs {
		scope := cfg.Scope
		if scope == "" {
			scope = "project"
		}
		for _, srv := range cfg.Servers {
			checked := srv.AnthropicCompatible // default: only deployable ones on
			if priorMCP != nil {
				checked = priorMCP[srv.Name]
			}
			d.MCPCandidates = append(d.MCPCandidates, MCPCandidate{
				Name:       srv.Name,
				Scope:      scope,
				Harness:    cfg.Harness,
				Type:       srv.Type,
				URL:        srv.URL,
				Command:    srv.Command,
				Compatible: srv.AnthropicCompatible,
				Warning:    srv.Warning,
				Checked:    checked,
			})
		}
	}
	return d
}
