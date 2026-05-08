package scanner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/askdao/askdao-cli/internal/types"
)

// mcpConfigSources are the well-known MCP config files we probe in priority
// order. Each `host` is the runtime that originally writes it; we record the
// source path verbatim so KOLs can trace which file fed the recommendation.
var mcpConfigSources = []string{
	".mcp.json",
	".cursor/mcp.json",
	"claude_desktop_config.json",
}

// mcpFile mirrors the canonical `mcpServers` map shape that all three known
// hosts (Claude Code, Claude Desktop, Cursor) share.
type mcpFile struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

type mcpServer struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	Command string `json:"command"`
}

// DetectMCPConfigs reads every known MCP config file at root and returns one
// DetectedMCPConfig per file present, with each server tagged for Anthropic
// Managed Agents compatibility (only `type: url` is portable).
func DetectMCPConfigs(root string) ([]types.DetectedMCPConfig, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}
	var out []types.DetectedMCPConfig
	for _, rel := range mcpConfigSources {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var raw mcpFile
		if err := json.Unmarshal(data, &raw); err != nil {
			// Malformed config shouldn't stop the whole scan — record nothing
			// for this source and move on.
			continue
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
			compat := t == "url"
			warn := ""
			if !compat {
				warn = "Anthropic Managed Agents only supports type=url; stdio MCP cannot be deployed"
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
			continue
		}
		out = append(out, types.DetectedMCPConfig{Source: rel, Servers: servers})
	}
	return out, nil
}
