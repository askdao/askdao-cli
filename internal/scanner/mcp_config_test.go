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
