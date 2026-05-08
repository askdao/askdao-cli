package providers

import (
	"regexp"

	"github.com/askdao/askdao-cli/internal/types"
)

// PythonProvider ports the detect / framework-inference half of nixpacks
// python.rs (882 LOC → ~250 LOC Go; we drop install/start phase logic). The
// provider is stateful — Pkgs must be set so framework heuristics can ask
// HasDep — but otherwise read-only.
type PythonProvider struct {
	// Pkgs is the dev-filtered package map produced by internal/scanner. Only
	// IsProd=true entries influence framework inference and apt mapping.
	Pkgs map[string][]types.Package
}

// PythonPackageManager mirrors nixpacks's lockfile-strong-signal selection.
type PythonPackageManager string

const (
	PMPip        PythonPackageManager = "pip"
	PMPoetry     PythonPackageManager = "poetry"
	PMPDM        PythonPackageManager = "pdm"
	PMUv         PythonPackageManager = "uv"
	PMPipenv     PythonPackageManager = "pipenv"
	PMSetuptools PythonPackageManager = "setuptools"
	PMNone       PythonPackageManager = "none"
)

// Name implements Provider.
func (p *PythonProvider) Name() string { return "python" }

// Detect implements Provider. Triggers on any of: main.py / requirements.txt /
// pyproject.toml / Pipfile, matching nixpacks python.rs:36-49.
func (p *PythonProvider) Detect(app *App, _ *Env) (bool, error) {
	signals := []string{
		"main.py",
		"manage.py", // Django projects often skip pyproject.toml in favor of requirements.txt
		"requirements.txt",
		"pyproject.toml",
		"Pipfile",
		"setup.py",
	}
	for _, f := range signals {
		if app.IncludesFile(f) {
			return true, nil
		}
	}
	return false, nil
}

// SelectPackageManager implements the nixpacks python.rs:81-99 priority chain.
// Lockfile presence is a stronger signal than manifest mention.
func (p *PythonProvider) SelectPackageManager(app *App, env *Env) PythonPackageManager {
	if v, ok := env.GetConfigVariable("PYTHON_PACKAGE_MANAGER"); ok && v != "" {
		return PythonPackageManager(v)
	}
	switch {
	case app.IncludesFile("uv.lock"):
		return PMUv
	case app.IncludesFile("poetry.lock"):
		return PMPoetry
	case app.IncludesFile("pdm.lock"):
		return PMPDM
	case app.IncludesFile("Pipfile.lock"), app.IncludesFile("Pipfile"):
		return PMPipenv
	case app.IncludesFile("requirements.txt"):
		return PMPip
	case app.IncludesFile("pyproject.toml"):
		return PMSetuptools
	}
	return PMNone
}

