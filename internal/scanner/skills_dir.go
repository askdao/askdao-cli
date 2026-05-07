package scanner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/askdao/askdao-cli/internal/types"
)

// skillDirCandidates are the well-known on-disk locations KOLs use to author
// custom skills. Each is checked relative to the project root.
var skillDirCandidates = []string{
	".claude/skills",
	".agents/skills",
	".cursor/skills",
	"skills",
	"agents/skills",
}

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
// custom_local entry per `<dir>/<skill-name>/SKILL.md`. Then it appends a
// single inferred-builtin entry whose ImpliedAnthropicSkills slice carries any
// matched skill IDs.
//
// pkgs is the syft-derived package map (from ScanPackages) used as input to
// the builtin-skill heuristic; pass nil to skip inference.
func DetectSkills(root string, pkgs map[string][]types.Package) ([]types.DetectedSkill, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}

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
			out = append(out, types.DetectedSkill{
				Source:    filepath.ToSlash(filepath.Join(rel, e.Name(), "SKILL.md")),
				SkillName: e.Name(),
				Kind:      "custom_local",
				SizeBytes: info.Size(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })

	if implied := inferBuiltinSkills(pkgs); len(implied) > 0 {
		out = append(out, types.DetectedSkill{ImpliedAnthropicSkills: implied})
	}
	return out, nil
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
