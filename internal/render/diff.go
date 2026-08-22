package render

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// FieldDiff is one yaml-path level change. design.md §3.5 deploy diff preview
// shape:
//
//	persona.model_preferences[0].id:
//	  -  claude-opus-4-6
//	  +  claude-sonnet-4-6
type FieldDiff struct {
	Path   string // yaml-style dotted path, with [N] for slice indices
	Before string
	After  string
}

// DiffAgentSpec returns the field-level differences between a and b. Only a
// pragmatic subset of AgentSpec fields is walked — those that KOLs realistically
// edit between recommendation and deploy (persona, capabilities, mcp_servers,
// workspace.packages, preferred_harness, vault_hints). Adding more is a
// reflection-based extension; we keep the explicit walk so output ordering is
// stable and the field paths are KOL-readable.
func DiffAgentSpec(a, b *types.AgentSpec) []FieldDiff {
	if a == nil || b == nil {
		return nil
	}
	var out []FieldDiff

	out = appendScalarDiff(out, "metadata.name", a.Metadata.Name, b.Metadata.Name)
	out = appendScalarDiff(out, "metadata.description", a.Metadata.Description, b.Metadata.Description)
	out = appendScalarDiff(out, "metadata.visibility", a.Metadata.Visibility, b.Metadata.Visibility)

	out = appendScalarDiff(out, "persona.model_class", a.Persona.ModelClass, b.Persona.ModelClass)
	out = appendScalarDiff(out, "persona.system_prompt", a.Persona.SystemPrompt, b.Persona.SystemPrompt)

	mpA, mpB := a.Persona.ModelPreferences, b.Persona.ModelPreferences
	for i := 0; i < max(len(mpA), len(mpB)); i++ {
		var av, bv types.ModelPreference
		if i < len(mpA) {
			av = mpA[i]
		}
		if i < len(mpB) {
			bv = mpB[i]
		}
		out = appendScalarDiff(out, fmt.Sprintf("persona.model_preferences[%d].id", i), av.ID, bv.ID)
		out = appendScalarDiff(out, fmt.Sprintf("persona.model_preferences[%d].provider", i), av.Provider, bv.Provider)
		out = appendScalarDiff(out, fmt.Sprintf("persona.model_preferences[%d].speed", i), av.Speed, bv.Speed)
	}

	out = appendCapabilityDiff(out, "shell", a.Capabilities.Shell, b.Capabilities.Shell)
	out = appendCapabilityDiff(out, "filesystem", a.Capabilities.Filesystem, b.Capabilities.Filesystem)
	out = appendCapabilityDiff(out, "web", a.Capabilities.Web, b.Capabilities.Web)
	out = appendCapabilityDiff(out, "code_execution", a.Capabilities.CodeExecution, b.Capabilities.CodeExecution)

	out = appendListDiff(out, "workspace.packages.pip", a.Workspace.Packages.Pip, b.Workspace.Packages.Pip)
	out = appendListDiff(out, "workspace.packages.apt", a.Workspace.Packages.Apt, b.Workspace.Packages.Apt)
	out = appendListDiff(out, "workspace.packages.npm", a.Workspace.Packages.Npm, b.Workspace.Packages.Npm)
	out = appendScalarDiff(out, "workspace.networking.mode", a.Workspace.Networking.Mode, b.Workspace.Networking.Mode)

	out = appendMCPDiff(out, a.MCPServers, b.MCPServers)

	out = appendScalarDiff(out, "preferred_harness", a.PreferredHarness, b.PreferredHarness)

	// #303 automation blocks (nil-safe: zero-value view when the block is absent)
	oa, ob := outcomesView(a.Outcomes), outcomesView(b.Outcomes)
	out = appendScalarDiff(out, "outcomes.enabled", oa.enabled, ob.enabled)
	out = appendScalarDiff(out, "outcomes.rubric", oa.rubric, ob.rubric)
	out = appendScalarDiff(out, "outcomes.max_iterations", oa.maxIter, ob.maxIter)
	sa, sb := scheduleView(a.Schedule), scheduleView(b.Schedule)
	out = appendScalarDiff(out, "schedule.enabled", sa.enabled, sb.enabled)
	out = appendScalarDiff(out, "schedule.mode", sa.mode, sb.mode)
	out = appendScalarDiff(out, "schedule.cron", sa.cron, sb.cron)
	out = appendScalarDiff(out, "schedule.timezone", sa.timezone, sb.timezone)
	out = appendScalarDiff(out, "schedule.task", sa.task, sb.task)
	out = appendScalarDiff(out, "schedule.notify_channel", sa.notify, sb.notify)
	// First-deploy toggle seeds (conductor applies them on create only).
	ma, mb := memoryView(a.Memory), memoryView(b.Memory)
	out = appendScalarDiff(out, "memory.inject_user_profile", ma.injectUser, mb.injectUser)
	out = appendScalarDiff(out, "memory.inject_agent_profile", ma.injectAgent, mb.injectAgent)
	wa, wb := wikiView(a.Wiki), wikiView(b.Wiki)
	out = appendScalarDiff(out, "wiki.enabled", wa.enabled, wb.enabled)
	out = appendScalarDiff(out, "wiki.evolution", wa.evolution, wb.evolution)
	return out
}

