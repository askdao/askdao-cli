package providers

import (
	"encoding/json"
	"regexp"

	"github.com/askdao/askdao-cli/internal/types"
)

// NodeProvider ports detect / framework-inference of nixpacks node/mod.rs.
type NodeProvider struct {
	Pkgs map[string][]types.Package
}

// NodePackageManager mirrors nixpacks node lockfile selection.
type NodePackageManager string

const (
	PMNpm  NodePackageManager = "npm"
	PMPnpm NodePackageManager = "pnpm"
	PMYarn NodePackageManager = "yarn"
	PMBun  NodePackageManager = "bun"
)

// Name implements Provider.
func (p *NodeProvider) Name() string { return "node" }

// Detect implements Provider. Triggers on package.json (the canonical npm
// manifest); deno / bun-only projects are picked up by other providers if
// they exist (out of scope for #4).
func (p *NodeProvider) Detect(app *App, _ *Env) (bool, error) {
	return app.IncludesFile("package.json"), nil
}

// SelectPackageManager picks the lockfile-strongest match, defaulting to npm.
func (p *NodeProvider) SelectPackageManager(app *App, env *Env) NodePackageManager {
	if v, ok := env.GetConfigVariable("NODE_PACKAGE_MANAGER"); ok && v != "" {
		return NodePackageManager(v)
	}
	switch {
	case app.IncludesFile("bun.lockb"), app.IncludesFile("bun.lock"):
		return PMBun
	case app.IncludesFile("pnpm-lock.yaml"):
		return PMPnpm
	case app.IncludesFile("yarn.lock"):
		return PMYarn
	default:
		return PMNpm
	}
}

// Plan implements Provider.
func (p *NodeProvider) Plan(app *App, env *Env) (*FrameworkPlan, error) {
	ok, _ := p.Detect(app, env)
	if !ok {
		return nil, nil
	}
	plan := &FrameworkPlan{
		Runtime:    RuntimeHint{Kind: "node"},
		SystemPkgs: map[string][]types.InferredAptPackage{},
	}

	for _, fw := range p.detectFrameworks(app) {
		plan.Frameworks = append(plan.Frameworks, fw)
		for _, e := range fw.Evidence {
			plan.Evidence = append(plan.Evidence, Evidence{Marker: e})
		}
	}
	plan.SystemPkgs["apt"] = InferAptPackages(p.Pkgs)
	plan.ExternalSvc = p.detectExternalServices(app)

	if len(plan.Frameworks) > 0 {
		plan.Confidence = plan.Frameworks[0].Confidence
	}
	return plan, nil
}

// Metadata implements Provider.
func (p *NodeProvider) Metadata(app *App, env *Env) (Metadata, error) {
	return Metadata{
		"package_manager": string(p.SelectPackageManager(app, env)),
	}, nil
}

// nodePackageJSONSubset is the minimum slice of package.json that drives
// framework inference. Scripts / engines etc. are not consumed here.
type nodePackageJSONSubset struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// readPackageJSON returns the parsed deps maps; nil-safe.
func readPackageJSON(app *App) (*nodePackageJSONSubset, error) {
	data, err := app.ReadFile("package.json")
	if err != nil {
		return nil, err
	}
	var pj nodePackageJSONSubset
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil, err
	}
	return &pj, nil
}

// nodeFrameworkProbe is a Node-specific probe; the npm namespace lets us
// match scoped packages (`@nestjs/core`) without relying on import scans.
type nodeFrameworkProbe struct {
	weight float64
	marker string
	check  func(p *NodeProvider, app *App, pj *nodePackageJSONSubset) bool
}

type nodeFrameworkRule struct {
	name     string
	probes   []nodeFrameworkProbe
	minScore float64
}

