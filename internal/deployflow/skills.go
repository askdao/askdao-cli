// [INPUT]: 依赖 fmt/os/path·filepath/strings；internal/scanner 的 ParseSkillFrontmatter、internal/types 的 AgentSpec/Skill、internal/deploy 的 ZipDir
// [OUTPUT]: 对外提供 PackageSkills；包内 resolveSkillDir
// [POS]: internal/deployflow 部署编排层 —— skill 打包（枚举 custom_local → frontmatter 前置校验 → deploy.ZipDir）。
//
//	CLI（cmd/askdao agent deploy）与桌面（cmd/askdao-studio）单源共用，杜绝双写漂移。
//	放此包而非 internal/deploy：deploy 纪律是 stdlib-only 的 HTTP+zip 域、不 import types/scanner；
//	skill 打包是编排（读 spec + 校验 frontmatter + 调 deploy.ZipDir），依赖 types/scanner，归编排层。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package deployflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/scanner"
	"github.com/askdao/askdao-cli/internal/types"
)

// PackageSkills zips every custom_local skill referenced by spec into a
// name→zip map, applying the harness-neutral invariant (zip top dir =
// filepath.Base). Shared by `agent deploy` (CLI) and the desktop studio's
// OnDeploy. Returns a descriptive error on a missing dir / SKILL.md / name
// collision / incomplete SKILL.md frontmatter (name + description are how the
// model decides to activate a skill — a skill missing them deploys fine but
// never triggers, so we fail fast here instead).
func PackageSkills(dir string, spec *types.AgentSpec) (map[string][]byte, error) {
	skillZips := map[string][]byte{}
	fmNames := map[string]string{} // frontmatter name → yaml path (collision detection)
	for _, s := range spec.Skills {
		if s.Type != "custom_local" {
			continue
		}
		if s.Path == "" {
			return nil, fmt.Errorf("a custom_local skill entry is missing 'path'")
		}
		skillName := filepath.Base(filepath.Clean(s.Path))
		if skillName == "" || skillName == "." || skillName == "/" {
			return nil, fmt.Errorf("custom_local skill path %q: cannot resolve a skill name", s.Path)
		}
		skillDir, err := resolveSkillDir(dir, s)
		if err != nil {
			return nil, err
		}
		if fi, serr := os.Stat(skillDir); serr != nil || !fi.IsDir() {
			return nil, fmt.Errorf("custom_local skill %q: directory not found at %s", s.Path, skillDir)
		}
		skillMD := filepath.Join(skillDir, "SKILL.md")
		if _, serr := os.Stat(skillMD); serr != nil {
			return nil, fmt.Errorf("custom_local skill %q: %s has no SKILL.md", s.Path, skillDir)
		}
		fmName, fmDesc := scanner.ParseSkillFrontmatter(skillMD)
		if fmName == "" {
			return nil, fmt.Errorf("custom_local skill %q: SKILL.md frontmatter must declare 'name' (add a leading `---` block with name + description)", s.Path)
		}
		if fmDesc == "" {
			return nil, fmt.Errorf("custom_local skill %q: SKILL.md frontmatter must declare 'description' — it is the trigger instruction the model matches against; without it the skill never activates", s.Path)
		}
		if prev, dup := fmNames[fmName]; dup {
			return nil, fmt.Errorf("skill frontmatter name collision %q (declared by both %s and %s)", fmName, prev, s.Path)
		}
		fmNames[fmName] = s.Path
		zb, zerr := deploy.ZipDir(skillDir, skillName)
		if zerr != nil {
			return nil, fmt.Errorf("packaging skill %q: %w", s.Path, zerr)
		}
		if _, dup := skillZips[skillName]; dup {
			return nil, fmt.Errorf("skill name collision %q (two paths share a basename)", skillName)
		}
		skillZips[skillName] = zb
	}
	return skillZips, nil
}

// resolveSkillDir resolves a custom_local skill's on-disk directory. Project
// scope (relative path) joins dir; user scope (Scope=="user", an absolute path,
// or a ~-prefixed path) resolves against the home dir / filesystem so global
// skills picked in the studio package correctly.
func resolveSkillDir(dir string, s types.Skill) (string, error) {
	path := s.Path
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, filepath.FromSlash(path[2:])), nil
	}
	if filepath.IsAbs(path) || s.Scope == "user" {
		return filepath.FromSlash(path), nil
	}
	return filepath.Join(dir, filepath.FromSlash(path)), nil
}
