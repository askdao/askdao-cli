// [INPUT]: 依赖 testing / os / path·filepath / sort / strings；internal/types
// [OUTPUT]: TestResolveSkillDir（四分支路径解析）+ TestPackageSkills_FrontmatterValidation（SKILL.md frontmatter 校验）
// [POS]: internal/deployflow skill 打包层测试 —— 提取自 cmd/askdao/skill_package_test.go（随 PackageSkills/resolveSkillDir 移入本包，CLI+桌面单源守护）；runDeploy 的 e2e 回归仍在 cmd/askdao
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package deployflow

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

// TestResolveSkillDir locks the four path-resolution branches shared by the CLI
// (`agent deploy`) and the studio (one-stop deploy). The studio writes global
// (user-scope) skills into the yaml with an ABSOLUTE path (webstudio api.go
// derives it from filepath.Dir of an absolute Source), so the absolute +
// Scope=="user" branches are the ones that actually fire in production.
func TestResolveSkillDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	const dir = "/proj"
	abs := filepath.Join(string(filepath.Separator), "abs", "skills", "foo")

	cases := []struct {
		name  string
		skill types.Skill
		want  string
	}{
		{
			name:  "tilde expands to home regardless of scope",
			skill: types.Skill{Path: "~/.claude/skills/foo"},
			want:  filepath.Join(home, ".claude", "skills", "foo"),
		},
		{
			name:  "absolute path returned verbatim (the studio global-skill case)",
			skill: types.Skill{Path: abs, Scope: "user"},
			want:  abs,
		},
		{
			name:  "relative path with user scope kept as-is (documented CWD-relative)",
			skill: types.Skill{Path: "bar", Scope: "user"},
			want:  filepath.FromSlash("bar"),
		},
		{
			name:  "project-relative path joins dir",
			skill: types.Skill{Path: ".agents/skills/foo", Scope: "project"},
			want:  filepath.Join(dir, ".agents", "skills", "foo"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSkillDir(dir, tc.skill)
			if err != nil {
				t.Fatalf("resolveSkillDir: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveSkillDir(%q, %+v) = %q, want %q", dir, tc.skill, got, tc.want)
			}
		})
	}
}

// TestPackageSkills_FrontmatterValidation locks the deploy-time SKILL.md
// frontmatter gate: name + description are required (description is the trigger
// instruction the model matches against — a skill without it deploys fine but
// never activates), and frontmatter names must be unique across the packaged
// set. Validation fires in PackageSkills so both `agent deploy` and the studio's
// one-stop deploy reject the same inputs.
func TestPackageSkills_FrontmatterValidation(t *testing.T) {
	writeSkill := func(t *testing.T, root, dirName, skillMD string) string {
		t.Helper()
		d := filepath.Join(root, "skills", dirName)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
			t.Fatal(err)
		}
		return filepath.ToSlash(filepath.Join("skills", dirName))
	}
	specFor := func(paths ...string) *types.AgentSpec {
		s := &types.AgentSpec{}
		for _, p := range paths {
			s.Skills = append(s.Skills, types.Skill{Type: "custom_local", Path: p})
		}
		return s
	}

	t.Run("missing frontmatter rejected", func(t *testing.T) {
		root := t.TempDir()
		p := writeSkill(t, root, "bare", "Just a body, no frontmatter.\n")
		_, err := PackageSkills(root, specFor(p))
		if err == nil || !strings.Contains(err.Error(), "must declare 'name'") {
			t.Errorf("want missing-name error, got %v", err)
		}
	})

	t.Run("missing description rejected", func(t *testing.T) {
		root := t.TempDir()
		p := writeSkill(t, root, "no-desc", "---\nname: no-desc\n---\nbody\n")
		_, err := PackageSkills(root, specFor(p))
		if err == nil || !strings.Contains(err.Error(), "must declare 'description'") {
			t.Errorf("want missing-description error, got %v", err)
		}
	})

	t.Run("frontmatter name collision rejected", func(t *testing.T) {
		root := t.TempDir()
		md := "---\nname: same-name\ndescription: a skill\n---\nbody\n"
		p1 := writeSkill(t, root, "skill-a", md)
		p2 := writeSkill(t, root, "skill-b", md)
		_, err := PackageSkills(root, specFor(p1, p2))
		if err == nil || !strings.Contains(err.Error(), "name collision") {
			t.Errorf("want frontmatter-name collision error, got %v", err)
		}
	})

	t.Run("complete frontmatter packages fine", func(t *testing.T) {
		root := t.TempDir()
		p := writeSkill(t, root, "good", "---\nname: good\ndescription: does a good thing when asked\n---\nbody\n")
		zips, err := PackageSkills(root, specFor(p))
		if err != nil {
			t.Fatalf("PackageSkills: %v", err)
		}
		if _, ok := zips["good"]; !ok {
			t.Errorf("expected zip for 'good', got keys %v", sortedKeys(zips))
		}
	})
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
