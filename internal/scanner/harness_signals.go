package scanner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/askdao/askdao-cli/internal/types"
)

// HarnessProbeOpts lets tests inject a fake HOME so we can probe a temp tree.
type HarnessProbeOpts struct {
	// HomeDir overrides os.UserHomeDir(); empty falls through to the real home.
	HomeDir string
}

// DetectHarnessSignals probes the user's HOME for installed agent harnesses
// (Claude Code, Codex, Cursor, Gemini CLI) and produces a recommended deploy
// target based on which footprint is present.
func DetectHarnessSignals(opts HarnessProbeOpts) (types.DetectedHarnessSignals, error) {
	home := opts.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return types.DetectedHarnessSignals{}, errors.New("scanner: cannot resolve $HOME")
		}
		home = h
	}

	out := types.DetectedHarnessSignals{
		ClaudeCode: probeClaudeCode(home),
		Codex:      probeCodex(home),
		Cursor:     probeCursor(home),
		GeminiCLI:  probeGemini(home),
	}
	out.RecommendedHarness, out.RecommendationReason = recommendHarness(out)
	return out, nil
}

func probeClaudeCode(home string) types.HarnessProbe {
	root := filepath.Join(home, ".claude")
	if !dirExists(root) {
		return types.HarnessProbe{Installed: false, Evidence: []string{}}
	}
	ev := []string{"~/.claude/ exists"}
	if n := countSkillFiles(filepath.Join(root, "skills")); n > 0 {
		ev = append(ev, "~/.claude/skills/ has "+strconv.Itoa(n)+" SKILL.md")
	}
	return types.HarnessProbe{Installed: true, Evidence: ev}
}

func probeCodex(home string) types.HarnessProbe {
	root := filepath.Join(home, ".codex")
	if !dirExists(root) {
		return types.HarnessProbe{Installed: false, Evidence: []string{}}
	}
	return types.HarnessProbe{Installed: true, Evidence: []string{"~/.codex/ exists"}}
}

func probeCursor(home string) types.HarnessProbe {
	for _, rel := range []string{".cursor", "Library/Application Support/Cursor"} {
		if dirExists(filepath.Join(home, rel)) {
			return types.HarnessProbe{Installed: true, Evidence: []string{"~/" + rel + " exists"}}
		}
	}
	return types.HarnessProbe{Installed: false, Evidence: []string{}}
}

func probeGemini(home string) types.HarnessProbe {
	for _, rel := range []string{".gemini", ".config/gemini"} {
		if dirExists(filepath.Join(home, rel)) {
			return types.HarnessProbe{Installed: true, Evidence: []string{"~/" + rel + " exists"}}
		}
	}
	return types.HarnessProbe{Installed: false, Evidence: []string{}}
}

// recommendHarness applies the documented harness priority order:
//
//	claude_code installed → anthropic_managed_agents (current Phase 1 path)
//	codex installed       → openai_agents_sdk (Phase 2 — recorded but deploy
//	                        will still route through anthropic until adapter
//	                        ships)
//	otherwise             → anthropic_managed_agents as the safe default
func recommendHarness(s types.DetectedHarnessSignals) (string, string) {
	switch {
	case s.ClaudeCode.Installed:
		return "anthropic_managed_agents",
			"Claude Code is the primary local harness on this machine; Anthropic Managed Agents is the natural cloud counterpart"
	case s.Codex.Installed:
		return "openai_agents_sdk",
			"Codex is the primary local harness on this machine; OpenAI Agents SDK is the matching cloud runtime (Phase 2)"
	default:
		return "anthropic_managed_agents",
			"No local harness footprint detected; defaulting to Anthropic Managed Agents"
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func countSkillFiles(skillRoot string) int {
	n := 0
	_ = filepath.WalkDir(skillRoot, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			n++
		}
		return nil
	})
	return n
}
