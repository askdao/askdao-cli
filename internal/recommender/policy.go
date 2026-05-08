// Package recommender implements askdao-cli's L4 inference layer:
//   - policy.go infers tool permission_policy from on-disk production / user-data
//     signals (deterministic — no LLM involved).
//   - llm.go talks to conductor's recommend endpoint to convert detection.json
//     into the harness-neutral intermediate format yaml.
//
// Per design.md decision 9.1, LLM calls are routed via conductor (not direct
// BYOK), so the askdao-cli recommender only needs an HTTP client + a mock for
// hermetic development.
//
// [INPUT]: 项目根目录 + 可选 conductor endpoint
// [OUTPUT]: types.DetectedToolRiskHints (policy 层) + types.AgentSpec (LLM 层)
// [POS]: L4 边界 —— L1-L3 全确定性，本目录开始进入 LLM 模糊推断
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package recommender

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// productionPathPatterns are the well-known files / directories that indicate
// the project ships to a real production environment. Each rule's path is
// evaluated relative to the project root.
//
// The signal text is what surfaces in detection.json
// detected_tool_risk_hints.production_signals[].signal — KOL-readable.
var productionPathPatterns = []signalRule{
	{path: ".github/workflows/deploy.yml", signal: "AWS deploy workflow detected"},
	{path: ".github/workflows/release.yml", signal: "GitHub release workflow detected"},
	{path: ".env.production", signal: "Production env file referenced"},
	{path: ".env.prod", signal: "Production env file referenced"},
	{path: "production.toml", signal: "Production config file present"},
	{path: "config/production.toml", signal: "Production config file present"},
	{path: "config/production.yml", signal: "Production config file present"},
	{path: "config/production.yaml", signal: "Production config file present"},
	{path: "pulumi.yaml", signal: "Pulumi infrastructure file present"},
	{path: "Chart.yaml", signal: "Helm chart present"},
}

// productionDirPatterns trigger when the directory itself exists, regardless of
// contents (presence is a strong enough signal).
var productionDirPatterns = []signalRule{
	{path: "terraform", signal: "Terraform manifests directory present"},
	{path: "k8s", signal: "Kubernetes manifests directory present"},
	{path: "kubernetes", signal: "Kubernetes manifests directory present"},
	{path: "helm", signal: "Helm chart directory present"},
	{path: "deploy", signal: "deploy/ directory present"},
}

// productionGlobs match a category of files (e.g. *.tf) — first hit emits a
// single signal entry.
var productionGlobs = []signalRule{
	{path: "*.tf", signal: "Terraform .tf files at project root"},
	{path: ".github/workflows/deploy-*.yml", signal: "AWS deploy workflow detected"},
	{path: ".github/workflows/release-*.yml", signal: "GitHub release workflow detected"},
}

// userDataPatterns hint at presence of real user data — currently advisory
// only, surfaced into UserDataSignals but not changing default policy. Keeps
// the recommender conservative.
var userDataPatterns = []signalRule{
	{path: "users.csv", signal: "users.csv at project root"},
	{path: "customers.csv", signal: "customers.csv at project root"},
	{path: "data", signal: "data/ directory present"},
	{path: "datasets", signal: "datasets/ directory present"},
}

type signalRule struct {
	path   string // relative path or glob (file or dir)
	signal string // human-readable signal text
}

// InferToolRiskHints walks the well-known signal locations under root and
// produces the DetectedToolRiskHints recommendation. Default policy stays
// always_allow; production signals add per-tool overrides for `bash` / `write`
// to always_ask, mirroring design.md §4 example.
//
// User-data signals are recorded but currently do not flip the default — the
// upstream LLM recommender takes them as advisory.
func InferToolRiskHints(root string) (types.DetectedToolRiskHints, error) {
	if root == "" {
		return types.DetectedToolRiskHints{}, errors.New("recommender: root must be non-empty")
	}

	var prod, userData []types.ToolRiskSignal
	seen := map[string]bool{}

	addProd := func(signal, evidence string) {
		key := signal + "|" + evidence
		if seen[key] {
			return
		}
		seen[key] = true
		prod = append(prod, types.ToolRiskSignal{Signal: signal, Evidence: evidence})
	}
	addUserData := func(signal, evidence string) {
		key := signal + "|" + evidence
		if seen[key] {
			return
		}
		seen[key] = true
		userData = append(userData, types.ToolRiskSignal{Signal: signal, Evidence: evidence})
	}

	for _, rule := range productionPathPatterns {
		if fileExistsAt(filepath.Join(root, rule.path)) {
			addProd(rule.signal, rule.path)
		}
	}
	for _, rule := range productionDirPatterns {
		if dirExistsAt(filepath.Join(root, rule.path)) {
			addProd(rule.signal, rule.path)
		}
	}
	for _, rule := range productionGlobs {
		if matched, _ := globExists(root, rule.path); matched != "" {
			addProd(rule.signal, matched)
		}
	}
	for _, rule := range userDataPatterns {
		full := filepath.Join(root, rule.path)
		if fileExistsAt(full) || dirExistsAt(full) {
			addUserData(rule.signal, rule.path)
		}
	}

	out := types.DetectedToolRiskHints{
		ProductionSignals:        prod,
		UserDataSignals:          userData,
		RecommendedDefaultPolicy: "always_allow",
	}
	if len(prod) > 0 {
		out.ToolOverridesRecommended = []types.ToolPolicyOverride{
			{
				Tool:   "bash",
				Policy: "always_ask",
				Reason: "Production deploy detected; bash should require approval",
			},
			{
				Tool:   "write",
				Policy: "always_ask",
				Reason: "Production deploy detected; write should require approval",
			},
		}
	}
	return out, nil
}

func fileExistsAt(path string) bool {
	info, err := osStat(path)
	return err == nil && !info.IsDir()
}

func dirExistsAt(path string) bool {
	info, err := osStat(path)
	return err == nil && info.IsDir()
}

// osStat is a tiny indirection so that tests in the same package can stub
// without taking on a global flag — keeps policy.go decoupled from os
// directly.
var osStat = func(path string) (fs.FileInfo, error) {
	return defaultStat(path)
}

// globExists walks root non-recursively for top-level matches; for nested
// patterns like ".github/workflows/deploy-*.yml" it splits on the directory
// part and matches the basename inside.
func globExists(root, pattern string) (string, error) {
	dir, name := filepath.Split(pattern)
	target := filepath.Join(root, dir)
	entries, err := readDir(target)
	if err != nil {
		return "", nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ok, _ := filepath.Match(name, e.Name())
		if ok {
			return strings.TrimPrefix(filepath.ToSlash(filepath.Join(dir, e.Name())), "/"), nil
		}
	}
	return "", nil
}
