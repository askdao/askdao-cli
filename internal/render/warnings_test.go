package render

import (
	"bytes"
	"strings"
	"testing"
)

func sampleWarnings() []TranslationWarning {
	return []TranslationWarning{
		{Field: "workspace.base_image", Action: "IGNORED",
			Reason: "Anthropic uses fixed cloud image.", Severity: SeverityHigh},
		{Field: "workspace.setup_commands", Action: "PARTIALLY IGNORED",
			Reason: "apt/pip extracted; raw commands lost.", Severity: SeverityHigh,
			FallbackAttempted: true},
		{Field: "workspace.users", Action: "IGNORED",
			Reason: "Anthropic runs as fixed user.", Severity: SeverityMedium},
	}
}

func TestRenderTranslationWarnings_SummaryViewKeepsHighShowsCounts(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderTranslationWarnings(r, "Anthropic Managed Agents", sampleWarnings(), ViewSummary)
	out := buf.String()

	// Both HIGH entries are spelled out.
	if !strings.Contains(out, "workspace.base_image = IGNORED") {
		t.Errorf("HIGH entry missing: %s", out)
	}
	if !strings.Contains(out, "workspace.setup_commands = PARTIALLY IGNORED") {
		t.Errorf("HIGH PARTIAL entry missing: %s", out)
	}
	// Acceptance criterion: MEDIUM/LOW collapsed to count line.
	if !strings.Contains(out, "MEDIUM (1)") || !strings.Contains(out, "LOW (0)") {
		t.Errorf("expected MEDIUM/LOW count line, got:\n%s", out)
	}
	if strings.Contains(out, "workspace.users = IGNORED") {
		t.Errorf("MEDIUM entry should be collapsed in summary view: %s", out)
	}
	// Harness label appears in section title.
	if !strings.Contains(out, "Anthropic Managed Agents") {
		t.Errorf("missing harness label: %s", out)
	}
}

func TestRenderTranslationWarnings_AllViewExpandsEverything(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderTranslationWarnings(r, "Anthropic", sampleWarnings(), ViewAll)
	out := buf.String()
	if !strings.Contains(out, "workspace.users = IGNORED") {
		t.Errorf("ViewAll should expand MEDIUM entries: %s", out)
	}
}

func TestRenderTranslationWarnings_FallbackEntryHighlighted(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderTranslationWarnings(r, "", []TranslationWarning{
		{Field: "x", Action: "PARTIALLY IGNORED",
			Reason: "soft fail", Severity: SeverityHigh, FallbackAttempted: true},
	}, ViewSummary)
	if !strings.Contains(buf.String(), "Fallback applied") {
		t.Errorf("fallback marker missing: %s", buf.String())
	}
}

func TestRenderTranslationWarnings_EmptyNoop(t *testing.T) {
	var buf bytes.Buffer
	RenderTranslationWarnings(NewPlain(&buf), "x", nil, ViewSummary)
	if buf.Len() != 0 {
		t.Errorf("empty warnings should be noop, got %q", buf.String())
	}
}
