// [INPUT]: 依赖 path/filepath；internal/types 的 AgentSpec / Detection / DetectedSkill / ReasoningDecision
// [OUTPUT]: 对外提供 StudioData / SkillCandidate / MCPCandidate / ObservedData / BuildStudioData
// [POS]: webstudio 的数据契约层 —— 把 pipeline 产物（spec 草稿 + detection 候选）摊平成前端 JSON；
//
//	默认勾选策略（project 全勾 / user opt-in / 仅兼容 MCP 默认勾）在此实现
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
	Observe         bool                      `json:"observe"` // --observe session: frontend shows the observe panel + polls /api/observe
	Icons           []IconDef                 `json:"icons"`   // avatar icon 网格的 lucide 子集（AvatarIcons）
}

// ObservedData is the GET /api/observe payload: the skills and MCP servers seen
// activated during an --observe session. The frontend polls this and overlays
// "actually used" evidence onto the candidate lists — additive, never auto-unticks.
type ObservedData struct {
	Skills     []string `json:"skills"`
	MCPServers []string `json:"mcp_servers"`
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
// Default selection: project-scope skills on, user-scope (global) skills off
// (opt-in), implied builtins on, Anthropic-compatible MCP on / incompatible off.
func BuildStudioData(spec *types.AgentSpec, det *types.Detection, harnessLabel string) *StudioData {
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

	for _, s := range det.DetectedSkills {
		if s.SkillName == "" { // implied-builtin placeholder
			for _, b := range s.ImpliedAnthropicSkills {
				d.SkillCandidates = append(d.SkillCandidates, SkillCandidate{
					Name:      b.SkillID,
					Builtin:   true,
					BuiltinID: b.SkillID,
					Checked:   true,
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
		d.SkillCandidates = append(d.SkillCandidates, SkillCandidate{
			Name:        s.SkillName,
			Scope:       scope,
			Harness:     s.Harness,
			Path:        filepath.ToSlash(filepath.Dir(s.Source)),
			Description: s.Description,
			Origin:      origin,
			Checked:     scope != "user", // project default-on, user opt-in
		})
	}

	for _, cfg := range det.DetectedMCPConfigs {
		scope := cfg.Scope
		if scope == "" {
			scope = "project"
		}
		for _, srv := range cfg.Servers {
			d.MCPCandidates = append(d.MCPCandidates, MCPCandidate{
				Name:       srv.Name,
				Scope:      scope,
				Harness:    cfg.Harness,
				Type:       srv.Type,
				URL:        srv.URL,
				Command:    srv.Command,
				Compatible: srv.AnthropicCompatible,
				Warning:    srv.Warning,
				Checked:    srv.AnthropicCompatible, // only deployable ones default-on
			})
		}
	}
	return d
}
