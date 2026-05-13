package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

// miniPipelineRepo builds a stripped-down homework-spelling layout: one
// repo-native skill, one vendored skill pinned in skills-lock.json, plus the
// usual junk/generated/input directories.
func miniPipelineRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "CLAUDE.md"), "# operating manual\n")
	mustWrite(t, filepath.Join(root, "package.json"), `{"devDependencies":{"playwright":"^1.60.0"}}`)
	mustWrite(t, filepath.Join(root, "skills-lock.json"),
		`{"version":1,"skills":{"tts":{"source":"marswaveai/skills","sourceType":"github","skillPath":"tts/SKILL.md","computedHash":"abc123"}}}`)
	mustWrite(t, filepath.Join(root, ".agents", "skills", "homework-gen", "SKILL.md"),
		"---\nname: homework-gen\ndescription: turn a spelling list into an HTML page\n---\n# body\n")
	mustWrite(t, filepath.Join(root, ".agents", "skills", "homework-gen", "scripts", "render.mjs"), "// render\n")
	mustWrite(t, filepath.Join(root, ".agents", "skills", "homework-gen", "evals", "case1.json"), "{}")
	mustWrite(t, filepath.Join(root, ".agents", "skills", "tts", "SKILL.md"), "---\nname: tts\n---\n# tts\n")
	mustWrite(t, filepath.Join(root, "input", "spelling.pdf"), "%PDF-1.4 stub")
	mustWrite(t, filepath.Join(root, "output", "spelling.html"), "<html></html>")
	mustWrite(t, filepath.Join(root, "node_modules", "playwright", "index.js"), "module.exports={}")
	mustWrite(t, filepath.Join(root, ".DS_Store"), "junk")
	return root
}

// assembleDetection runs the cheap scanner pieces the payload step depends on.
func assembleDetection(t *testing.T, root string) *types.Detection {
	t.Helper()
	det := &types.Detection{}
	var err error
	det.DetectedSkills, err = DetectSkills(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	det.DetectedLanguages, _ = DetectLanguages(root, nil)
	det.Archetype = InferArchetype(det)
	return det
}

func findEntry(es []types.PayloadEntry, path string) (types.PayloadEntry, bool) {
	for _, e := range es {
		if e.Path == path {
			return e, true
		}
	}
	return types.PayloadEntry{}, false
}

func TestDetectDeploymentPayload_LockfileDrivenSkillClassification(t *testing.T) {
	root := miniPipelineRepo(t)
	det := assembleDetection(t, root)

	pl, warns := DetectDeploymentPayload(root, det, PayloadOptions{IncludeEvals: true})

	// Repo-native skill ships inline.
	if _, ok := findEntry(pl.Includes, ".agents/skills/homework-gen/"); !ok {
		t.Errorf("repo-native skill missing from includes: %+v", pl.Includes)
	}
	// Vendored skill is a reference, not an upload.
	if _, ok := findEntry(pl.Includes, ".agents/skills/tts/"); ok {
		t.Errorf("vendored skill should NOT be in includes")
	}
	if len(pl.SkillReferences) != 1 || pl.SkillReferences[0].Name != "tts" {
		t.Fatalf("expected one skill reference (tts), got %+v", pl.SkillReferences)
	}
	if pl.SkillReferences[0].Source != "marswaveai/skills" || pl.SkillReferences[0].Resolvable != "yes" {
		t.Errorf("unexpected skill ref: %+v", pl.SkillReferences[0])
	}
	if _, ok := findEntry(pl.Excludes, ".agents/skills/tts/"); !ok {
		t.Errorf("vendored skill should appear in excludes with a reason")
	}

	// Agent doc + manifests included.
	for _, want := range []string{"CLAUDE.md", "package.json", "skills-lock.json"} {
		if _, ok := findEntry(pl.Includes, want); !ok {
			t.Errorf("%s should be included", want)
		}
	}

	// Junk / generated / input excluded.
	for _, want := range []string{".DS_Store", "output/", "input/", "node_modules/"} {
		if _, ok := findEntry(pl.Excludes, want); !ok {
			t.Errorf("%s should be excluded", want)
		}
	}
	if _, ok := findEntry(pl.Includes, "node_modules/"); ok {
		t.Errorf("node_modules must never be in includes")
	}

	// Totals only count includes.
	if pl.TotalBytes == 0 || pl.TotalFiles == 0 {
		t.Errorf("totals should be non-zero: %+v", pl)
	}

	for _, w := range warns {
		t.Logf("warning: %s", w)
	}
}

func TestDetectDeploymentPayload_NoEvals(t *testing.T) {
	root := miniPipelineRepo(t)
	det := assembleDetection(t, root)

	with, _ := DetectDeploymentPayload(root, det, PayloadOptions{IncludeEvals: true})
	without, _ := DetectDeploymentPayload(root, det, PayloadOptions{IncludeEvals: false})

	w1, _ := findEntry(with.Includes, ".agents/skills/homework-gen/")
	w0, _ := findEntry(without.Includes, ".agents/skills/homework-gen/")
	if w0.Files >= w1.Files {
		t.Errorf("--no-evals should reduce skill file count: with=%d without=%d", w1.Files, w0.Files)
	}
}

func TestDetectDeploymentPayload_NoLockfileBundlesAll(t *testing.T) {
	root := miniPipelineRepo(t)
	if err := os.Remove(filepath.Join(root, "skills-lock.json")); err != nil {
		t.Fatal(err)
	}
	det := assembleDetection(t, root)
	pl, warns := DetectDeploymentPayload(root, det, PayloadOptions{})

	if _, ok := findEntry(pl.Includes, ".agents/skills/tts/"); !ok {
		t.Errorf("without a lockfile every skill should be bundled inline")
	}
	if len(pl.SkillReferences) != 0 {
		t.Errorf("no lockfile → no references, got %+v", pl.SkillReferences)
	}
	foundWarn := false
	for _, w := range warns {
		if strings.Contains(w, "no skills-lock.json") {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Errorf("expected a 'no skills-lock.json' warning, got %v", warns)
	}
}

func TestDetectDeploymentPayload_AskdaoignoreNegation(t *testing.T) {
	root := miniPipelineRepo(t)
	mustWrite(t, filepath.Join(root, ".askdaoignore"), "# keep one generated file\n!output/spelling.html\n")
	det := assembleDetection(t, root)
	pl, _ := DetectDeploymentPayload(root, det, PayloadOptions{})

	if _, ok := findEntry(pl.Includes, "output/spelling.html"); !ok {
		t.Errorf("negated path should be force-included: %+v", pl.Includes)
	}
	// The directory itself stays excluded.
	if _, ok := findEntry(pl.Excludes, "output/"); !ok {
		t.Errorf("output/ dir should still be excluded")
	}
	hasSrc := false
	for _, s := range pl.IgnoreSources {
		if s == ".askdaoignore" {
			hasSrc = true
		}
	}
	if !hasSrc {
		t.Errorf(".askdaoignore should be listed as an ignore source: %v", pl.IgnoreSources)
	}
}

func TestDetectDeploymentPayload_ForceBundleSkill(t *testing.T) {
	root := miniPipelineRepo(t)
	det := assembleDetection(t, root)
	pl, _ := DetectDeploymentPayload(root, det, PayloadOptions{ForceBundleSkills: []string{"tts"}})

	if _, ok := findEntry(pl.Includes, ".agents/skills/tts/"); !ok {
		t.Errorf("--bundle-skill tts should move tts into includes")
	}
	for _, ref := range pl.SkillReferences {
		if ref.Name == "tts" {
			t.Errorf("force-bundled skill should not also be a reference")
		}
	}
}
