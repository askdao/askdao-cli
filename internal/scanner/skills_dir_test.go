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

	got, err := DetectSkills(root, pkgs)
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
	got, err := DetectSkills(t.TempDir(), nil)
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
