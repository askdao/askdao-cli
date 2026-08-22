package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// SummaryInput bundles everything Render needs in one place. Anything beyond
// the AgentSpec itself (reasoning trace, package counts pre-filter,
// translation warnings, MCP filter info) lives here so the cmd-layer can fold
// detection.json + provider plans + adapter feedback into a single render
// call without polluting AgentSpec.
type SummaryInput struct {
	Spec               *types.AgentSpec
	ReasoningSummary   string
	ReasoningDecisions []types.ReasoningDecision

	// Counters around Workspace.Packages — needed to print the "(28 prod, 14
	// dev filtered)" hint that doesn't survive in AgentSpec.
	TotalProdPipDeps int
	TotalDevPipDeps  int

	// FilteredMCPServers carries servers that were dropped during translation
	// (e.g. stdio under Anthropic). Empty when nothing was filtered.
	FilteredMCPServers []types.MCPServerConfig

	// Warnings is the conductor adapter's translation_report.
	Warnings []TranslationWarning
	Harness  string // label used in the warnings section title

	// SystemPromptLen drives the inline "[P] view" hint when system_prompt is
	// long. (Persona is fully embedded in spec.Persona.SystemPrompt as of v0.7;
	// no external persona.md file anymore.)
}

// RenderSummary prints the full 7-block mid-density review card from
// design.md §3.1. Sections are emitted in the exact spec order: PERSONA,
// SKILLS, MCP SERVERS, CAPABILITIES, RUNTIME, SUBSCRIBER ONBOARDING,
// TRANSLATION WARNINGS.
func RenderSummary(r *Renderer, in SummaryInput) {
	if in.Spec == nil {
		return
	}
	renderPersona(r, in)
	r.println("")
	renderSkills(r, in.Spec.Skills)
	r.println("")
	renderMCPServers(r, in.Spec.MCPServers, in.FilteredMCPServers)
	r.println("")
	renderCapabilities(r, in.Spec.Capabilities)
	r.println("")
	renderRuntime(r, in)
	r.println("")
	renderAutomation(r, in.Spec)
	renderKnowledge(r, in.Spec)
	renderSubscriberOnboarding(r, in.Spec.VaultHints)
	r.println("")
	if len(in.Warnings) > 0 {
		RenderTranslationWarnings(r, in.Harness, in.Warnings, ViewSummary)
	}
}

func renderPersona(r *Renderer, in SummaryInput) {
	r.section("PERSONA")
	r.println("")

	pairs := [][2]string{}
	pairs = append(pairs, [2]string{"Name", in.Spec.Metadata.Name})
	if in.Spec.Metadata.Description != "" {
		pairs = append(pairs, [2]string{"Description", in.Spec.Metadata.Description})
	}
	pairs = append(pairs, [2]string{"Model class", in.Spec.Persona.ModelClass})

	primaryLabel := primaryModelLabel(in.Spec.Persona.ModelPreferences)
	pairs = append(pairs, [2]string{"Primary model", primaryLabel})

	RenderKVList(r, "  ", 14, pairs)

	// Inline reasoning right under "Primary model" if a relevant decision exists.
	if dec := findDecision(in.ReasoningDecisions, "selected_model", "primary_model", "model"); dec != nil {
		RenderReason(r, dec.Reason, ReasonOpts{Indent: "                   ", Confidence: dec.Confidence})
	}

	if fb := fallbackModels(in.Spec.Persona.ModelPreferences); fb != "" {
		RenderKVList(r, "  ", 14, [][2]string{{"Fallback", fb}})
	}

	// v0.7: persona is embedded in spec.Persona.SystemPrompt as a YAML literal
	// block; there is no external persona.md file anymore (see design.md §9.15).
	if in.Spec.Persona.SystemPrompt != "" {
		val := fmt.Sprintf("%d chars  %s", len(in.Spec.Persona.SystemPrompt), r.dim("[P] view"))
		RenderKVList(r, "  ", 14, [][2]string{{"System prompt", val}})
	}
}

func renderSkills(r *Renderer, skills []types.Skill) {
	r.section(fmt.Sprintf("SKILLS  (%d)", len(skills)))
	r.println("")
	if len(skills) == 0 {
		r.println("  " + r.dim("(no skills selected)"))
		return
	}
	for _, s := range skills {
		switch s.Type {
		case "builtin":
			provider := s.Provider
			if provider == "" {
				provider = "anthropic"
			}
			r.printf("  %s %s  (%s builtin)\n", r.green("✓"), r.bold(s.ID), provider)
		case "custom_local":
			r.printf("  %s %s  (custom local)\n", r.green("✓"), r.bold(skillNameFromPath(s.Path)))
			r.printf("    Path: %s\n", s.Path)
			r.println("    " + r.dim("Will be uploaded on deploy."))
		case "git_repo":
			r.printf("  %s %s  (git_repo)\n", r.green("✓"), r.bold(s.Repo))
			if s.Ref != "" {
				r.printf("    Ref: %s\n", s.Ref)
			}
		}
	}
}

