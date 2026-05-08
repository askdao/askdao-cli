package scanner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/askdao/askdao-cli/internal/types"
)

// ApplyDevFilter mutates pkgs in place: for each ecosystem it reads the
// canonical manifest at root and flips Package.IsProd=false when the package
// name appears in any dev/test/bench scope. Packages absent from the manifest
// (transitive deps, unrelated extras) keep their syft-default IsProd=true.
//
// Supported manifests:
//
//	pip   → pyproject.toml (uv [dependency-groups], poetry [tool.poetry.group.*],
//	        PEP 621 [project.optional-dependencies])
//	npm   → package.json (devDependencies, optionalDependencies)
//	cargo → Cargo.toml ([dev-dependencies], [build-dependencies])
//
// Missing manifests are tolerated: the scan returns nil error and leaves the
// corresponding ecosystem untouched.
func ApplyDevFilter(root string, pkgs map[string][]types.Package) error {
	if root == "" {
		return errors.New("scanner: root must be non-empty")
	}
	if pkgs == nil {
		return nil
	}

	devNames := map[string]map[string]bool{}

	if names, err := readPyDevDeps(filepath.Join(root, "pyproject.toml")); err != nil {
		return err
	} else if len(names) > 0 {
		devNames["pip"] = names
	}
	if names, err := readNpmDevDeps(filepath.Join(root, "package.json")); err != nil {
		return err
	} else if len(names) > 0 {
		devNames["npm"] = names
	}
	if names, err := readCargoDevDeps(filepath.Join(root, "Cargo.toml")); err != nil {
		return err
	} else if len(names) > 0 {
		devNames["cargo"] = names
	}

	for ecosystem, list := range pkgs {
		dev := devNames[ecosystem]
		if len(dev) == 0 {
			continue
		}
		for i := range list {
			if dev[normalizeDepName(ecosystem, list[i].Name)] {
				list[i].IsProd = false
			}
		}
		pkgs[ecosystem] = list
	}
	return nil
}

// normalizeDepName canonicalizes a dep name for cross-manifest matching:
// PEP 503 says pip deps are case-insensitive and `_`/`.` are equivalent to `-`.
// npm preserves case but lowercase compare is safe in practice. cargo is also
// case-insensitive in dep declarations.
func normalizeDepName(ecosystem, name string) string {
	low := strings.ToLower(name)
	if ecosystem == "pip" {
		low = strings.ReplaceAll(low, "_", "-")
		low = strings.ReplaceAll(low, ".", "-")
	}
	return low
}

// pyprojectFile maps the subset of pyproject.toml we read.
type pyprojectFile struct {
	Project struct {
		OptionalDependencies map[string][]string `toml:"optional-dependencies"`
	} `toml:"project"`
	DependencyGroups map[string][]string `toml:"dependency-groups"`
	Tool             struct {
		Poetry struct {
			Group map[string]struct {
				Dependencies map[string]toml.Primitive `toml:"dependencies"`
			} `toml:"group"`
			DevDependencies map[string]toml.Primitive `toml:"dev-dependencies"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

// readPyDevDeps returns the union of all dev-style scopes in pyproject.toml,
// covering the three flavors askdao-cli encounters in practice (uv, poetry,
// PEP 621). Names are normalized for PEP 503 matching.
func readPyDevDeps(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var pp pyprojectFile
	if _, err := toml.Decode(string(data), &pp); err != nil {
		return nil, err
	}

	out := map[string]bool{}
	devScopeNames := []string{"dev", "test", "tests", "lint", "type", "typing", "docs", "bench"}

	for _, scope := range devScopeNames {
		for _, dep := range pp.DependencyGroups[scope] {
			out[normalizeDepName("pip", parsePEP508Name(dep))] = true
		}
		for _, dep := range pp.Project.OptionalDependencies[scope] {
			out[normalizeDepName("pip", parsePEP508Name(dep))] = true
		}
		if g, ok := pp.Tool.Poetry.Group[scope]; ok {
			for name := range g.Dependencies {
				out[normalizeDepName("pip", name)] = true
			}
		}
	}
	for name := range pp.Tool.Poetry.DevDependencies {
		out[normalizeDepName("pip", name)] = true
	}
	return out, nil
}

// parsePEP508Name strips a PEP 508 dep string ("requests[security]>=2,<3 ; python_version<'3.10'")
// down to its package name.
func parsePEP508Name(spec string) string {
	spec = strings.TrimSpace(spec)
	for _, sep := range []string{";", " "} {
		if i := strings.Index(spec, sep); i >= 0 {
			spec = spec[:i]
		}
	}
	for i, r := range spec {
		switch r {
		case '[', '<', '>', '=', '!', '~', '(':
			return strings.TrimSpace(spec[:i])
		}
	}
	return spec
}

type packageJSON struct {
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

func readNpmDevDeps(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pj packageJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for k := range pj.DevDependencies {
		out[normalizeDepName("npm", k)] = true
	}
	for k := range pj.OptionalDependencies {
		out[normalizeDepName("npm", k)] = true
	}
	return out, nil
}

type cargoToml struct {
	DevDependencies   map[string]toml.Primitive `toml:"dev-dependencies"`
	BuildDependencies map[string]toml.Primitive `toml:"build-dependencies"`
}

func readCargoDevDeps(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ct cargoToml
	if _, err := toml.Decode(string(data), &ct); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for k := range ct.DevDependencies {
		out[normalizeDepName("cargo", k)] = true
	}
	for k := range ct.BuildDependencies {
		out[normalizeDepName("cargo", k)] = true
	}
	return out, nil
}
