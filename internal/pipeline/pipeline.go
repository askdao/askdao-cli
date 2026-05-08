// Package pipeline orchestrates the four askdao-cli layers (scanner →
// dev_filter → providers → policy → optional LLM) into a single Run() call.
// Both `askdao detect` (LLM=nil) and `askdao agent init --auto` (LLM ≠ nil)
// share this entry point.
//
// [INPUT]: 项目根目录 + 可选 LLMClient + 排除 glob
// [OUTPUT]: types.Detection + 可选 RecommendResponse + 软警告
// [POS]: cmd 之下、scanner/providers/recommender 之上 —— 唯一 orchestration
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/askdao/askdao-cli/internal/providers"
	"github.com/askdao/askdao-cli/internal/recommender"
	"github.com/askdao/askdao-cli/internal/scanner"
	"github.com/askdao/askdao-cli/internal/types"
)

// GeneratorVersion stamped into Detection.GeneratorVersion. cmd-layer can
// override at link-time via -ldflags but the default suits dev binaries.
var GeneratorVersion = "askdao-cli/0.1.0-dev"

// Options bundles the inputs Run accepts. Default values produce a useful
// detect report; only the LLM client toggles whether a recommendation is
// produced.
type Options struct {
	// Root is the project directory to scan. Required.
	Root string
	// Excludes are extra syft-style globs appended to the scanner defaults.
	Excludes []string
	// AgentName drives the LLM recommendation request — ignored when LLM nil.
	AgentName string
	// PreferredHarness is set by `--harness <id>` (init / deploy). Empty
	// defers to detected harness signals.
	PreferredHarness string
	// LLM is the recommender client. nil → skip recommendation step entirely.
	LLM recommender.LLMClient
	// SyftRunner overrides the syft invocation; nil uses the binary on PATH.
	// Tests can inject a fake to keep runs hermetic.
	SyftRunner scanner.SyftRunner
	// HomeDir overrides $HOME for harness-signal probing. Empty falls through
	// to os.UserHomeDir().
	HomeDir string
}

// Result is the full pipeline output. ProviderPlans is the per-provider
// FrameworkPlan (kept separate from Detection so cmd-layer can show
// provider-source attribution if needed).
type Result struct {
	Detection      *types.Detection
	ProviderPlans  []ProviderEntry
	Recommendation *recommender.RecommendResponse
	Warnings       []string
}

// ProviderEntry pairs a provider name with the plan it produced.
type ProviderEntry struct {
	Name string
	Plan *providers.FrameworkPlan
}

// Run is the orchestrator entrypoint. It is deterministic up through provider
// inference; the only non-deterministic step is the optional LLM call.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Root == "" {
		return nil, errors.New("pipeline: Options.Root must be set")
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("pipeline: resolve root: %w", err)
	}

	start := time.Now()
	res := &Result{}
	det := &types.Detection{
		SchemaVersion:    types.DetectionSchemaVersion,
		GeneratedAt:      time.Now().UTC(),
		GeneratorVersion: GeneratorVersion,
		DetectedPackages: map[string][]types.Package{},
	}

	// 1. Scanner phase.
	det.DetectedLanguages, _ = scanner.DetectLanguages(root, opts.Excludes)
	det.DetectedRuntimes, _ = scanner.DetectRuntimes(root)

	pkgs, syftWarn := runSyft(ctx, root, opts)
	if syftWarn != "" {
		res.Warnings = append(res.Warnings, syftWarn)
	}
	det.DetectedPackages = pkgs

	if df, err := scanner.ParseDockerfile(filepath.Join(root, "Dockerfile")); err == nil && df.Exists {
		det.DetectedDockerfile = df
	}
	det.DetectedMCPConfigs, _ = scanner.DetectMCPConfigs(root)
	det.DetectedSkills, _ = scanner.DetectSkills(root, pkgs)
	det.DetectedRequiredSecrets, _ = scanner.DetectRequiredSecrets(root, det.DetectedMCPConfigs)
	if hs, err := scanner.DetectHarnessSignals(scanner.HarnessProbeOpts{HomeDir: opts.HomeDir}); err == nil {
		det.DetectedHarnessSignals = hs
	}

	// 2. Apply dev filter — turns syft's IsProd=true placeholders into the
	// real prod/dev split using manifest scopes.
	if err := scanner.ApplyDevFilter(root, pkgs); err != nil {
		res.Warnings = append(res.Warnings, "dev_filter: "+err.Error())
	}

	// 3. Provider phase.
	app, err := providers.NewApp(root)
	if err != nil {
		return nil, fmt.Errorf("pipeline: app index: %w", err)
	}
	env := providers.NewEnv(nil)

	provs := []providers.Provider{
		&providers.PythonProvider{Pkgs: pkgs},
		&providers.NodeProvider{Pkgs: pkgs},
		&providers.GoProvider{},
		&providers.RustProvider{Pkgs: pkgs},
	}
	for _, p := range provs {
		ok, _ := p.Detect(app, env)
		if !ok {
			continue
		}
		plan, err := p.Plan(app, env)
		if err != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("provider %s: %v", p.Name(), err))
			continue
		}
		if plan == nil {
			continue
		}
		res.ProviderPlans = append(res.ProviderPlans, ProviderEntry{Name: p.Name(), Plan: plan})
		det.DetectedFrameworks = append(det.DetectedFrameworks, plan.Frameworks...)
		det.DetectedExternalServices = append(det.DetectedExternalServices, plan.ExternalSvc...)
	}

	det.InferredAptPackages = mergeAptHints(res.ProviderPlans, det.DetectedDockerfile)
	dedupeFrameworks(det)
	dedupeServices(det)

	// 4. Policy phase.
	if hints, err := recommender.InferToolRiskHints(root); err == nil {
		det.DetectedToolRiskHints = hints
	} else {
		res.Warnings = append(res.Warnings, "policy: "+err.Error())
	}

	// 5. Scan metadata that needs the assembled view (file count etc.).
	det.Scan = types.ScanInfo{
		Root:           root,
		ScanDurationMS: time.Since(start).Milliseconds(),
		ExcludedPaths:  append([]string{}, opts.Excludes...),
	}

	res.Detection = det

	// 6. Optional LLM phase.
	if opts.LLM != nil {
		req := recommender.RecommendRequest{
			AgentName:        opts.AgentName,
			PreferredHarness: opts.PreferredHarness,
			Detection:        det,
			ProviderSummary:  toProviderSummaries(res.ProviderPlans),
			Policy:           det.DetectedToolRiskHints,
		}
		resp, err := opts.LLM.Recommend(ctx, req)
		if err != nil {
			return res, fmt.Errorf("recommender: %w", err)
		}
		res.Recommendation = resp
	}

	return res, nil
}

