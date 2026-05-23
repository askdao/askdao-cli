// [INPUT]: 依赖 internal/types 的 Capabilities/Capability/DetectedToolRiskHints；同包 defaultShellPolicy
// [OUTPUT]: 对外提供 DefaultCapabilities
// [POS]: recommender 的 capabilities 确定性真相源 —— capabilities 是 hard field（4 固定槽 + 有限枚举值），
//
//	像 skills(design.md §9.13) 一样本地 deterministic 生成、不交 LLM 即兴自由文本。
//	被 MockClient（llm.go）+ cmd/askdao/edit.go loadOrScan 消费覆盖 spec.Capabilities。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package recommender

import "github.com/askdao/askdao-cli/internal/types"

// DefaultCapabilities returns the deterministic capability set. capabilities is a
// hard field (4 fixed slots, each with a small enum of permission + scope values),
// so — like skills (design.md §9.13) — it is built here, not left to the LLM's
// free text.
//
// The Anthropic Managed Agents adapter reads only enabled + permission (mapped to
// each tool's permission_policy). scopes are askdao's harness-neutral *intent*
// layer: Anthropic ignores them today (no filesystem scoping primitive), future
// harnesses (OpenAI/E2B) may enforce them. Keeping scopes a fixed vocabulary
// (not LLM free text like "read files from input/") makes the yaml规范、可读、可移植.
//
// shell.permission follows the production-signal heuristic (gate bash on prod);
// the other three auto-run (always_allow) — appropriate for the self-use 修身期.
func DefaultCapabilities(policy types.DetectedToolRiskHints) types.Capabilities {
	return types.Capabilities{
		Shell:         types.Capability{Enabled: true, Permission: defaultShellPolicy(policy), Scopes: []string{"read", "write", "execute"}},
		Filesystem:    types.Capability{Enabled: true, Permission: "always_allow", Scopes: []string{"read", "write"}},
		Web:           types.Capability{Enabled: true, Permission: "always_allow", Scopes: []string{"fetch"}},
		CodeExecution: types.Capability{Enabled: true, Permission: "always_allow", Scopes: []string{"javascript", "shell"}},
	}
}
