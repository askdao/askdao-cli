package providers

import (
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/askdao/askdao-cli/internal/types"
)

// RustProvider ports detect / framework-inference of nixpacks rust.rs, plus
// the askdao-cli sys-crate → apt reverse map (a thin Rust-flavored equivalent
// of the Python/Node tables in apt_map.go).
type RustProvider struct {
	// Pkgs is the dev-filtered package map produced by internal/scanner. Used
	// for sys-crate apt mapping; nil disables the cargoAptMap pass.
	Pkgs map[string][]types.Package
}

// Name implements Provider.
func (p *RustProvider) Name() string { return "rust" }

// Detect implements Provider. Triggers on Cargo.toml at the project root.
func (p *RustProvider) Detect(app *App, _ *Env) (bool, error) {
	return app.IncludesFile("Cargo.toml"), nil
}

// cargoToml is the minimal subset we read from Cargo.toml — package metadata
// and workspace declaration are enough for askdao-cli's needs.
type cargoTomlFile struct {
	Package struct {
		Name        string `toml:"name"`
		Edition     string `toml:"edition"`
		RustVersion string `toml:"rust-version"`
	} `toml:"package"`
	Workspace *struct {
		Members []string `toml:"members"`
	} `toml:"workspace"`
}

// rustToolchainFile maps rust-toolchain.toml — the canonical pin source.
type rustToolchainFile struct {
	Toolchain struct {
		Channel string `toml:"channel"`
	} `toml:"toolchain"`
}

// Plan implements Provider.
func (p *RustProvider) Plan(app *App, env *Env) (*FrameworkPlan, error) {
	ok, _ := p.Detect(app, env)
	if !ok {
		return nil, nil
	}

	plan := &FrameworkPlan{
		Runtime:    RuntimeHint{Kind: "rust"},
		SystemPkgs: map[string][]types.InferredAptPackage{},
	}

	// Runtime version: rust-toolchain.toml > rust-toolchain > Cargo.toml rust-version.
	if v := readRustToolchainPin(app); v != "" {
		plan.Runtime.Version = v
		plan.Evidence = append(plan.Evidence, Evidence{Marker: "rust-toolchain pin: " + v})
	} else if data, err := app.ReadFile("Cargo.toml"); err == nil {
		var ct cargoTomlFile
		if _, err := toml.Decode(string(data), &ct); err == nil && ct.Package.RustVersion != "" {
			plan.Runtime.Version = ct.Package.RustVersion
			plan.Evidence = append(plan.Evidence, Evidence{Marker: "Cargo.toml rust-version: " + ct.Package.RustVersion})
		}
	}

	// Workspace marker → useful for the recommender's package-manager metadata.
	if data, err := app.ReadFile("Cargo.toml"); err == nil {
		var ct cargoTomlFile
		if _, err := toml.Decode(string(data), &ct); err == nil && ct.Workspace != nil {
			plan.Evidence = append(plan.Evidence, Evidence{Marker: "cargo workspace: " + strings.Join(ct.Workspace.Members, ",")})
		}
	}

	// Sys-crate → apt reverse map. Mirrors the python/npm pattern: the table is
	// the Rust-specific addition kept here rather than apt_map.go to keep
	// language tables next to their providers.
	plan.SystemPkgs["apt"] = inferRustAptPackages(p.Pkgs)
	plan.ExternalSvc = p.detectExternalServices()
	return plan, nil
}

// Metadata implements Provider.
func (p *RustProvider) Metadata(app *App, _ *Env) (Metadata, error) {
	md := Metadata{"package_manager": "cargo"}
	if app.IncludesFile("rust-toolchain.toml") || app.IncludesFile("rust-toolchain") {
		md["toolchain_pinned"] = "true"
	}
	return md, nil
}

// readRustToolchainPin returns the channel string from rust-toolchain.toml or
// rust-toolchain (single-line legacy form). Empty string when neither exists.
func readRustToolchainPin(app *App) string {
	if data, err := app.ReadFile("rust-toolchain.toml"); err == nil {
		var rt rustToolchainFile
		if _, err := toml.Decode(string(data), &rt); err == nil && rt.Toolchain.Channel != "" {
			return rt.Toolchain.Channel
		}
	}
	if data, err := app.ReadFile("rust-toolchain"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				return line
			}
		}
	}
	return ""
}

// cargoAptMap captures the most common "*-sys" crates → apt headers. Each entry
// triggers when ANY package whose name matches the key (PEP-style normalize:
// lowercase, no version) is present in `Pkgs["cargo"]` with IsProd=true.
var cargoAptMap = map[string]aptEntry{
	"openssl-sys":     {pkgs: []string{"libssl-dev", "pkg-config"}, reason: "openssl-sys binds to system libssl"},
	"pq-sys":          {pkgs: []string{"libpq-dev"}, reason: "pq-sys / diesel postgres backend needs libpq"},
	"mysqlclient-sys": {pkgs: []string{"libmysqlclient-dev"}, reason: "mysqlclient-sys binds to system libmysqlclient"},
	"libsqlite3-sys":  {pkgs: []string{"libsqlite3-dev"}, reason: "libsqlite3-sys binds to system sqlite3"},
	"zstd-sys":        {pkgs: []string{"libzstd-dev"}, reason: "zstd-sys binds to system libzstd"},
	"libxml":          {pkgs: []string{"libxml2-dev"}, reason: "libxml binds to system libxml2"},
	"alsa-sys":        {pkgs: []string{"libasound2-dev"}, reason: "alsa-sys binds to system ALSA"},
}

func inferRustAptPackages(pkgs map[string][]types.Package) []types.InferredAptPackage {
	if len(pkgs) == 0 {
		return nil
	}
	seen := map[string]string{}
	for _, p := range pkgs["cargo"] {
		if !p.IsProd {
			continue
		}
		key := normalizeDepName("cargo", p.Name)
		entry, ok := cargoAptMap[key]
		if !ok {
			continue
		}
		for _, apt := range entry.pkgs {
			if _, exists := seen[apt]; !exists {
				seen[apt] = entry.reason
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]types.InferredAptPackage, 0, len(seen))
	for apt, reason := range seen {
		out = append(out, types.InferredAptPackage{Name: apt, Reason: reason})
	}
	sortInferred(out)
	return out
}

// sortInferred is a tiny named helper to keep import surface stable; the
// alternative was importing sort just here.
func sortInferred(a []types.InferredAptPackage) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1].Name > a[j].Name; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func (p *RustProvider) detectExternalServices() []types.DetectedExternalService {
	var out []types.DetectedExternalService
	if HasDep(p.Pkgs, "cargo", "diesel") || HasDep(p.Pkgs, "cargo", "sqlx") || HasDep(p.Pkgs, "cargo", "tokio-postgres") {
		out = append(out, types.DetectedExternalService{
			Service: "PostgreSQL", Confidence: 0.85,
			Evidence: []string{"cargo dep: diesel/sqlx/tokio-postgres"},
		})
	}
	if HasDep(p.Pkgs, "cargo", "redis") {
		out = append(out, types.DetectedExternalService{
			Service: "Redis", Confidence: 0.85,
			Evidence: []string{"cargo dep: redis"},
		})
	}
	return out
}
