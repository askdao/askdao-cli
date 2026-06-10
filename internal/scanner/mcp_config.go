package scanner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/askdao/askdao-cli/internal/types"
)

// mcpConfigSources are the project-scope MCP config files we probe. User-scope
// configs (Claude Code's ~/.claude.json, Codex's ~/.codex/config.toml, Cowork's
// claude_desktop_config.json) are added separately via activeHarnesses. We
// record the source path verbatim so KOLs can trace which file fed the
// recommendation.
var mcpConfigSources = []string{
	".mcp.json",
	".codex/config.toml",
}

// mcpFile mirrors the canonical `mcpServers` map shape shared by the project
// .mcp.json and the user-scope configs (Claude Code, Cowork/Claude Desktop).
type mcpFile struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

type mcpServer struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	Command string `json:"command"`
}

// codexConfigFile mirrors the subset of Codex's config.toml we care about:
// [mcp_servers.<id>] tables (https://developers.openai.com/codex/mcp). The
// rest of the file (model, approvals, …) is ignored by the TOML decoder.
type codexConfigFile struct {
	MCPServers map[string]codexMCPServer `toml:"mcp_servers"`
}

// codexMCPServer is one [mcp_servers.<id>] table. url marks a remote server;
// command (+args) a local stdio subprocess. Codex's bearer_token_env_var is
// auth plumbing, not transport — it never affects compatibility gating, so we
// don't decode it.
type codexMCPServer struct {
	URL     string   `toml:"url"`
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// DetectMCPConfigs reads every known MCP config file across two scopes:
//
//   - project scope: mcpConfigSources under root (Scope="project")
//   - user scope: the global MCP config of whichever harnesses the project root
//     marks (a .claude/ project pulls in ~/.claude.json) — Scope="user". Gated
//     by opts.HomeDir; empty HomeDir skips user scope.
//
// Each server is tagged for Anthropic Managed Agents compatibility: any remote
// (URL-bearing) transport is portable and normalized to `type: url` — Claude
// Code labels these `http`/`sse` — while stdio (local subprocess) is not.
func DetectMCPConfigs(root string, opts ScanScopeOpts) ([]types.DetectedMCPConfig, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}
	var out []types.DetectedMCPConfig

	// Project scope — Source stays project-relative.
	for _, rel := range mcpConfigSources {
		cfg, err := readMCPConfig(filepath.Join(root, rel), rel, "project", harnessForProjectDir(rel))
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			out = append(out, *cfg)
		}
	}

	// User scope — global MCP config, gated by the project's harness markers.
	if opts.HomeDir != "" {
		for _, h := range activeHarnesses(root, opts.HomeDir) {
			for _, p := range h.userMCPFiles {
				path := p.resolve(opts.HomeDir)
				cfg, err := readMCPConfig(path, path, "user", h.name)
				if err != nil {
					return nil, err
				}
				if cfg != nil {
					out = append(out, *cfg)
				}
			}
		}
	}
	return out, nil
}

// readMCPConfig parses one MCP config file. source is recorded verbatim into
// DetectedMCPConfig.Source; scope ("project"|"user") and harness tag the result.
// A missing file or malformed content yields (nil, nil) so the scan continues;
// only an unexpected read error propagates. Returns nil when zero servers
// declared. Format is dispatched by extension: .toml → Codex [mcp_servers.<id>]
// tables, everything else → the canonical JSON {"mcpServers": {...}} shape.
func readMCPConfig(path, source, scope, harness string) (*types.DetectedMCPConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	raw := parseMCPServers(path, data)
	var servers []types.MCPServerConfig
	for name, s := range raw {
		t := s.Type
		if t == "" {
			if s.URL != "" {
				t = "url"
			} else if s.Command != "" {
				t = "stdio"
			}
		}
		// Anthropic Managed Agents deploys remote (URL-based) MCP servers; its
		// spec calls the transport "url". Claude Code / Cowork label the same
		// remote transport "http" or "sse". Collapse every URL-bearing remote
		// transport to the portable "url"; only stdio (a local subprocess) is
		// non-deployable.
		if s.URL != "" && t != "stdio" {
			t = "url"
		}
		compat := t == "url"
		warn := ""
		if !compat {
			warn = "Anthropic Managed Agents only supports remote (url) MCP servers; stdio MCP cannot be deployed"
		}
		servers = append(servers, types.MCPServerConfig{
			Name:                name,
			Type:                t,
			URL:                 s.URL,
			Command:             s.Command,
			AnthropicCompatible: compat,
			Warning:             warn,
		})
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	if len(servers) == 0 {
		return nil, nil
	}
	return &types.DetectedMCPConfig{Source: source, Scope: scope, Harness: harness, Servers: servers}, nil
}

// parseMCPServers decodes raw config bytes into the shared mcpServer map,
// dispatching JSON vs TOML on the file extension. Malformed content yields nil
// (the caller skips the source without failing the scan).
func parseMCPServers(path string, data []byte) map[string]mcpServer {
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		var cfg codexConfigFile
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return nil
		}
		out := make(map[string]mcpServer, len(cfg.MCPServers))
		for name, s := range cfg.MCPServers {
			cmd := s.Command
			if cmd != "" && len(s.Args) > 0 {
				cmd += " " + strings.Join(s.Args, " ")
			}
			out[name] = mcpServer{URL: s.URL, Command: cmd}
		}
		return out
	}
	var raw mcpFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw.MCPServers
}
