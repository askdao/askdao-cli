package scanner

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/askdao/askdao-cli/internal/types"
)

// DetectRuntimes parses pinned-version files at root and returns one entry per
// runtime found. Order is deterministic: python, node, go, rust — same as the
// design.md §4 example.
func DetectRuntimes(root string) ([]types.DetectedRuntime, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}
	var out []types.DetectedRuntime

	if rt := detectPython(root); rt != nil {
		out = append(out, *rt)
	}
	if rt := detectNode(root); rt != nil {
		out = append(out, *rt)
	}
	if rt := detectGo(root); rt != nil {
		out = append(out, *rt)
	}
	if rt := detectRust(root); rt != nil {
		out = append(out, *rt)
	}
	for _, rt := range detectToolVersions(root) {
		if !haveRuntime(out, rt.Kind) {
			out = append(out, rt)
		}
	}
	return out, nil
}

func haveRuntime(have []types.DetectedRuntime, kind string) bool {
	for _, r := range have {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

// detectPython prefers pyproject.toml's requires-python (richer constraint),
// falls back to .python-version (single pin).
func detectPython(root string) *types.DetectedRuntime {
	if path := filepath.Join(root, "pyproject.toml"); fileExists(path) {
		var pp struct {
			Project struct {
				RequiresPython string `toml:"requires-python"`
			} `toml:"project"`
		}
		if data, err := os.ReadFile(path); err == nil {
			if _, err := toml.Decode(string(data), &pp); err == nil && pp.Project.RequiresPython != "" {
				return &types.DetectedRuntime{
					Kind:       "python",
					Version:    extractFirstVersion(pp.Project.RequiresPython),
					Source:     "pyproject.toml",
					Constraint: pp.Project.RequiresPython,
				}
			}
		}
	}
	if v := readSingleLine(filepath.Join(root, ".python-version")); v != "" {
		return &types.DetectedRuntime{
			Kind:       "python",
			Version:    v,
			Source:     ".python-version",
			Constraint: v,
		}
	}
	return nil
}

func detectNode(root string) *types.DetectedRuntime {
	if v := readSingleLine(filepath.Join(root, ".nvmrc")); v != "" {
		v = strings.TrimPrefix(v, "v")
		return &types.DetectedRuntime{
			Kind:       "node",
			Version:    v,
			Source:     ".nvmrc",
			Constraint: v,
		}
	}
	return nil
}

var goModRe = regexp.MustCompile(`(?m)^go\s+(\d+(?:\.\d+){0,2})`)

func detectGo(root string) *types.DetectedRuntime {
	path := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	m := goModRe.FindStringSubmatch(string(data))
	if m == nil {
		return nil
	}
	return &types.DetectedRuntime{
		Kind:       "go",
		Version:    m[1],
		Source:     "go.mod",
		Constraint: m[1],
	}
}

func detectRust(root string) *types.DetectedRuntime {
	tomlPath := filepath.Join(root, "rust-toolchain.toml")
	if data, err := os.ReadFile(tomlPath); err == nil {
		var rt struct {
			Toolchain struct {
				Channel string `toml:"channel"`
			} `toml:"toolchain"`
		}
		if _, err := toml.Decode(string(data), &rt); err == nil && rt.Toolchain.Channel != "" {
			return &types.DetectedRuntime{
				Kind:       "rust",
				Version:    rt.Toolchain.Channel,
				Source:     "rust-toolchain.toml",
				Constraint: rt.Toolchain.Channel,
			}
		}
	}
	if v := readSingleLine(filepath.Join(root, "rust-toolchain")); v != "" {
		return &types.DetectedRuntime{
			Kind:       "rust",
			Version:    v,
			Source:     "rust-toolchain",
			Constraint: v,
		}
	}
	return nil
}

// detectToolVersions parses asdf-style .tool-versions files. Each line is
// `<plugin> <version>` and serves as a fallback when no language-native pin
// file was found.
func detectToolVersions(root string) []types.DetectedRuntime {
	path := filepath.Join(root, ".tool-versions")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []types.DetectedRuntime
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		kind := normalizeToolName(parts[0])
		if kind == "" {
			continue
		}
		out = append(out, types.DetectedRuntime{
			Kind:       kind,
			Version:    parts[1],
			Source:     ".tool-versions",
			Constraint: parts[1],
		})
	}
	return out
}

func normalizeToolName(plugin string) string {
	switch strings.ToLower(plugin) {
	case "python":
		return "python"
	case "nodejs", "node":
		return "node"
	case "golang", "go":
		return "go"
	case "rust":
		return "rust"
	}
	return ""
}

func readSingleLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var firstVersionRe = regexp.MustCompile(`(\d+(?:\.\d+){0,2})`)

func extractFirstVersion(constraint string) string {
	m := firstVersionRe.FindString(constraint)
	return m
}
