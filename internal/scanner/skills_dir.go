// [INPUT]: 依赖 标准库 (crypto/sha256, encoding/hex, encoding/json, io/fs, os, path/filepath, sort, strings),
//
//	internal/types 的 DetectedSkill / SkillRef / Package / ImpliedAnthropicSkill
//
// [OUTPUT]: 对外提供 DetectSkills（custom_local skill 枚举 + bundle 体积 + lockfile 关联 + builtin 反向推）、
//
//	LoadSkillsLock（skills-lock.json → name→SkillRef map）、SkillDirCandidates、hashFile
//
// [POS]: internal/scanner 的 skill 探测器；产物喂 payload.go 做上传清单分类、archetype.go 做原型判定、recommender 做 L4 推荐
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// SkillDirCandidates are the well-known on-disk locations KOLs use to author
// custom skills. Each is checked relative to the project root. Exported so
// payload.go can classify the same directories.
var SkillDirCandidates = []string{
	".claude/skills",
	".agents/skills",
	".cursor/skills",
	"skills",
	"agents/skills",
}

// skillDirCandidates is an unexported alias so existing internal references
// stay terse.
var skillDirCandidates = SkillDirCandidates

// builtinSkillRule maps Anthropic-provided builtin skill IDs to the dependency
// fingerprint that suggests them.
type builtinSkillRule struct {
	skillID string
	deps    []string // pip dep names; ALL must be present
	reason  string
	conf    float64
}

// builtinSkillRules is the heuristic table for L3 skill inference. Conservative
// on confidence — recommender layer can choose to surface or hide based on it.
var builtinSkillRules = []builtinSkillRule{
	{"xlsx", []string{"pandas", "openpyxl"}, "detected dependency: pandas + openpyxl", 0.85},
	{"pdf", []string{"pypdf2"}, "detected dependency: PyPDF2", 0.75},
	{"pdf", []string{"pdfplumber"}, "detected dependency: pdfplumber", 0.78},
	{"docx", []string{"python-docx"}, "detected dependency: python-docx", 0.8},
}

// DetectSkills walks every candidate skill directory and emits one
// custom_local entry per `<dir>/<skill-name>/SKILL.md`, enriched with the
// frontmatter description, the whole-directory size, and (when a
// skills-lock.json is present) whether the skill is a vendored dependency or
// repo-native. Then it appends a single inferred-builtin entry whose
// ImpliedAnthropicSkills slice carries any matched skill IDs.
//
// pkgs is the syft-derived package map (from ScanPackages) used as input to
// the builtin-skill heuristic; pass nil to skip inference.
func DetectSkills(root string, pkgs map[string][]types.Package) ([]types.DetectedSkill, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}

	lock, _ := LoadSkillsLock(root) // best-effort; nil/empty when absent or malformed

	var out []types.DetectedSkill
	for _, rel := range skillDirCandidates {
		base := filepath.Join(root, rel)
		entries, err := os.ReadDir(base)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillFile := filepath.Join(base, e.Name(), "SKILL.md")
			info, err := os.Stat(skillFile)
			if err != nil {
				continue
			}
			ds := types.DetectedSkill{
				Source:          filepath.ToSlash(filepath.Join(rel, e.Name(), "SKILL.md")),
				SkillName:       e.Name(),
				Kind:            "custom_local",
				SizeBytes:       info.Size(),
				IsLocalOriginal: true,
			}
			_, ds.Description = parseSkillFrontmatter(skillFile)
			ds.BundleBytes, ds.BundleFiles = dirSize(filepath.Join(base, e.Name()))
			if ref, ok := lock[e.Name()]; ok {
				ds.LockedSource = ref.Source
				ds.LockedHash = ref.LockedHash
				ds.IsLocalOriginal = false
			}
			out = append(out, ds)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })

	if implied := inferBuiltinSkills(pkgs); len(implied) > 0 {
		out = append(out, types.DetectedSkill{ImpliedAnthropicSkills: implied})
	}
	return out, nil
}