func renderMCPServers(r *Renderer, active []types.MCPServer, filtered []types.MCPServerConfig) {
	header := fmt.Sprintf("MCP SERVERS  (%d active", len(active))
	if len(filtered) > 0 {
		header += fmt.Sprintf(", %d filtered", len(filtered))
	}
	header += ")"
	r.section(header)
	r.println("")

	if len(active) == 0 && len(filtered) == 0 {
		r.println("  " + r.dim("(no MCP servers configured)"))
		return
	}

	for _, s := range active {
		r.printf("  %s %s\n", r.green("✓"), r.bold(s.Name))
		if s.URL != "" {
			r.printf("    URL  : %s\n", s.URL)
		}
		r.printf("    Type : %s  %s\n", s.Type, r.dim("(Anthropic compatible ✓)"))
	}
	for _, s := range filtered {
		r.println("")
		r.printf("  %s %s  (filtered out)\n", r.yellow("⊘"), s.Name)
		r.printf("    Type : %s  %s\n", s.Type, r.dim("(not supported by Anthropic)"))
		r.println("    " + r.dim("[M] see filtered + alternatives"))
	}
}

func renderCapabilities(r *Renderer, caps types.Capabilities) {
	r.section("CAPABILITIES (Tool permissions)")
	r.println("")
	pairs := []capabilityRow{
		{"shell", caps.Shell},
		{"filesystem", caps.Filesystem},
		{"web", caps.Web},
		{"code_execution", caps.CodeExecution},
	}
	colWidth := 14
	for _, p := range pairs {
		marker, perm := capabilityMarker(r, p.cap)
		extra := ""
		if len(p.cap.Scopes) > 0 {
			extra = fmt.Sprintf("  (scopes: %s)", JoinTruncated(p.cap.Scopes, 5))
		}
		r.printf("  %-*s %s %s%s\n", colWidth, p.name, marker, perm, extra)
	}
}

type capabilityRow struct {
	name string
	cap  types.Capability
}

func capabilityMarker(r *Renderer, c types.Capability) (string, string) {
	if !c.Enabled {
		return r.dim("⊘"), r.dim("disabled")
	}
	switch c.Permission {
	case "always_ask", "ask_for_dangerous":
		return r.yellow("⚠️"), c.Permission
	case "always_allow", "allow":
		return r.green("✓"), c.Permission
	default:
		return r.green("✓"), c.Permission
	}
}

func renderRuntime(r *Renderer, in SummaryInput) {
	r.section("RUNTIME  (Anthropic Managed Agents environment)")
	r.println("")

	pip := in.Spec.Workspace.Packages.Pip
	if len(pip) > 0 {
		hint := fmt.Sprintf("Python production deps (%d total", len(pip))
		if in.TotalProdPipDeps > 0 {
			hint = fmt.Sprintf("Python production deps (%d total", in.TotalProdPipDeps)
		}
		if in.TotalDevPipDeps > 0 {
			hint += fmt.Sprintf(", %d dev filtered", in.TotalDevPipDeps)
		}
		hint += "):"
		r.printf("  %s\n", hint)
		RenderItemList(r, pip, TruncateOpts{
			MaxShown:  8,
			Indent:    "    ",
			MoreEntry: "[D] view all   [F] view filtered",
		})
	}

	if npm := in.Spec.Workspace.Packages.Npm; len(npm) > 0 {
		r.println("")
		r.printf("  %s\n", fmt.Sprintf("Node deps (%d):", len(npm)))
		RenderItemList(r, npm, TruncateOpts{MaxShown: 8, Indent: "    ", MoreEntry: "[D] view all"})
	}

	if apt := in.Spec.Workspace.Packages.Apt; len(apt) > 0 {
		r.println("")
		r.printf("  System libs (apt, all %d):\n", len(apt))
		for _, a := range apt {
			r.printf("    • %s\n", a)
		}
	}

	netMode := in.Spec.Workspace.Networking.Mode
	if netMode == "" {
		netMode = "limited"
	}
	r.println("")
	r.printf("  Networking: %s\n", netMode)
	if hosts := in.Spec.Workspace.Networking.AllowedHosts; len(hosts) > 0 {
		r.printf("    Allowed hosts: %s  (%d total)\n", JoinTruncated(hosts, 5), len(hosts))
	}
	if wd := in.Spec.Workspace.Workdir; wd != "" {
		r.printf("  Workdir: %s\n", wd)
	}
}

