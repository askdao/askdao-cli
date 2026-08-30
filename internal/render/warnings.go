package render

import (
	"sort"
	"strings"
)

// Severity levels for translation warnings. design.md §3.1 prescribes:
//   - HIGH always shown verbatim
//   - MEDIUM / LOW collapsed to a count line by default; full view via [W]
type Severity string

const (
	SeverityHigh   Severity = "HIGH"
	SeverityMedium Severity = "MEDIUM"
	SeverityLow    Severity = "LOW"
)

// TranslationWarning is what the conductor adapter returns at deploy time — a
// triple of (field, action, reason). askdao-cli's render layer doesn't depend
// on the conductor schema yet (Phase 1 has only AnthropicAdapter), so we
// define a portable Warning struct here and let cmd-layer build them.
type TranslationWarning struct {
	Field             string
	Action            string // IGNORED | PARTIALLY IGNORED | TRANSLATED | ...
	Reason            string
	Severity          Severity
	FallbackAttempted bool
}

// WarningView controls how the warnings section renders.
type WarningView int

const (
	// ViewSummary keeps HIGH visible, collapses MEDIUM/LOW to "MEDIUM (1) · LOW (0)   [W] see all".
	ViewSummary WarningView = iota
	// ViewAll prints every warning verbatim under its severity heading.
	ViewAll
)

// RenderTranslationWarnings emits the design.md §3.1 TRANSLATION WARNINGS block.
// The harness label (e.g. "Anthropic Managed Agents") is shown in the section
// title so KOLs see which adapter the warnings came from.
func RenderTranslationWarnings(r *Renderer, harness string, warnings []TranslationWarning, view WarningView) {
	if len(warnings) == 0 {
		return
	}
	bySev := groupBySeverity(warnings)
	sectionTitle := "⚠️  TRANSLATION WARNINGS"
	if harness != "" {
		sectionTitle = sectionTitle + "  (" + harness + ")"
	}
	r.section(sectionTitle)
	r.println("")

	highs := bySev[SeverityHigh]
	if len(highs) > 0 {
		r.printf("  %s (%d):\n", r.red("HIGH"), len(highs))
		for _, w := range highs {
			renderOneWarning(r, w)
		}
		r.println("")
	}

	if view == ViewAll {
		for _, sev := range []Severity{SeverityMedium, SeverityLow} {
			ws := bySev[sev]
			if len(ws) == 0 {
				continue
			}
			label := r.yellow(string(sev))
			if sev == SeverityLow {
				label = r.grey(string(sev))
			}
			r.printf("  %s (%d):\n", label, len(ws))
			for _, w := range ws {
				renderOneWarning(r, w)
			}
			r.println("")
		}
		return
	}

	// ViewSummary: roll MEDIUM + LOW into a single count line with [W] hint.
	mid := len(bySev[SeverityMedium])
	low := len(bySev[SeverityLow])
	if mid+low > 0 {
		r.printf("  %s (%d) · %s (%d)   %s\n",
			r.yellow("MEDIUM"), mid,
			r.grey("LOW"), low,
			r.dim("[W] see all"))
	}
}

// renderOneWarning prints a single warning entry plus its fallback hint.
func renderOneWarning(r *Renderer, w TranslationWarning) {
	r.printf("    • %s = %s\n", r.bold(w.Field), w.Action)
	if strings.TrimSpace(w.Reason) != "" {
		r.printf("      %s %s\n", r.dim("→"), w.Reason)
	}
	if w.FallbackAttempted {
		r.printf("      %s\n", r.green("→ Fallback applied where possible."))
	}
}

func groupBySeverity(ws []TranslationWarning) map[Severity][]TranslationWarning {
	out := map[Severity][]TranslationWarning{}
	for _, w := range ws {
		sev := w.Severity
		if sev == "" {
			sev = SeverityMedium // default
		}
		out[sev] = append(out[sev], w)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].Field < out[k][j].Field })
	}
	return out
}

