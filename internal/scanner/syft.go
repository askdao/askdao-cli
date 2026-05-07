// Package scanner implements askdao-cli's L1-L2 deterministic scanners.
//
// [INPUT]: 项目根目录路径 + 排除 glob
// [OUTPUT]: 包列表 / 语言占比 / Dockerfile AST，喂给 internal/types.Detection
// [POS]: L1-L2 真相源；L3 providers + L4 LLM 都消费这里的输出
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"

	"github.com/askdao/askdao-cli/internal/types"
)

// DefaultSyftExcludes is applied unless caller overrides — the openviking
// submodule produced 1159 noisy artifacts in the conductor spike (see
// docs/investigations/syft-spike-for-askdao-cli.md §3).
var DefaultSyftExcludes = []string{"./openviking/**"}

// SyftRunner abstracts spawning the syft binary so tests can inject canned
// JSON without requiring syft on PATH.
type SyftRunner func(ctx context.Context, args []string) ([]byte, error)

// DefaultSyftRunner spawns the system `syft` binary; used in production.
func DefaultSyftRunner(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "syft", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("syft: %w (stderr: %s)", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// SyftOptions tunes a single ScanPackages invocation.
type SyftOptions struct {
	// Excludes are appended to DefaultSyftExcludes; pass nil to keep defaults.
	Excludes []string
	// Runner injects an alternative syft launcher (defaults to DefaultSyftRunner).
	Runner SyftRunner
}

// syftJSON is the minimal subset of `syft -o syft-json` we consume.
type syftJSON struct {
	Artifacts []syftArtifact `json:"artifacts"`
}

type syftArtifact struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Type     string `json:"type"`
	Language string `json:"language,omitempty"`
}

// ScanPackages runs syft against root and returns packages keyed by ecosystem
// (pip / npm / gem / cargo / go / github_actions / ...). All packages are
// returned with IsProd=true; the dev/prod refinement layer (issue #3) flips
// IsProd to false for entries that match a manifest's dev scope.
func ScanPackages(ctx context.Context, root string, opts SyftOptions) (map[string][]types.Package, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}
	runner := opts.Runner
	if runner == nil {
		runner = DefaultSyftRunner
	}

	excludes := append([]string{}, DefaultSyftExcludes...)
	excludes = append(excludes, opts.Excludes...)

	args := []string{"dir:" + root, "-o", "syft-json", "--quiet"}
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}

	out, err := runner(ctx, args)
	if err != nil {
		return nil, err
	}

	var raw syftJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("scanner: parse syft json: %w", err)
	}

	grouped := map[string][]types.Package{}
	for _, a := range raw.Artifacts {
		ecosystem := mapSyftTypeToEcosystem(a.Type)
		if ecosystem == "" {
			continue
		}
		grouped[ecosystem] = append(grouped[ecosystem], types.Package{
			Name:    a.Name,
			Version: a.Version,
			IsProd:  true,
		})
	}

	for k := range grouped {
		sort.Slice(grouped[k], func(i, j int) bool {
			return grouped[k][i].Name < grouped[k][j].Name
		})
	}
	return grouped, nil
}

// mapSyftTypeToEcosystem normalizes syft's package `type` field onto the
// detection.json ecosystem keys defined in design.md §4.
func mapSyftTypeToEcosystem(syftType string) string {
	switch syftType {
	case "python":
		return "pip"
	case "npm", "javascript":
		return "npm"
	case "gem":
		return "gem"
	case "rust-crate":
		return "cargo"
	case "go-module":
		return "go"
	case "github-action", "github-action-workflow":
		return "github_actions"
	case "java-archive":
		return "maven"
	case "dotnet":
		return "nuget"
	case "":
		return ""
	default:
		// Pass unknown ecosystems through under their syft-native key so we do
		// not silently lose information.
		return syftType
	}
}