// renderAutomation prints the #303 outcomes / schedule blocks. Both are
// optional — the whole section is skipped when neither block is present, so
// pre-#303 specs render byte-identical (golden safety).
func renderAutomation(r *Renderer, spec *types.AgentSpec) {
	if spec.Outcomes == nil && spec.Schedule == nil {
		return
	}
	r.section("AUTOMATION  (Outcomes & Schedule)")
	r.println("")
	if o := spec.Outcomes; o != nil {
		state := "enabled"
		if o.Enabled != nil && !*o.Enabled {
			state = "disabled"
		}
		iters := o.MaxIterations
		if iters <= 0 {
			iters = 3
		}
		r.printf("  Outcomes: %s — rubric %d chars, max %d grader iterations\n",
			state, len(o.Rubric), iters)
	}
	if s := spec.Schedule; s != nil {
		state := "enabled"
		if s.Enabled != nil && !*s.Enabled {
			state = "disabled"
		}
		tz := s.Timezone
		if tz == "" {
			tz = "UTC"
		}
		mode := s.Mode
		if mode == "" {
			mode = "personal"
		}
		r.printf("  Schedule: %s — %s, cron %q (%s)\n", state, mode, s.Cron, tz)
		if s.Task != "" {
			task := s.Task
			if runes := []rune(task); len(runes) > 72 {
				task = string(runes[:72]) + "…"
			}
			r.printf("    Task: %s\n", task)
		}
		if s.NotifyChannel != "" {
			r.printf("    Notify: %s\n", s.NotifyChannel)
		}
	}
	r.println("")
}

// renderKnowledge prints the memory-injection / wiki switches. Skipped whole
// when neither block declares one, so older specs render byte-identical.
// Declared switches apply on every deploy; the printed note says so, because
// the reverse (dashboard wins) is what a KOL would otherwise assume after
// flipping one there.
func renderKnowledge(r *Renderer, spec *types.AgentSpec) {
	var on []string
	if m := spec.Memory; m != nil {
		if m.InjectUserProfile != nil && *m.InjectUserProfile {
			on = append(on, "user profile")
		}
		if m.InjectAgentProfile != nil && *m.InjectAgentProfile {
			on = append(on, "agent profile")
		}
	}
	if w := spec.Wiki; w != nil && w.Enabled != nil && *w.Enabled {
		if w.Evolution != nil && *w.Evolution {
			on = append(on, "wiki + evolution")
		} else {
			on = append(on, "wiki")
		}
	}
	if len(on) == 0 {
		return
	}
	r.section("MEMORY & KNOWLEDGE")
	r.println("")
	r.printf("  Enabled: %s\n", strings.Join(on, ", "))
	r.println("  Applied on every deploy; the web dashboard can flip them in between.")
	r.println("")
}

func renderSubscriberOnboarding(r *Renderer, hints types.VaultHints) {
	r.section("SUBSCRIBER ONBOARDING  (Vault hints)")
	r.println("")
	if len(hints.RequiredCredentials) == 0 && len(hints.OptionalCredentials) == 0 {
		r.println("  " + r.dim("(no credentials required)"))
		return
	}
	if n := len(hints.RequiredCredentials); n > 0 {
		r.printf("  %s (%d):\n", r.bold("Required"), n)
		for _, c := range hints.RequiredCredentials {
			renderCredential(r, c)
		}
	}
	if n := len(hints.OptionalCredentials); n > 0 {
		r.println("")
		r.printf("  %s (%d):\n", r.bold("Optional"), n)
		for _, c := range hints.OptionalCredentials {
			renderCredential(r, c)
		}
	}
}

func renderCredential(r *Renderer, c types.VaultCredential) {
	r.printf("    🔑 %s\n", r.bold(c.Name))
	if used := formatUsedBy(c.UsedBy); used != "" {
		r.printf("       Used by: %s\n", used)
	}
	if c.From != "" {
		r.printf("       From   : %s\n", c.From)
	}
	if c.Note != "" {
		r.printf("       %s\n", r.dim(c.Note))
	}
	if !c.Required {
		r.printf("       %s\n", r.dim("Optional — KOL or subscriber may skip."))
	}
}

func formatUsedBy(u map[string]interface{}) string {
	if len(u) == 0 {
		return ""
	}
	keys := make([]string, 0, len(u))
	for k := range u {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, u[k]))
	}
	return strings.Join(parts, ", ")
}

func primaryModelLabel(prefs []types.ModelPreference) string {
	if len(prefs) == 0 {
		return "(unset)"
	}
	p := prefs[0]
	speed := p.Speed
	if speed == "" {
		speed = "standard"
	}
	return fmt.Sprintf("%s (%s)  [%s]", p.ID, speed, p.Provider)
}

func fallbackModels(prefs []types.ModelPreference) string {
	if len(prefs) <= 1 {
		return ""
	}
	parts := make([]string, 0, len(prefs)-1)
	for _, p := range prefs[1:] {
		parts = append(parts, p.ID)
	}
	return strings.Join(parts, ", ")
}

// findDecision returns the first reasoning decision whose Decision string
// contains any of the given keywords (case-insensitive substring).
func findDecision(decisions []types.ReasoningDecision, keywords ...string) *types.ReasoningDecision {
	for i, d := range decisions {
		low := strings.ToLower(d.Decision)
		for _, kw := range keywords {
			if strings.Contains(low, strings.ToLower(kw)) {
				return &decisions[i]
			}
		}
	}
	return nil
}

func skillNameFromPath(p string) string {
	if p == "" {
		return "(unnamed)"
	}
	// Strip trailing slash + extract last directory component.
	clean := strings.TrimRight(p, "/")
	if i := strings.LastIndex(clean, "/"); i >= 0 {
		return clean[i+1:]
	}
	return clean
}
