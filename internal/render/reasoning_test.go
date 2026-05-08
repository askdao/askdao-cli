package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestRenderReason_InlineFormat(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderReason(r, "FastAPI + Alembic migration logic complex", ReasonOpts{
		Indent:     "    ",
		Confidence: 0.78,
	})
	out := buf.String()
	if !strings.Contains(out, "↳ Why: FastAPI + Alembic") {
		t.Errorf("missing ↳ Why marker:\n%s", out)
	}
	if !strings.Contains(out, "Confidence: 0.78") {
		t.Errorf("missing confidence line:\n%s", out)
	}
}

func TestRenderReason_NoConfidenceWhenZero(t *testing.T) {
	var buf bytes.Buffer
	RenderReason(NewPlain(&buf), "x", ReasonOpts{})
	if strings.Contains(buf.String(), "Confidence:") {
		t.Errorf("zero confidence should not print line: %q", buf.String())
	}
}

func TestRenderReason_MultiLineAlignsContinuations(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderReason(r, "line one\nline two", ReasonOpts{Indent: "  "})
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], "  ↳ Why: ") {
		t.Errorf("first line should start with marker, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "         ") {
		t.Errorf("continuation should be indented, got %q", lines[1])
	}
}

func TestRenderReason_EmptyReasonNoop(t *testing.T) {
	var buf bytes.Buffer
	RenderReason(NewPlain(&buf), "  \n  ", ReasonOpts{})
	if buf.Len() != 0 {
		t.Errorf("blank reason should produce no output, got %q", buf.String())
	}
}

func TestRenderReasoningTrace(t *testing.T) {
	var buf bytes.Buffer
	r := NewPlain(&buf)
	RenderReasoningTrace(r, []types.ReasoningDecision{
		{Decision: "selected_runtime=python", Reason: "FastAPI detected", Confidence: 0.9},
		{Decision: "shell.permission=ask_for_dangerous", Reason: "Production deploy detected"},
	})
	out := buf.String()
	if !strings.Contains(out, "1. selected_runtime=python") || !strings.Contains(out, "2. shell.permission") {
		t.Errorf("expected numbered headers, got:\n%s", out)
	}
}

func TestRenderReasoningTrace_EmptyDecisionsShowsHint(t *testing.T) {
	var buf bytes.Buffer
	RenderReasoningTrace(NewPlain(&buf), nil)
	if !strings.Contains(buf.String(), "no reasoning decisions recorded") {
		t.Errorf("empty trace should print hint, got %q", buf.String())
	}
}
