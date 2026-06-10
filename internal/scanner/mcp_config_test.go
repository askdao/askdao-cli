package scanner

import (
	"path/filepath"
	"testing"
)

func TestDetectMCPConfigs_MixedTransports(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".mcp.json"), `{
      "mcpServers": {
        "github": {
          "type": "url",
          "url": "https://api.githubcopilot.com/mcp/"
        },
        "filesystem": {
          "type": "stdio",
          "command": "mcp-server-filesystem"
        }
      }
    }`)

	got, err := DetectMCPConfigs(root, ScanScopeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 source, got %d", len(got))
	}
	cfg := got[0]
	if cfg.Source != ".mcp.json" {
		t.Errorf("source = %q", cfg.Source)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("want 2 servers, got %d", len(cfg.Servers))
	}
	// Sorted by name: filesystem before github.
	if cfg.Servers[0].Name != "filesystem" || cfg.Servers[1].Name != "github" {
		t.Errorf("ordering: %+v", cfg.Servers)
	}
	if cfg.Servers[0].AnthropicCompatible {
		t.Errorf("stdio should be incompatible")
	}
	if cfg.Servers[0].Warning == "" {
		t.Errorf("stdio should carry a warning")
	}
	if !cfg.Servers[1].AnthropicCompatible {
		t.Errorf("url should be compatible")
	}
}

func TestDetectMCPConfigs_TypeInference(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".mcp.json"), `{
      "mcpServers": {
        "no-type-url":     {"url": "https://x"},
        "no-type-stdio":   {"command": "foo"}
      }
    }`)
	got, err := DetectMCPConfigs(root, ScanScopeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got[0].Servers {
		switch s.Name {
		case "no-type-url":
			if s.Type != "url" {
				t.Errorf("expected url inference, got %q", s.Type)
			}
		case "no-type-stdio":
			if s.Type != "stdio" {
				t.Errorf("expected stdio inference, got %q", s.Type)
			}
		}
	}
}

// Regression: Claude Code / Cowork label remote MCP servers "http" or "sse"
// (Anthropic's spec calls the same remote transport "url"). These ARE
// deployable to Managed Agents and must normalize to "url" + compatible — the
// gateway at mcp.askdao.ai/mcp is exactly such a server. Only stdio is dropped.
func TestDetectMCPConfigs_RemoteHTTPCompatible(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".mcp.json"), `{
      "mcpServers": {
        "askdao-voice": {"type": "http", "url": "https://mcp.askdao.ai/mcp"},
        "sse-server":   {"type": "sse",  "url": "https://example.com/sse"},
        "local-fs":     {"type": "stdio", "command": "mcp-server-filesystem"}
      }
    }`)
	got, err := DetectMCPConfigs(root, ScanScopeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got[0].Servers {
		switch s.Name {
		case "askdao-voice", "sse-server":
			if s.Type != "url" {
				t.Errorf("%s: remote transport should normalize to url, got %q", s.Name, s.Type)
			}
			if !s.AnthropicCompatible {
				t.Errorf("%s: remote (http/sse) MCP must be Anthropic-compatible", s.Name)
			}
			if s.Warning != "" {
				t.Errorf("%s: compatible server should carry no warning, got %q", s.Name, s.Warning)
			}
		case "local-fs":
			if s.AnthropicCompatible {
				t.Errorf("local-fs: stdio must remain incompatible")
			}
			if s.Warning == "" {
				t.Errorf("local-fs: stdio should carry a warning")
			}
		}
	}
}

