package scanner

import (
	"path/filepath"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestDetectSkills_CustomDirsAndInference(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "skills", "portfolio-analyzer", "SKILL.md"),
		"# Portfolio Analyzer\n\nLong description...\n")
	mustWrite(t, filepath.Join(root, "skills", "report-writer", "SKILL.md"),
		"# Report writer\n")

	pkgs := map[string][]types.Package{
		"pip": {
			{Name: "pandas", IsProd: true},
			{Name: "openpyxl", IsProd: true},
			{Name: "fastapi", IsProd: true},
		},
	}

	got, err := DetectSkills(root, pkgs, ScanScopeOpts{})
	if err != nil {
		t.Fatal(err)
	}

	customs := 0
	var implied []types.ImpliedAnthropicSkill
	for _, e := range got {
		if e.Kind == "custom_local" {
			customs++
			if e.SizeBytes <= 0 {
				t.Errorf("custom_local SizeBytes should be >0: %+v", e)
			}
		}
		if e.ImpliedAnthropicSkills != nil {
			implied = e.ImpliedAnthropicSkills
		}
	}
	if customs != 2 {
		t.Errorf("expected 2 custom_local skills, got %d (%+v)", customs, got)
	}
	if len(implied) != 1 || implied[0].SkillID != "xlsx" {
		t.Errorf("expected xlsx inference, got %+v", implied)
	}
	if implied[0].Confidence < 0.8 {
		t.Errorf("low confidence: %v", implied[0].Confidence)
	}
}

func TestDetectSkills_NoMatches(t *testing.T) {
	got, err := DetectSkills(t.TempDir(), nil, ScanScopeOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestInferBuiltinSkills_DedupesByID(t *testing.T) {
	pkgs := map[string][]types.Package{
		"pip": {
			{Name: "PyPDF2"},
			{Name: "pdfplumber"},
		},
	}
	got := inferBuiltinSkills(pkgs)
	if len(got) != 1 || got[0].SkillID != "pdf" {
		t.Errorf("expected single pdf entry, got %+v", got)
	}
	// Higher-conf rule (pdfplumber) wins.
	if got[0].Confidence < 0.78 {
		t.Errorf("expected pdfplumber rule (conf 0.78), got %v", got[0])
	}
}

func TestDetectSkills_UserScopeGatedByMarker(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	// Project marker (.claude/) + a project-scope skill.
	mustWrite(t, filepath.Join(root, ".claude", "skills", "proj-skill", "SKILL.md"), "# proj\n")
	// A global skill under a fake HOME.
	mustWrite(t, filepath.Join(home, ".claude", "skills", "global-skill", "SKILL.md"), "# global\n")

	got, err := DetectSkills(root, nil, ScanScopeOpts{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	var proj, user *types.DetectedSkill
	for i := range got {
		switch got[i].SkillName {
		case "proj-skill":
			proj = &got[i]
		case "global-skill":
			user = &got[i]
		}
	}
	if proj == nil || proj.Scope != "project" || proj.Harness != "claude" {
		t.Errorf("proj-skill: want scope=project harness=claude, got %+v", proj)
	}
	if user == nil || user.Scope != "user" || user.Harness != "claude" {
		t.Errorf("global-skill: want scope=user harness=claude, got %+v", user)
	}
}

func TestDetectSkills_CodexUserScope(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	// .codex/ marker activates Codex user-scope discovery.
	mustWrite(t, filepath.Join(root, ".codex", "config.toml"), "x")
	// Codex global skills live at ~/.agents/skills (official path).
	mustWrite(t, filepath.Join(home, ".agents", "skills", "codex-skill", "SKILL.md"), "# codex\n")

	got, err := DetectSkills(root, nil, ScanScopeOpts{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	var user *types.DetectedSkill
	for i := range got {
		if got[i].SkillName == "codex-skill" {
			user = &got[i]
		}
	}
	if user == nil || user.Scope != "user" || user.Harness != "codex" {
		t.Errorf("codex-skill: want scope=user harness=codex, got %+v", user)
	}
}

func TestDetectSkills_UserScopeSkippedWithoutMarker(t *testing.T) {
	root := t.TempDir() // no .claude/ marker
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".claude", "skills", "global-skill", "SKILL.md"), "# global\n")

	got, err := DetectSkills(root, nil, ScanScopeOpts{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.Scope == "user" {
			t.Errorf("no harness marker in root → user scope must be skipped, got %+v", s)
		}
	}
}
