package providers

import (
	"path/filepath"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestRust_DetectAndRuntime_FromToolchainToml(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Cargo.toml"), `[package]
name = "demo"
version = "0.1.0"
`)
	mustWriteFile(t, filepath.Join(root, "rust-toolchain.toml"), `[toolchain]
channel = "1.83.0"
`)

	app, _ := NewApp(root)
	rp := &RustProvider{}
	ok, err := rp.Detect(app, NewEnv(nil))
	if err != nil || !ok {
		t.Fatalf("Detect = (%v, %v)", ok, err)
	}
	plan, err := rp.Plan(app, NewEnv(nil))
	if err != nil || plan == nil {
		t.Fatalf("Plan = (%v, %v)", plan, err)
	}
	if plan.Runtime.Kind != "rust" || plan.Runtime.Version != "1.83.0" {
		t.Errorf("Runtime = %+v, want {rust 1.83.0}", plan.Runtime)
	}
}

func TestRust_RuntimeFallbackChain(t *testing.T) {
	t.Run("legacy rust-toolchain single-line", func(t *testing.T) {
		root := t.TempDir()
		mustWriteFile(t, filepath.Join(root, "Cargo.toml"), `[package]
name = "x"
`)
		mustWriteFile(t, filepath.Join(root, "rust-toolchain"), "stable\n")
		app, _ := NewApp(root)
		plan, _ := (&RustProvider{}).Plan(app, NewEnv(nil))
		if plan.Runtime.Version != "stable" {
			t.Errorf("Version = %q, want stable", plan.Runtime.Version)
		}
	})
	t.Run("Cargo.toml rust-version fallback", func(t *testing.T) {
		root := t.TempDir()
		mustWriteFile(t, filepath.Join(root, "Cargo.toml"), `[package]
name = "x"
rust-version = "1.78"
`)
		app, _ := NewApp(root)
		plan, _ := (&RustProvider{}).Plan(app, NewEnv(nil))
		if plan.Runtime.Version != "1.78" {
			t.Errorf("Version = %q, want 1.78", plan.Runtime.Version)
		}
	})
}

func TestRust_SysCrateAptMap(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Cargo.toml"), `[package]
name = "x"
`)
	pkgs := map[string][]types.Package{
		"cargo": {
			{Name: "openssl-sys", IsProd: true},
			{Name: "pq-sys", IsProd: true},
			{Name: "criterion", IsProd: false}, // dev — must not contribute
		},
	}
	app, _ := NewApp(root)
	plan, _ := (&RustProvider{Pkgs: pkgs}).Plan(app, NewEnv(nil))
	apt := plan.SystemPkgs["apt"]
	got := map[string]bool{}
	for _, p := range apt {
		got[p.Name] = true
	}
	for _, want := range []string{"libssl-dev", "pkg-config", "libpq-dev"} {
		if !got[want] {
			t.Errorf("missing %s in apt list: %+v", want, apt)
		}
	}
}

func TestRust_Workspace(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Cargo.toml"), `[workspace]
members = ["crates/a", "crates/b"]
`)
	app, _ := NewApp(root)
	plan, _ := (&RustProvider{}).Plan(app, NewEnv(nil))

	hasWorkspaceEvidence := false
	for _, e := range plan.Evidence {
		if len(e.Marker) >= len("cargo workspace") && e.Marker[:len("cargo workspace")] == "cargo workspace" {
			hasWorkspaceEvidence = true
		}
	}
	if !hasWorkspaceEvidence {
		t.Errorf("expected workspace evidence, got %+v", plan.Evidence)
	}
}

func TestRust_DetectFalseForGoOnly(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module x\n")
	app, _ := NewApp(root)
	ok, _ := (&RustProvider{}).Detect(app, NewEnv(nil))
	if ok {
		t.Error("go-only project should not detect as rust")
	}
}

func TestRust_InterfaceConformance(t *testing.T) {
	var _ Provider = (*GoProvider)(nil)
	var _ Provider = (*RustProvider)(nil)
}
