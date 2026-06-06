package scanner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/askdao/askdao-cli/internal/types"
)

// mcpConfigSources are the project-scope MCP config files we probe. User-scope
// configs (Claude Code's ~/.claude.json, Cowork's claude_desktop_config.json)
// are added separately via activeHarnesses. We record the source path verbatim
// so KOLs can trace which file fed the recommendation.
var mcpConfigSources = []string{
	".mcp.json",
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
// A missing file or malformed JSON yields (nil, nil) so the scan continues; only
// an unexpected read error propagates. Returns nil when zero servers declared.
func readMCPConfig(path, source, scope, harness string) (*types.DetectedMCPConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw mcpFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil // malformed → skip this source
	}
	var servers []types.MCPServerConfig
	for name, s := range raw.MCPServers {
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
