package providers

import (
	"regexp"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// GoProvider ports detect / framework-inference of nixpacks go.rs. Plan
// extracts the go directive version, build tags, and cgo signal — enough to
// hand recommender + adapter a faithful runtime hint.
type GoProvider struct{}

// Name implements Provider.
func (p *GoProvider) Name() string { return "go" }

// Detect implements Provider. Triggers when go.mod lives at the project root.
// Sub-module workspaces (`go.work`) are also recognized as a Go project.
func (p *GoProvider) Detect(app *App, _ *Env) (bool, error) {
	return app.IncludesFile("go.mod") || app.IncludesFile("go.work"), nil
}

// goDirectiveRe matches `go 1.23` or `go 1.23.4` or `go 1.23rc1` in go.mod.
var goDirectiveRe = regexp.MustCompile(`(?m)^go\s+(\d+(?:\.\d+){0,2}[a-zA-Z0-9.+-]*)`)

// cgoEnvRe matches `// CGO_ENABLED=1` style hints in any .go file's header
// comments — heuristic only.
var cgoImportRe = regexp.MustCompile(`(?m)^\s*import\s+"C"`)

// buildTagRe matches Go 1.17+ `//go:build foo && bar` directives.
var buildTagRe = regexp.MustCompile(`(?m)^//go:build\s+([^\n]+)`)

// Plan implements Provider.
func (p *GoProvider) Plan(app *App, env *Env) (*FrameworkPlan, error) {
	ok, _ := p.Detect(app, env)
	if !ok {
		return nil, nil
	}

	plan := &FrameworkPlan{
		Runtime:    RuntimeHint{Kind: "go"},
		SystemPkgs: map[string][]types.InferredAptPackage{},
	}

	// go directive → runtime version.
	if data, err := app.ReadFile("go.mod"); err == nil {
		if m := goDirectiveRe.FindStringSubmatch(string(data)); m != nil {
			plan.Runtime.Version = m[1]
			plan.Evidence = append(plan.Evidence, Evidence{Marker: "go.mod directive: go " + m[1]})
		}
	}

	// cgo signal → gcc + pkg-config recommended.
	if app.FindMatch(cgoImportRe, "**/*.go") {
		plan.SystemPkgs["apt"] = []types.InferredAptPackage{
			{Name: "gcc", Reason: "cgo `import \"C\"` detected — Go needs a C toolchain"},
			{Name: "pkg-config", Reason: "cgo builds usually call pkg-config to resolve C deps"},
		}
		plan.Evidence = append(plan.Evidence, Evidence{Marker: "cgo:import \"C\""})
	}

	// build tags → surface for diagnostics; not surfaced as framework but
	// recorded as evidence so KOL sees them in the review card.
	for _, f := range app.FindFiles("**/*.go") {
		data, err := app.ReadFile(f)
		if err != nil {
			continue
		}
		if m := buildTagRe.FindStringSubmatch(string(data)); m != nil {
			tag := strings.TrimSpace(m[1])
			plan.Evidence = append(plan.Evidence, Evidence{Marker: "build tag: " + tag})
			break // one example is enough; full enumeration is overkill
		}
	}

	plan.ExternalSvc = p.detectExternalServices(app)
	return plan, nil
}

// Metadata implements Provider.
func (p *GoProvider) Metadata(app *App, _ *Env) (Metadata, error) {
	md := Metadata{}
	if app.IncludesFile("go.work") {
		md["workspace"] = "go.work"
	}
	return md, nil
}

// detectExternalServices for Go scans go.mod require block for well-known
// driver imports. Cross-source evidence is harder than for Python/Node
// (no manifest scope), so we keep this conservative.
func (p *GoProvider) detectExternalServices(app *App) []types.DetectedExternalService {
	data, err := app.ReadFile("go.mod")
	if err != nil {
		return nil
	}
	src := string(data)

	var out []types.DetectedExternalService
	if strings.Contains(src, "github.com/lib/pq") || strings.Contains(src, "github.com/jackc/pgx") {
		out = append(out, types.DetectedExternalService{
			Service: "PostgreSQL", Confidence: 0.9,
			Evidence: []string{"go.mod: lib/pq or jackc/pgx"},
		})
	}
	if strings.Contains(src, "github.com/go-sql-driver/mysql") {
		out = append(out, types.DetectedExternalService{
			Service: "MySQL", Confidence: 0.9,
			Evidence: []string{"go.mod: go-sql-driver/mysql"},
		})
	}
	if strings.Contains(src, "github.com/redis/go-redis") || strings.Contains(src, "github.com/go-redis/redis") {
		out = append(out, types.DetectedExternalService{
			Service: "Redis", Confidence: 0.85,
			Evidence: []string{"go.mod: redis client"},
		})
	}
	if strings.Contains(src, "github.com/anthropics/anthropic-sdk-go") {
		out = append(out, types.DetectedExternalService{
			Service: "Anthropic API", Confidence: 0.99,
			Evidence: []string{"go.mod: anthropic-sdk-go"},
		})
	}
	return out
}