var nodeFrameworkRules = []nodeFrameworkRule{
	{
		name:     "Next.js",
		minScore: 0.5,
		probes: []nodeFrameworkProbe{
			{weight: 0.5, marker: "uses_dep:next", check: hasNpmDep("next")},
			{weight: 0.3, marker: "next.config.js present", check: hasNodeFile("next.config.js")},
			{weight: 0.3, marker: "next.config.mjs present", check: hasNodeFile("next.config.mjs")},
			{weight: 0.3, marker: "next.config.ts present", check: hasNodeFile("next.config.ts")},
		},
	},
	{
		name:     "Nuxt",
		minScore: 0.5,
		probes: []nodeFrameworkProbe{
			{weight: 0.5, marker: "uses_dep:nuxt", check: hasNpmDep("nuxt")},
			{weight: 0.3, marker: "nuxt.config.ts present", check: hasNodeFile("nuxt.config.ts")},
			{weight: 0.3, marker: "nuxt.config.js present", check: hasNodeFile("nuxt.config.js")},
		},
	},
	{
		name:     "Express",
		minScore: 0.5,
		probes: []nodeFrameworkProbe{
			{weight: 0.5, marker: "uses_dep:express", check: hasNpmDep("express")},
			{weight: 0.4, marker: "import_pattern:require('express')", check: nodeImportsPattern(`require\(['"]express['"]\)`, "**/*.js")},
		},
	},
	{
		name:     "NestJS",
		minScore: 0.5,
		probes: []nodeFrameworkProbe{
			{weight: 0.6, marker: "uses_dep:@nestjs/core", check: hasNpmDep("@nestjs/core")},
			{weight: 0.3, marker: "nest-cli.json present", check: hasNodeFile("nest-cli.json")},
		},
	},
	{
		name:     "Vite",
		minScore: 0.5,
		probes: []nodeFrameworkProbe{
			{weight: 0.5, marker: "uses_dep:vite", check: hasNpmDep("vite")},
			{weight: 0.3, marker: "vite.config.ts present", check: hasNodeFile("vite.config.ts")},
			{weight: 0.3, marker: "vite.config.js present", check: hasNodeFile("vite.config.js")},
		},
	},
	{
		name:     "React",
		minScore: 0.5,
		probes: []nodeFrameworkProbe{
			{weight: 0.5, marker: "uses_dep:react", check: hasNpmDep("react")},
			{weight: 0.3, marker: "uses_dep:react-dom", check: hasNpmDep("react-dom")},
		},
	},
}

func (p *NodeProvider) detectFrameworks(app *App) []types.DetectedFramework {
	pj, _ := readPackageJSON(app)
	if pj == nil {
		pj = &nodePackageJSONSubset{}
	}

	var out []types.DetectedFramework
	for _, rule := range nodeFrameworkRules {
		score := 0.0
		var evidence []string
		for _, probe := range rule.probes {
			if probe.check(p, app, pj) {
				score += probe.weight
				evidence = append(evidence, probe.marker)
			}
		}
		if score < rule.minScore || len(evidence) == 0 {
			continue
		}
		if score > 1.0 {
			score = 1.0
		}
		out = append(out, types.DetectedFramework{
			Name:       rule.name,
			Confidence: score,
			Evidence:   evidence,
		})
	}
	return out
}

func (p *NodeProvider) detectExternalServices(_ *App) []types.DetectedExternalService {
	var out []types.DetectedExternalService
	if HasDep(p.Pkgs, "npm", "pg") || HasDep(p.Pkgs, "npm", "postgres") {
		out = append(out, types.DetectedExternalService{
			Service: "PostgreSQL", Confidence: 0.9, Evidence: []string{"uses_dep:pg/postgres"},
		})
	}
	if HasDep(p.Pkgs, "npm", "mysql2") || HasDep(p.Pkgs, "npm", "mysql") {
		out = append(out, types.DetectedExternalService{
			Service: "MySQL", Confidence: 0.9, Evidence: []string{"uses_dep:mysql2/mysql"},
		})
	}
	if HasDep(p.Pkgs, "npm", "ioredis") || HasDep(p.Pkgs, "npm", "redis") {
		out = append(out, types.DetectedExternalService{
			Service: "Redis", Confidence: 0.85, Evidence: []string{"uses_dep:ioredis/redis"},
		})
	}
	if HasDep(p.Pkgs, "npm", "@anthropic-ai/sdk") {
		out = append(out, types.DetectedExternalService{
			Service: "Anthropic API", Confidence: 0.99, Evidence: []string{"uses_dep:@anthropic-ai/sdk"},
		})
	}
	if HasDep(p.Pkgs, "npm", "openai") {
		out = append(out, types.DetectedExternalService{
			Service: "OpenAI API", Confidence: 0.99, Evidence: []string{"uses_dep:openai"},
		})
	}
	return out
}

func hasNpmDep(name string) func(p *NodeProvider, app *App, pj *nodePackageJSONSubset) bool {
	return func(p *NodeProvider, _ *App, pj *nodePackageJSONSubset) bool {
		// Prefer the syft-derived dev-filtered list when present (more accurate
		// than reading package.json directly because it sees lockfile reality).
		if HasDep(p.Pkgs, "npm", name) {
			return true
		}
		// Fallback to package.json declaration so providers still work without
		// a syft-driven scan upstream.
		if pj == nil {
			return false
		}
		_, ok := pj.Dependencies[name]
		return ok
	}
}

func hasNodeFile(rel string) func(p *NodeProvider, app *App, pj *nodePackageJSONSubset) bool {
	return func(_ *NodeProvider, app *App, _ *nodePackageJSONSubset) bool {
		return app.IncludesFile(rel)
	}
}

func nodeImportsPattern(pattern, glob string) func(p *NodeProvider, app *App, pj *nodePackageJSONSubset) bool {
	re := regexp.MustCompile(pattern)
	return func(_ *NodeProvider, app *App, _ *nodePackageJSONSubset) bool {
		return app.FindMatch(re, glob)
	}
}