func TestDetectMCPConfigs_Missing(t *testing.T) {
	got, err := DetectMCPConfigs(t.TempDir(), ScanScopeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestDetectMCPConfigs_Malformed(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".mcp.json"), "not-json")
	got, err := DetectMCPConfigs(root, ScanScopeOpts{})
	if err != nil {
		t.Fatalf("malformed JSON should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("malformed JSON should yield no entries, got %+v", got)
	}
}

func TestDetectMCPConfigs_UserScope(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	// Project .claude/ marker activates claude user-scope discovery.
	mustWrite(t, filepath.Join(root, ".claude", "settings.json"), "{}")
	mustWrite(t, filepath.Join(home, ".claude.json"),
		`{"mcpServers":{"global-mcp":{"type":"url","url":"https://x"}}}`)

	got, err := DetectMCPConfigs(root, ScanScopeOpts{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cfg := range got {
		if cfg.Scope == "user" && cfg.Harness == "claude" {
			for _, s := range cfg.Servers {
				if s.Name == "global-mcp" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected user-scope claude MCP 'global-mcp', got %+v", got)
	}
}

func TestDetectMCPConfigs_CodexProjectTOML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".codex", "config.toml"), `
model = "gpt-5.1" # unrelated keys must be ignored

[mcp_servers.askdao-mcp]
url = "https://mcp.askdao.ai/mcp"
bearer_token_env_var = "ASKDAO_MCP_TOKEN"

[mcp_servers.local-fs]
command = "mcp-server-filesystem"
args = ["--root", "/tmp"]
`)

	got, err := DetectMCPConfigs(root, ScanScopeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 source, got %d: %+v", len(got), got)
	}
	cfg := got[0]
	if cfg.Source != ".codex/config.toml" || cfg.Scope != "project" || cfg.Harness != "codex" {
		t.Errorf("source/scope/harness = %q/%q/%q", cfg.Source, cfg.Scope, cfg.Harness)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("want 2 servers, got %+v", cfg.Servers)
	}
	// Sorted by name: askdao-mcp before local-fs.
	remote, local := cfg.Servers[0], cfg.Servers[1]
	if remote.Name != "askdao-mcp" || remote.Type != "url" || !remote.AnthropicCompatible {
		t.Errorf("remote: %+v", remote)
	}
	if local.Name != "local-fs" || local.Type != "stdio" || local.AnthropicCompatible {
		t.Errorf("local: %+v", local)
	}
	if local.Command != "mcp-server-filesystem --root /tmp" {
		t.Errorf("args should join into Command, got %q", local.Command)
	}
	if local.Warning == "" {
		t.Errorf("stdio should carry a warning")
	}
}

func TestDetectMCPConfigs_CodexUserScope(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	// Project .agents/ marker activates codex user-scope discovery.
	mustWrite(t, filepath.Join(root, ".agents", "skills", ".keep"), "")
	mustWrite(t, filepath.Join(home, ".codex", "config.toml"), `
[mcp_servers.global-codex-mcp]
url = "https://mcp.askdao.ai/mcp"
`)

	got, err := DetectMCPConfigs(root, ScanScopeOpts{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cfg := range got {
		if cfg.Scope == "user" && cfg.Harness == "codex" {
			for _, s := range cfg.Servers {
				if s.Name == "global-codex-mcp" && s.Type == "url" && s.AnthropicCompatible {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected user-scope codex MCP 'global-codex-mcp', got %+v", got)
	}
}

func TestDetectMCPConfigs_MalformedTOML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".codex", "config.toml"), "not [[ valid toml =")
	got, err := DetectMCPConfigs(root, ScanScopeOpts{})
	if err != nil {
		t.Fatalf("malformed TOML should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("malformed TOML should yield no entries, got %+v", got)
	}
}

func TestDetectMCPConfigs_CoworkUserScope(t *testing.T) {
	root := t.TempDir() // Cowork is markerless — no project marker required.
	home := t.TempDir()
	// Force home-fallback so appDataDir is deterministic across CI OSes.
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	// Cowork's MCP lives in claude_desktop_config.json under the app-data dir.
	cfgPath := filepath.Join(appDataDir(home), "Claude", "claude_desktop_config.json")
	mustWrite(t, cfgPath, `{"mcpServers":{"cowork-mcp":{"type":"url","url":"https://y"}}}`)

	got, err := DetectMCPConfigs(root, ScanScopeOpts{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cfg := range got {
		if cfg.Scope == "user" && cfg.Harness == "cowork" {
			for _, s := range cfg.Servers {
				if s.Name == "cowork-mcp" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("expected user-scope cowork MCP 'cowork-mcp', got %+v", got)
	}
}