// SkillsLockEntry is one record from skills-lock.json, used internally by the
// scanner to enrich DetectedSkill with origin metadata (LockedSource +
// LockedHash) and previously to populate the now-removed DeploymentPayload.
// SkillReferences sidecar. Public so test fixtures can stage lockfile-shaped
// data without going through file I/O.
type SkillsLockEntry struct {
	Name       string
	Source     string // e.g. "marswaveai/skills"
	LockedHash string // computedHash passthrough — used as origin tag short hash
}

// LoadSkillsLock finds a skills-lock.json (project root, plus each skill
// directory candidate and its parent) and parses it into a name→entry map.
// A missing or malformed lockfile yields a nil map and nil error — callers
// degrade gracefully.
func LoadSkillsLock(root string) (map[string]SkillsLockEntry, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}
	candidates := []string{filepath.Join(root, "skills-lock.json")}
	for _, rel := range skillDirCandidates {
		candidates = append(candidates,
			filepath.Join(root, rel, "skills-lock.json"),
			filepath.Join(root, filepath.Dir(rel), "skills-lock.json"),
		)
	}
	seen := map[string]bool{}
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc struct {
			Skills map[string]struct {
				Source       string `json:"source"`
				ComputedHash string `json:"computedHash"`
			} `json:"skills"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			continue // malformed → skip this source
		}
		if len(doc.Skills) == 0 {
			continue
		}
		out := make(map[string]SkillsLockEntry, len(doc.Skills))
		for name, s := range doc.Skills {
			out[name] = SkillsLockEntry{
				Name:       name,
				Source:     s.Source,
				LockedHash: s.ComputedHash,
			}
		}
		return out, nil
	}
	return nil, nil
}

// parseSkillFrontmatter reads the leading `--- ... ---` YAML frontmatter of a
// SKILL.md and returns its `name` / `description` values. It does a tiny
// line-based parse (no YAML dep) — enough for the flat `key: value` shape
// SKILL.md frontmatter uses in practice. Missing frontmatter → empty strings.
func parseSkillFrontmatter(path string) (name, description string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", ""
	}
	for _, ln := range lines[1:end] {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.Trim(strings.TrimSpace(v), `"'`)
		switch key {
		case "name":
			name = val
		case "description":
			description = val
		}
	}
	return name, description
}

// dirSize returns the recursive byte total and file count under root.
// Directories and unreadable entries are skipped silently.
func dirSize(root string) (bytes int64, files int) {
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files
}

// hashFile returns the hex sha256 of a file's contents, or "" on error. Used to
// compare a vendored skill's on-disk SKILL.md against its locked computedHash.
func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// inferBuiltinSkills walks builtinSkillRules; emits one ImpliedAnthropicSkill
// per rule whose dep fingerprint fully matches the project's pip packages.
// De-dupes by skill_id keeping the highest-confidence match.
func inferBuiltinSkills(pkgs map[string][]types.Package) []types.ImpliedAnthropicSkill {
	if len(pkgs) == 0 {
		return nil
	}
	have := map[string]bool{}
	for _, p := range pkgs["pip"] {
		have[normalizeDepName("pip", p.Name)] = true
	}

	best := map[string]types.ImpliedAnthropicSkill{}
	for _, rule := range builtinSkillRules {
		matched := true
		for _, d := range rule.deps {
			if !have[normalizeDepName("pip", d)] {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		cur, ok := best[rule.skillID]
		if !ok || rule.conf > cur.Confidence {
			best[rule.skillID] = types.ImpliedAnthropicSkill{
				SkillID:    rule.skillID,
				Reason:     rule.reason,
				Confidence: rule.conf,
			}
		}
	}
	if len(best) == 0 {
		return nil
	}
	out := make([]types.ImpliedAnthropicSkill, 0, len(best))
	for _, v := range best {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SkillID < out[j].SkillID })
	return out
}
