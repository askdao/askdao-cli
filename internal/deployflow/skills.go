// [INPUT]: 依赖 fmt/os/path·filepath/strings；internal/scanner 的 ParseSkillFrontmatter、internal/types 的 AgentSpec/Skill、internal/deploy 的 ZipDir
// [OUTPUT]: 对外提供 PackageSkills / ResolveSkillDir（custom_local skill path→磁盘目录四分支解析）/ UpsertSkillFrontmatter（补全 SKILL.md frontmatter name/description，行级 upsert 保留 body）；包内 quoteYAMLValue
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
		skillDir, err := ResolveSkillDir(dir, s)
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

// ResolveSkillDir resolves a custom_local skill's on-disk directory. Project
// scope (relative path) joins dir; user scope (Scope=="user", an absolute path,
// or a ~-prefixed path) resolves against the home dir / filesystem so global
// skills picked in the studio package correctly. Exported so the studio's
// skill-validate / skill-fix actions locate the same SKILL.md the deploy gate reads.
func ResolveSkillDir(dir string, s types.Skill) (string, error) {
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

// UpsertSkillFrontmatter sets name and/or description in a SKILL.md's leading
// `--- ... ---` YAML frontmatter, creating the block when absent. An empty name
// or description is left untouched, so a caller can fix just the missing field.
// It is a surgical line-level edit — the body and any other frontmatter keys are
// preserved (no re-marshal) — used by the studio's one-tap skill-fix (and the
// assistant's write-SKILL flow) to clear the same name+description gate
// PackageSkills enforces at deploy time.
func UpsertSkillFrontmatter(skillMDPath, name, description string) error {
	if name == "" && description == "" {
		return nil
	}
	raw, err := os.ReadFile(skillMDPath)
	if err != nil {
		return err
	}
	text := string(raw)
	lines := strings.Split(text, "\n")

	// Locate an existing frontmatter block: first line "---" up to the next "---".
	hasBlock := len(lines) > 0 && strings.TrimSpace(lines[0]) == "---"
	end := -1
	if hasBlock {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
		}
		if end < 0 {
			hasBlock = false // unterminated "---" — treat as no frontmatter
		}
	}

	if !hasBlock {
		var b strings.Builder
		b.WriteString("---\n")
		if name != "" {
			b.WriteString("name: " + quoteYAMLValue(name) + "\n")
		}
		if description != "" {
			b.WriteString("description: " + quoteYAMLValue(description) + "\n")
		}
		b.WriteString("---\n\n")
		b.WriteString(text)
		return os.WriteFile(skillMDPath, []byte(b.String()), 0o644)
	}

	// Copy the frontmatter lines so append can't clobber the closing "---".
	fm := append([]string(nil), lines[1:end]...)
	set := func(key, val string) {
		if val == "" {
			return
		}
		line := key + ": " + quoteYAMLValue(val)
		for i, ln := range fm {
			if k, _, ok := strings.Cut(ln, ":"); ok && strings.TrimSpace(k) == key {
				fm[i] = line
				return
			}
		}
		fm = append(fm, line)
	}
	set("name", name)
	set("description", description)

	out := append([]string{lines[0]}, fm...)
	out = append(out, lines[end:]...)
	return os.WriteFile(skillMDPath, []byte(strings.Join(out, "\n")), 0o644)
}

// quoteYAMLValue renders a scalar for a SKILL.md frontmatter line. It stays bare
// when safe and quotes only when the value carries YAML-special characters (a
// colon, '#', or an existing quote), matching the forgiving reader in
// scanner.ParseSkillFrontmatter (which strips a single layer of surrounding
// quotes). Newlines fold to spaces — frontmatter values are single-line.
func quoteYAMLValue(v string) string {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\n", " "))
	if v == "" {
		return `""`
	}
	if !strings.ContainsAny(v, ":#\"'") {
		return v
	}
	if !strings.Contains(v, `"`) {
		return `"` + v + `"`
	}
	if !strings.Contains(v, `'`) {
		return `'` + v + `'`
	}
	return `"` + strings.ReplaceAll(v, `"`, `'`) + `"`
}