// runSyft spawns syft (or skips with a warning when the binary is absent).
// Returns an empty map + a soft warning rather than failing — the rest of the
// pipeline still produces useful output without packages.
func runSyft(ctx context.Context, root string, opts Options) (map[string][]types.Package, string) {
	runner := opts.SyftRunner
	if runner == nil {
		if _, err := exec.LookPath("syft"); err != nil {
			return map[string][]types.Package{},
				"syft binary not found on PATH — package list will be empty. Install: brew install syft"
		}
	}
	pkgs, err := scanner.ScanPackages(ctx, root, scanner.SyftOptions{
		Excludes: opts.Excludes,
		Runner:   runner,
	})
	if err != nil {
		return map[string][]types.Package{},
			"syft: " + err.Error() + " — proceeding without package data"
	}
	return pkgs, ""
}

// mergeAptHints folds the per-provider apt suggestions plus any
// dockerfile-extracted apt packages into a single deduped list.
func mergeAptHints(plans []ProviderEntry, df *types.DetectedDockerfile) []types.InferredAptPackage {
	seen := map[string]string{}
	for _, e := range plans {
		if e.Plan == nil {
			continue
		}
		for _, a := range e.Plan.SystemPkgs["apt"] {
			if _, ok := seen[a.Name]; !ok {
				seen[a.Name] = a.Reason
			}
		}
	}
	if df != nil {
		for _, name := range df.ExtractedAptPackages {
			if _, ok := seen[name]; !ok {
				seen[name] = "Extracted from Dockerfile RUN apt-get install"
			}
		}
	}
	out := make([]types.InferredAptPackage, 0, len(seen))
	for name, reason := range seen {
		out = append(out, types.InferredAptPackage{Name: name, Reason: reason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func dedupeFrameworks(det *types.Detection) {
	seen := map[string]bool{}
	out := det.DetectedFrameworks[:0]
	for _, f := range det.DetectedFrameworks {
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		out = append(out, f)
	}
	det.DetectedFrameworks = out
}

func dedupeServices(det *types.Detection) {
	seen := map[string]bool{}
	out := det.DetectedExternalServices[:0]
	for _, s := range det.DetectedExternalServices {
		if seen[s.Service] {
			continue
		}
		seen[s.Service] = true
		out = append(out, s)
	}
	det.DetectedExternalServices = out
}

func toProviderSummaries(plans []ProviderEntry) []recommender.ProviderSummary {
	out := make([]recommender.ProviderSummary, 0, len(plans))
	for _, e := range plans {
		if e.Plan == nil {
			continue
		}
		out = append(out, recommender.ProviderSummary{
			Name:       e.Name,
			Frameworks: e.Plan.Frameworks,
			SystemApt:  e.Plan.SystemPkgs["apt"],
			Runtime: recommender.ProviderSummaryRuntime{
				Kind:    e.Plan.Runtime.Kind,
				Version: e.Plan.Runtime.Version,
			},
			Confidence: e.Plan.Confidence,
			External:   e.Plan.ExternalSvc,
		})
	}
	return out
}
