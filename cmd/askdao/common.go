// [INPUT]: 标准库 os / path/filepath + internal/recommender；resolveServerAndToken（deploy.go 同包）
// [OUTPUT]: askdaoAgentFileName / askdaoDirName 常量 + ensureAskdaoDir / defaultAgentName / chooseLLMClient
// [POS]: cmd/askdao 的共享 helper —— edit/deploy 共用的产物布局常量 + LLM 客户端选择（env/credentials → conductor，否则离线 mock）
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"os"
	"path/filepath"

	"github.com/askdao/askdao-cli/internal/recommender"
)

// askdaoAgentFileName is the project-declaration file written at the project
// root (parallel to Cargo.toml / package.json). Persona lives entirely in its
// persona.system_prompt block — there is no separate persona.md.
const askdaoAgentFileName = "askdao-agent.yml"

// askdaoDirName is the hidden tool-products directory next to the declaration:
// recommendation.yml (diff baseline) + detection.json (scan dump).
const askdaoDirName = ".askdao"

// ensureAskdaoDir creates <root>/.askdao if absent.
func ensureAskdaoDir(root string) error {
	return os.MkdirAll(filepath.Join(root, askdaoDirName), 0o755)
}

// defaultAgentName returns the basename of the absolute project root, falling
// back to "agent" for "/" or unresolvable paths.
func defaultAgentName(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "agent"
	}
	base := filepath.Base(abs)
	if base == "" || base == "/" || base == "." {
		return "agent"
	}
	return base
}

// chooseLLMClient picks the conductor client when both a server URL and bearer
// token resolve (env pair or credentials.json); otherwise falls back to the
// in-process MockClient so edit/scan work offline. Partial env (only one of the
// pair) is surfaced as an error by resolveServerAndToken, not silently mocked.
func chooseLLMClient() recommender.LLMClient {
	url, token, err := resolveServerAndToken()
	if err != nil {
		return &recommender.MockClient{}
	}
	c := recommender.NewConductorClient(url)
	c.AuthToken = token
	return c
}
