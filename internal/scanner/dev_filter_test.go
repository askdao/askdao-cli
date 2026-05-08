package scanner

import (
	"path/filepath"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestApplyDevFilter_PyprojectUv(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "pyproject.toml"), `
[project]
name = "demo"
dependencies = ["fastapi", "sqlalchemy"]

[dependency-groups]
dev = ["pytest>=9", "mypy", "ruff"]
docs = ["mkdocs"]
`)

	pkgs := map[string][]types.Package{
		"pip": {
			{Name: "fastapi", Version: "0.135.1", IsProd: true},
			{Name: "sqlalchemy", Version: "2.0.48", IsProd: true},
			{Name: "pytest", Version: "9.0.2", IsProd: true},
			{Name: "mypy", Version: "1.19.1", IsProd: true},
			{Name: "ruff", Version: "0.15.6", IsProd: true},
			{Name: "MkDocs", Version: "1.5", IsProd: true}, // case-insensitive
			{Name: "transitive-only", Version: "0.1", IsProd: true},
		},
	}
	if err := ApplyDevFilter(root, pkgs); err != nil {
		t.Fatalf("ApplyDevFilter: %v", err)
	}
	wantProd := map[string]bool{
		"fastapi":         true,
		"sqlalchemy":      true,
		"transitive-only": true, // not declared anywhere → keep IsProd=true
	}
	for _, p := range pkgs["pip"] {
		got := p.IsProd
		want := wantProd[normalizeDepName("pip", p.Name)]
		if got != want {
			t.Errorf("pkg %s IsProd=%v, want %v", p.Name, got, want)
		}
	}
}

func TestApplyDevFilter_PyprojectPoetry(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "pyproject.toml"), `
[tool.poetry]
name = "demo"

[tool.poetry.dependencies]
python = "^3.12"
fastapi = "*"

[tool.poetry.group.dev.dependencies]
pytest = "*"
black = "*"
`)
	pkgs := map[string][]types.Package{
		"pip": {
			{Name: "fastapi", IsProd: true},
			{Name: "pytest", IsProd: true},
			{Name: "black", IsProd: true},
		},
	}
	if err := ApplyDevFilter(root, pkgs); err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs["pip"] {
		want := p.Name == "fastapi"
		if p.IsProd != want {
			t.Errorf("%s IsProd=%v, want %v", p.Name, p.IsProd, want)
		}
	}
}

func TestApplyDevFilter_PackageJSON(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), `{
  "name": "demo",
  "dependencies": {"react": "^18", "express": "^4"},
  "devDependencies": {"jest": "^29", "eslint": "^8"}
}`)
	pkgs := map[string][]types.Package{
		"npm": {
			{Name: "react", IsProd: true},
			{Name: "express", IsProd: true},
			{Name: "jest", IsProd: true},
			{Name: "eslint", IsProd: true},
		},
	}
	if err := ApplyDevFilter(root, pkgs); err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs["npm"] {
		want := p.Name == "react" || p.Name == "express"
		if p.IsProd != want {
			t.Errorf("%s IsProd=%v, want %v", p.Name, p.IsProd, want)
		}
	}
}

func TestApplyDevFilter_CargoToml(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Cargo.toml"), `
[package]
name = "demo"

[dependencies]
serde = "1"

[dev-dependencies]
criterion = "0.5"

[build-dependencies]
cc = "1"
`)
	pkgs := map[string][]types.Package{
		"cargo": {
			{Name: "serde", IsProd: true},
			{Name: "criterion", IsProd: true},
			{Name: "cc", IsProd: true},
		},
	}
	if err := ApplyDevFilter(root, pkgs); err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs["cargo"] {
		want := p.Name == "serde"
		if p.IsProd != want {
			t.Errorf("%s IsProd=%v, want %v", p.Name, p.IsProd, want)
		}
	}
}

func TestApplyDevFilter_NoManifests(t *testing.T) {
	root := t.TempDir()
	pkgs := map[string][]types.Package{
		"pip": {{Name: "fastapi", IsProd: true}},
	}
	if err := ApplyDevFilter(root, pkgs); err != nil {
		t.Fatal(err)
	}
	if !pkgs["pip"][0].IsProd {
		t.Error("missing manifest should leave IsProd untouched")
	}
}

func TestParsePEP508Name(t *testing.T) {
	cases := map[string]string{
		"requests":           "requests",
		"requests>=2.31":     "requests",
		"requests[security]": "requests",
		"requests[security]>=2,<3 ; python_version<'3.10'": "requests",
		"  black==24.10.0   ":                              "black",
		"some-pkg ~= 1.2":                                  "some-pkg",
	}
	for in, want := range cases {
		if got := parsePEP508Name(in); got != want {
			t.Errorf("parsePEP508Name(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyDevFilter_NilMap(t *testing.T) {
	if err := ApplyDevFilter(t.TempDir(), nil); err != nil {
		t.Fatalf("nil map should be a no-op, got %v", err)
	}
}

func TestApplyDevFilter_RootRequired(t *testing.T) {
	if err := ApplyDevFilter("", map[string][]types.Package{}); err == nil {
		t.Fatal("expected error for empty root")
	}
}