type memoryFields struct{ injectUser, injectAgent string }

func memoryView(m *types.Memory) memoryFields {
	if m == nil {
		return memoryFields{}
	}
	return memoryFields{
		injectUser:  optBoolStr(m.InjectUserProfile),
		injectAgent: optBoolStr(m.InjectAgentProfile),
	}
}

type wikiFields struct{ enabled, evolution string }

func wikiView(w *types.Wiki) wikiFields {
	if w == nil {
		return wikiFields{}
	}
	return wikiFields{enabled: optBoolStr(w.Enabled), evolution: optBoolStr(w.Evolution)}
}

// optBoolStr renders a tri-state switch: "" when undeclared (so an absent
// block diffs clean against an absent one), else "true"/"false".
func optBoolStr(v *bool) string {
	if v == nil {
		return ""
	}
	return boolStr(*v)
}

type outcomesFields struct{ enabled, rubric, maxIter string }

func outcomesView(o *types.Outcomes) outcomesFields {
	if o == nil {
		return outcomesFields{}
	}
	v := outcomesFields{enabled: "true", rubric: o.Rubric}
	if o.Enabled != nil && !*o.Enabled {
		v.enabled = "false"
	}
	if o.MaxIterations > 0 {
		v.maxIter = fmt.Sprintf("%d", o.MaxIterations)
	}
	return v
}

type scheduleFields struct{ enabled, mode, cron, timezone, task, notify string }

func scheduleView(s *types.Schedule) scheduleFields {
	if s == nil {
		return scheduleFields{}
	}
	v := scheduleFields{
		enabled: "true", mode: s.Mode, cron: s.Cron, timezone: s.Timezone,
		task: s.Task, notify: s.NotifyChannel,
	}
	if v.mode == "" {
		v.mode = "personal"
	}
	if s.Enabled != nil && !*s.Enabled {
		v.enabled = "false"
	}
	return v
}

func appendCapabilityDiff(out []FieldDiff, name string, a, b types.Capability) []FieldDiff {
	out = appendScalarDiff(out, "capabilities."+name+".enabled", boolStr(a.Enabled), boolStr(b.Enabled))
	out = appendScalarDiff(out, "capabilities."+name+".permission", a.Permission, b.Permission)
	return out
}

func appendMCPDiff(out []FieldDiff, a, b []types.MCPServer) []FieldDiff {
	keyOf := func(s types.MCPServer) string { return s.Name }
	aMap := map[string]types.MCPServer{}
	bMap := map[string]types.MCPServer{}
	for _, s := range a {
		aMap[keyOf(s)] = s
	}
	for _, s := range b {
		bMap[keyOf(s)] = s
	}
	allKeys := map[string]bool{}
	for k := range aMap {
		allKeys[k] = true
	}
	for k := range bMap {
		allKeys[k] = true
	}
	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		av, aok := aMap[name]
		bv, bok := bMap[name]
		switch {
		case aok && !bok:
			out = append(out, FieldDiff{Path: "mcp_servers[" + name + "]", Before: serializeMCP(av), After: "(removed)"})
		case !aok && bok:
			out = append(out, FieldDiff{Path: "mcp_servers[" + name + "]", Before: "(added)", After: serializeMCP(bv)})
		default:
			out = appendScalarDiff(out, "mcp_servers["+name+"].url", av.URL, bv.URL)
			out = appendScalarDiff(out, "mcp_servers["+name+"].type", av.Type, bv.Type)
		}
	}
	return out
}

func serializeMCP(s types.MCPServer) string {
	if s.URL != "" {
		return fmt.Sprintf("%s (%s)", s.URL, s.Type)
	}
	return fmt.Sprintf("%s (%s)", s.Command, s.Type)
}

func appendScalarDiff(out []FieldDiff, path, a, b string) []FieldDiff {
	if a == b {
		return out
	}
	return append(out, FieldDiff{Path: path, Before: a, After: b})
}

func appendListDiff(out []FieldDiff, path string, a, b []string) []FieldDiff {
	if reflect.DeepEqual(a, b) {
		return out
	}
	return append(out, FieldDiff{
		Path:   path,
		Before: "[" + strings.Join(a, ", ") + "]",
		After:  "[" + strings.Join(b, ", ") + "]",
	})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// RenderDiff prints diffs in the design.md §3.5 deploy preview shape.
func RenderDiff(r *Renderer, diffs []FieldDiff) {
	if len(diffs) == 0 {
		r.println("  " + r.dim("(no changes since last recommendation)"))
		return
	}
	for _, d := range diffs {
		r.printf("  %s\n", r.bold(d.Path)+":")
		r.printf("    %s  %s\n", r.red("-"), valueOrEmpty(d.Before))
		r.printf("    %s  %s\n", r.green("+"), valueOrEmpty(d.After))
		r.println("")
	}
}

func valueOrEmpty(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
