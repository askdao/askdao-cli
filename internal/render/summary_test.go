package render

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

// fixtureSpec builds the AgentSpec that mirrors the example in design.md §3.1
// (my-agent / FastAPI / SQLAlchemy / FastAPI deps / GITHUB_TOKEN vault, etc.).
// Tests render this and compare to a golden file for the high-signal layout
// guarantee.
func fixtureSpec() SummaryInput {
	spec := &types.AgentSpec{
		APIVersion: types.AgentSpecAPIVersion,
		Kind:       types.AgentSpecKind,
		Metadata: types.Metadata{
			Name:        "my-agent",
			Description: "Backend engineering assistant",
			Version:     "0.1.0",
			Visibility:  "private",
		},
		Persona: types.Persona{
			ModelClass: "high_reasoning",
			ModelPreferences: []types.ModelPreference{
				{Provider: "anthropic", ID: "claude-opus-4-6", Speed: "standard"},
				{Provider: "openai", ID: "gpt-5.4"},
				{Provider: "anthropic", ID: "claude-sonnet-4-6"},
			},
			SystemPrompt: strings.Repeat("x", 380),
		},
		Capabilities: types.Capabilities{
			Shell:         types.Capability{Enabled: true, Permission: "ask_for_dangerous"},
			Filesystem:    types.Capability{Enabled: true, Permission: "allow", Scopes: []string{"./output", "./tmp"}},
			Web:           types.Capability{Enabled: true, Permission: "allow"},
			CodeExecution: types.Capability{Enabled: true, Permission: "allow"},
		},
		MCPServers: []types.MCPServer{
			{Name: "github", Type: "url", URL: "https://api.githubcopilot.com/mcp/"},
		},
		Skills: []types.Skill{
			{Type: "builtin", Provider: "anthropic", ID: "xlsx"},
			{Type: "custom_local", Path: "./skills/portfolio-analyzer"},
		},
		Workspace: types.Workspace{
			Workdir: "/app",
			Networking: types.Networking{
				Mode:                 "limited",
				AllowedHosts:         []string{"api.anthropic.com", "api.openai.com", "api.githubcopilot.com"},
				AllowMCPServers:      true,
				AllowPackageManagers: false,
			},
			Packages: types.WorkspacePackages{
				Pip: []string{
					"fastapi==0.135.1", "alembic==1.18.4",
					"sqlalchemy==2.0.48", "anthropic==0.97.0",
					"asyncpg==0.31.0", "pydantic==2.12.5",
					"uvicorn==0.36.0", "httpx==0.28.0",
					"redis==5.0.1", "celery==5.4.0",
					"orjson==3.9.10", "structlog==24.1.0",
				},
				Apt: []string{"libpq-dev", "gcc", "libjpeg-dev"},
			},
		},
		VaultHints: types.VaultHints{
			RequiredCredentials: []types.VaultCredential{
				{
					Name:     "GITHUB_TOKEN",
					Purpose:  "MCP server github authentication",
					From:     ".env.example",
					Required: true,
					UsedBy:   map[string]interface{}{"mcp_server": "github"},
				},
			},
		},
		PreferredHarness: "anthropic_managed_agents",
	}

	return SummaryInput{
		Spec:             spec,
		ReasoningSummary: "FastAPI + SQLAlchemy detected; high reasoning suits migration logic.",
		ReasoningDecisions: []types.ReasoningDecision{
			{Decision: "selected_primary_model=claude-opus-4-6",
				Reason: "FastAPI + Alembic migration logic complex", Confidence: 0.78},
			{Decision: "shell.permission=ask_for_dangerous",
				Reason: "Production deploy detected", Confidence: 0.92},
		},
		TotalProdPipDeps: 28,
		TotalDevPipDeps:  14,
		FilteredMCPServers: []types.MCPServerConfig{
			{Name: "filesystem", Type: "stdio"},
		},
		Warnings: []TranslationWarning{
			{Field: "workspace.base_image", Action: "IGNORED",
				Reason:   "Anthropic uses fixed cloud image.",
				Severity: SeverityHigh},
			{Field: "workspace.setup_commands", Action: "PARTIALLY IGNORED",
				Reason:            "apt/pip names extracted; raw commands lost.",
				Severity:          SeverityHigh,
				FallbackAttempted: true},
			{Field: "workspace.users", Action: "IGNORED",
				Severity: SeverityMedium},
		},
		Harness: "Anthropic Managed Agents",
	}
}

// TestRenderSummary_StableLayout is the acceptance test: the 7-section card
// rendered against the design.md §3.1 fixture matches a stable golden file.
//
// Update the golden by running:  go test ./internal/render -run StableLayout -update
func TestRenderSummary_StableLayout(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderSummary(r, fixtureSpec())

	got := buf.String()
	goldenPath := filepath.Join("testdata", "golden_summary.txt")

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("golden updated")
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v\n(set UPDATE_GOLDEN=1 to seed)", err)
	}
	if got != string(want) {
		t.Errorf("RenderSummary output drifted from golden.\n--- got ---\n%s\n--- want ---\n%s",
			got, string(want))
	}
}

// Smoke checks for individual sections so a regression points at the failing
// block instead of dumping the whole golden diff.
func TestRenderSummary_SectionSmoke(t *testing.T) {
	var buf bytes.Buffer
	RenderSummary(NewPlain(&buf), fixtureSpec())
	out := buf.String()

	for _, want := range []string{
		"PERSONA",
		"my-agent",
		"SKILLS  (2)",
		"xlsx",
		"portfolio-analyzer",
		"MCP SERVERS  (1 active, 1 filtered)",
		"github",
		"CAPABILITIES",
		"ask_for_dangerous",
		"RUNTIME",
		"fastapi==0.135.1",
		"... and 4 more",
		"libpq-dev",
		"SUBSCRIBER ONBOARDING",
		"GITHUB_TOKEN",
		"TRANSLATION WARNINGS",
		"workspace.base_image = IGNORED",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\n--- output ---\n%s", want, out)
		}
	}
}
