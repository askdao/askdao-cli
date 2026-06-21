package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

// miniPipelineRepo builds a stripped-down content-pipeline layout: one
// repo-native skill, one vendored skill pinned in skills-lock.json, plus the
// usual junk/generated/input directories.
func miniPipelineRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "CLAUDE.md"), "# operating manual\n")
	mustWrite(t, filepath.Join(root, "package.json"), `{"devDependencies":{"playwright":"^1.60.0"}}`)
	mustWrite(t, filepath.Join(root, "skills-lock.json"),
		`{"version":1,"skills":{"tts":{"source":"some-org/skills","sourceType":"github","skillPath":"tts/SKILL.md","computedHash":"abc123"}}}`)
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
	det.DetectedSkills, err = DetectSkills(root, nil, ScanScopeOpts{})
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

func TestDetectDeploymentPayload_AllSkillsShipInline(t *testing.T) {
	// v0.7: vendored and repo-native skills ALL ship inline (Anthropic Managed
	// Agents has no "reinstall from registry" path). Origin metadata
	// (LockedSource / LockedHash) is carried on DetectedSkill for the bundle
	// UI to render an inline tag — no separate SkillReferences sidecar.
	root := miniPipelineRepo(t)
	det := assembleDetection(t, root)

	pl, warns := DetectDeploymentPayload(root, det, PayloadOptions{IncludeEvals: true})

	// Both skills ship inline.
	nativeEntry, hasNative := findEntry(pl.Includes, ".agents/skills/homework-gen/")
	if !hasNative {
		t.Errorf("repo-native skill missing from includes: %+v", pl.Includes)
	}
	if nativeEntry.Reason != "repo-native" {
		t.Errorf("repo-native skill reason wrong: %q", nativeEntry.Reason)
	}
	vendoredEntry, hasVendored := findEntry(pl.Includes, ".agents/skills/tts/")
	if !hasVendored {
		t.Errorf("vendored skill should also be in includes (v0.7)")
	}
	if !strings.HasPrefix(vendoredEntry.Reason, "vendored: some-org/skills") {
		t.Errorf("vendored skill origin tag wrong: %q", vendoredEntry.Reason)
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

func TestDetectDeploymentPayload_NoLockfileStillIncludesEverything(t *testing.T) {
	// Without a lockfile, every skill is still in Includes (v0.7 contract:
	// no skill is ever a "reference"). The only difference: no LockedSource
	// metadata, so the Reason tag falls back to "repo-native".
	root := miniPipelineRepo(t)
	if err := os.Remove(filepath.Join(root, "skills-lock.json")); err != nil {
		t.Fatal(err)
	}
	det := assembleDetection(t, root)
	pl, _ := DetectDeploymentPayload(root, det, PayloadOptions{})

	if _, ok := findEntry(pl.Includes, ".agents/skills/tts/"); !ok {
		t.Errorf("without a lockfile every skill should be bundled inline")
	}
	if _, ok := findEntry(pl.Includes, ".agents/skills/homework-gen/"); !ok {
		t.Errorf("repo-native skill must always be included")
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
