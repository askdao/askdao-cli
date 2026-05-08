package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestDiffAgentSpec_TwoFieldChanges(t *testing.T) {
	// Acceptance criterion: detect two field modifications and produce the
	// design.md §3.5 diff format.
	a := &types.AgentSpec{
		Persona: types.Persona{
			ModelClass: "high_reasoning",
			ModelPreferences: []types.ModelPreference{
				{Provider: "anthropic", ID: "claude-opus-4-6", Speed: "standard"},
			},
		},
		Capabilities: types.Capabilities{
			Shell: types.Capability{Enabled: true, Permission: "ask_for_dangerous"},
		},
	}
	b := *a
	b.Persona = types.Persona{
		ModelClass: "high_reasoning",
		ModelPreferences: []types.ModelPreference{
			{Provider: "anthropic", ID: "claude-sonnet-4-6", Speed: "standard"},
		},
	}
	b.Capabilities.Shell.Permission = "always_allow"

	diffs := DiffAgentSpec(a, &b)
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d: %+v", len(diffs), diffs)
	}

	gotPaths := map[string]string{}
	for _, d := range diffs {
		gotPaths[d.Path] = d.Before + " -> " + d.After
	}
	if gotPaths["persona.model_preferences[0].id"] != "claude-opus-4-6 -> claude-sonnet-4-6" {
		t.Errorf("model_preferences diff = %q", gotPaths["persona.model_preferences[0].id"])
	}
	if gotPaths["capabilities.shell.permission"] != "ask_for_dangerous -> always_allow" {
		t.Errorf("shell.permission diff = %q", gotPaths["capabilities.shell.permission"])
	}
}

func TestRenderDiff_FormatMatchesDesign(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderDiff(r, []FieldDiff{
		{Path: "persona.model_preferences[0].id", Before: "claude-opus-4-6", After: "claude-sonnet-4-6"},
		{Path: "capabilities.shell.permission", Before: "ask_for_dangerous", After: "always_allow"},
	})
	out := buf.String()
	wantSnippets := []string{
		"persona.model_preferences[0].id:",
		"-  claude-opus-4-6",
		"+  claude-sonnet-4-6",
		"capabilities.shell.permission:",
		"-  ask_for_dangerous",
		"+  always_allow",
	}
	for _, s := range wantSnippets {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in diff output:\n%s", s, out)
		}
	}
}

func TestRenderDiff_NoChanges(t *testing.T) {
	var buf bytes.Buffer
	RenderDiff(NewPlain(&buf), nil)
	if !strings.Contains(buf.String(), "no changes since last recommendation") {
		t.Errorf("no-changes hint missing: %s", buf.String())
	}
}

func TestDiffAgentSpec_PackageList(t *testing.T) {
	a := &types.AgentSpec{Workspace: types.Workspace{
		Packages: types.WorkspacePackages{Pip: []string{"fastapi==0.135.1"}},
	}}
	b := &types.AgentSpec{Workspace: types.Workspace{
		Packages: types.WorkspacePackages{Pip: []string{"fastapi==0.135.1", "redis==5.0.1"}},
	}}
	diffs := DiffAgentSpec(a, b)
	if len(diffs) != 1 || diffs[0].Path != "workspace.packages.pip" {
		t.Fatalf("expected single pip diff, got %+v", diffs)
	}
	if !strings.Contains(diffs[0].After, "redis==5.0.1") {
		t.Errorf("After missing new pkg: %q", diffs[0].After)
	}
}

func TestDiffAgentSpec_MCPAddRemove(t *testing.T) {
	a := &types.AgentSpec{MCPServers: []types.MCPServer{{Name: "github", Type: "url", URL: "u1"}}}
	b := &types.AgentSpec{MCPServers: []types.MCPServer{{Name: "slack", Type: "url", URL: "u2"}}}
	diffs := DiffAgentSpec(a, b)
	gotPaths := map[string]bool{}
	for _, d := range diffs {
		gotPaths[d.Path] = true
	}
	if !gotPaths["mcp_servers[github]"] || !gotPaths["mcp_servers[slack]"] {
		t.Errorf("expected add+remove diffs, got %+v", diffs)
	}
}
