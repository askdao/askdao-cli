package scanner

import (
	"path/filepath"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestDetectRequiredSecrets_Classification(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".env.example"), `
# core
ANTHROPIC_API_KEY=
OPENAI_API_KEY=
GITHUB_TOKEN=

# data
DATABASE_URL=
REDIS_URL=

# cloud
AWS_ACCESS_KEY_ID=
AWS_SECRET_ACCESS_KEY=

# arbitrary third-party
STRIPE_API_KEY=

# unknown — no rule matches
CUSTOM_FLAG=true
`)

	mcps := []types.DetectedMCPConfig{
		{Source: ".mcp.json", Servers: []types.MCPServerConfig{{Name: "github", Type: "url"}}},
	}

	got, err := DetectRequiredSecrets(root, mcps)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]types.DetectedRequiredSecret{}
	for _, s := range got {
		byName[s.Name] = s
	}

	if !byName["ANTHROPIC_API_KEY"].Required {
		t.Error("ANTHROPIC_API_KEY should be required")
	}
	if byName["DATABASE_URL"].Required {
		t.Error("DATABASE_URL should be flagged not required (project-internal)")
	}
	if byName["DATABASE_URL"].Note == "" {
		t.Error("DATABASE_URL should carry a note")
	}
	gh := byName["GITHUB_TOKEN"]
	if gh.UsedByGuess == nil || gh.UsedByGuess.MCPServer != "github" {
		t.Errorf("GITHUB_TOKEN should be cross-linked to MCP github server: %+v", gh)
	}
	if byName["STRIPE_API_KEY"].PurposeGuess == "" || !byName["STRIPE_API_KEY"].Required {
		t.Errorf("STRIPE_API_KEY should match _API_KEY rule: %+v", byName["STRIPE_API_KEY"])
	}
	if byName["CUSTOM_FLAG"].Required {
		t.Error("CUSTOM_FLAG with no rule match should be required=false")
	}
}

func TestDetectRequiredSecrets_DedupAcrossFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".env.example"), "ANTHROPIC_API_KEY=\n")
	mustWrite(t, filepath.Join(root, ".env.template"), "ANTHROPIC_API_KEY=\nOPENAI_API_KEY=\n")
	got, err := DetectRequiredSecrets(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 distinct keys, got %d (%+v)", len(got), got)
	}
}

func TestDetectRequiredSecrets_NoSamples(t *testing.T) {
	got, err := DetectRequiredSecrets(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestReadEnvKeys_IgnoresValues(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".env.example"),
		"export FOO=secret-value-do-not-read\n# comment\nBAR=baz\n=missingkey\n")
	keys, err := readEnvKeys(filepath.Join(root, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "FOO" || keys[1] != "BAR" {
		t.Errorf("got %+v, want [FOO BAR]", keys)
	}
}