// Plan implements Provider. Returns nil when Detect would have been false.
func (p *PythonProvider) Plan(app *App, env *Env) (*FrameworkPlan, error) {
	ok, _ := p.Detect(app, env)
	if !ok {
		return nil, nil
	}

	plan := &FrameworkPlan{
		Runtime:    RuntimeHint{Kind: "python"},
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
func (p *PythonProvider) Metadata(app *App, env *Env) (Metadata, error) {
	return Metadata{
		"package_manager": string(p.SelectPackageManager(app, env)),
	}, nil
}

// frameworkRule represents a single multi-source heuristic. score is the sum
// of triggered evidence weights; minScore gates emission.
type frameworkRule struct {
	name     string
	probes   []frameworkProbe
	minScore float64
}

type frameworkProbe struct {
	weight float64
	marker string
	check  func(p *PythonProvider, app *App) bool
}

// pythonFrameworkRules port the multi-source pattern from
// docs/investigations/nixpacks-provider-pattern.md §6 — at least two pieces
// of evidence reach the threshold for surfaced frameworks.
var pythonFrameworkRules = []frameworkRule{
	{
		name:     "FastAPI",
		minScore: 0.5,
		probes: []frameworkProbe{
			{weight: 0.5, marker: "uses_dep:fastapi", check: hasPyDep("fastapi")},
			{weight: 0.4, marker: "import_pattern:from fastapi import", check: importsPattern(`from\s+fastapi\b`)},
			{weight: 0.1, marker: "uses_dep:uvicorn", check: hasPyDep("uvicorn")},
		},
	},
	{
		name:     "Django",
		minScore: 0.6,
		probes: []frameworkProbe{
			{weight: 0.5, marker: "uses_dep:django", check: hasPyDep("django")},
			{weight: 0.4, marker: "manage.py present", check: hasFile("manage.py")},
			{weight: 0.2, marker: "import_pattern:from django", check: importsPattern(`from\s+django\b`)},
		},
	},
	{
		name:     "Flask",
		minScore: 0.5,
		probes: []frameworkProbe{
			{weight: 0.5, marker: "uses_dep:flask", check: hasPyDep("flask")},
			{weight: 0.4, marker: "import_pattern:from flask import", check: importsPattern(`from\s+flask\b`)},
		},
	},
	{
		name:     "SQLAlchemy",
		minScore: 0.5,
		probes: []frameworkProbe{
			{weight: 0.5, marker: "uses_dep:sqlalchemy", check: hasPyDep("sqlalchemy")},
			{weight: 0.4, marker: "import_pattern:from sqlalchemy import", check: importsPattern(`from\s+sqlalchemy\b`)},
		},
	},
	{
		name:     "Alembic",
		minScore: 0.5,
		probes: []frameworkProbe{
			{weight: 0.5, marker: "uses_dep:alembic", check: hasPyDep("alembic")},
			{weight: 0.4, marker: "alembic.ini present", check: hasFile("alembic.ini")},
		},
	},
	{
		name:     "Pandas",
		minScore: 0.5,
		probes: []frameworkProbe{
			{weight: 0.5, marker: "uses_dep:pandas", check: hasPyDep("pandas")},
			{weight: 0.4, marker: "import_pattern:import pandas", check: importsPattern(`import\s+pandas\b`)},
		},
	},
}

func (p *PythonProvider) detectFrameworks(app *App) []types.DetectedFramework {
	var out []types.DetectedFramework
	for _, rule := range pythonFrameworkRules {
		score := 0.0
		var evidence []string
		for _, probe := range rule.probes {
			if probe.check(p, app) {
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

func (p *PythonProvider) detectExternalServices(app *App) []types.DetectedExternalService {
	var out []types.DetectedExternalService

	if HasDep(p.Pkgs, "pip", "psycopg2") || HasDep(p.Pkgs, "pip", "psycopg2-binary") ||
		HasDep(p.Pkgs, "pip", "psycopg") || HasDep(p.Pkgs, "pip", "asyncpg") ||
		app.FindMatch(regexp.MustCompile(`django\.db\.backends\.postgresql`), "**/*.py") {
		out = append(out, types.DetectedExternalService{
			Service:    "PostgreSQL",
			Confidence: 0.95,
			Evidence:   []string{"uses_dep:psycopg/asyncpg or postgres backend import"},
		})
	}
	if HasDep(p.Pkgs, "pip", "mysqlclient") || HasDep(p.Pkgs, "pip", "pymysql") {
		out = append(out, types.DetectedExternalService{
			Service:    "MySQL",
			Confidence: 0.9,
			Evidence:   []string{"uses_dep:mysqlclient/pymysql"},
		})
	}
	if HasDep(p.Pkgs, "pip", "redis") {
		out = append(out, types.DetectedExternalService{
			Service:    "Redis",
			Confidence: 0.85,
			Evidence:   []string{"uses_dep:redis"},
		})
	}
	if HasDep(p.Pkgs, "pip", "anthropic") {
		out = append(out, types.DetectedExternalService{
			Service:    "Anthropic API",
			Confidence: 0.99,
			Evidence:   []string{"uses_dep:anthropic"},
		})
	}
	if HasDep(p.Pkgs, "pip", "openai") {
		out = append(out, types.DetectedExternalService{
			Service:    "OpenAI API",
			Confidence: 0.99,
			Evidence:   []string{"uses_dep:openai"},
		})
	}
	return out
}

// hasPyDep curries HasDep("pip", name) into a probe func.
func hasPyDep(name string) func(p *PythonProvider, app *App) bool {
	return func(p *PythonProvider, _ *App) bool {
		return HasDep(p.Pkgs, "pip", name)
	}
}

func hasFile(rel string) func(p *PythonProvider, app *App) bool {
	return func(_ *PythonProvider, app *App) bool {
		return app.IncludesFile(rel)
	}
}

func importsPattern(pattern string) func(p *PythonProvider, app *App) bool {
	re := regexp.MustCompile(pattern)
	return func(_ *PythonProvider, app *App) bool {
		return app.FindMatch(re, "**/*.py")
	}
}
