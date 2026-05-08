package render

import (
	"fmt"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// ReasonOpts pins the inline reasoning block format from design.md §3.1:
//
//	↳ Why: FastAPI + Alembic migration logic complex
//	       Confidence: 0.78
type ReasonOpts struct {
	// Indent prefixes the entire block (typically the column where the value
	// it justifies starts). Defaults to "" — caller usually passes a 19-space
	// indent matching the PERSONA card column.
	Indent string
	// Confidence is rendered if > 0.0; values >= 1 print as 1.00 anyway so
	// confidence is bounded for the KOL.
	Confidence float64
}

// RenderReason emits an `↳ Why:` block for one decision. Multi-line reasons
// keep the leading `↳` on the first line and align continuation lines.
func RenderReason(r *Renderer, reason string, opts ReasonOpts) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	indent := opts.Indent

	prefix := indent + r.dim("↳ Why: ")
	cont := indent + "       " // align with the colon

	lines := strings.Split(reason, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " ")
		if i == 0 {
			r.printf("%s%s\n", prefix, line)
		} else {
			r.printf("%s%s\n", cont, line)
		}
	}

	if opts.Confidence > 0 {
		c := opts.Confidence
		if c > 1 {
			c = 1
		}
		r.printf("%sConfidence: %.2f\n", cont, c)
	}
}

// RenderReasoningTrace prints all reasoning decisions flat — used by the
// `[R] view full reasoning trace` action and `askdao agent show --reasoning`.
func RenderReasoningTrace(r *Renderer, decisions []types.ReasoningDecision) {
	if len(decisions) == 0 {
		r.println(r.dim("    (no reasoning decisions recorded)"))
		return
	}
	for i, d := range decisions {
		header := fmt.Sprintf("  %d. %s", i+1, d.Decision)
		r.println(r.bold(header))
		RenderReason(r, d.Reason, ReasonOpts{Indent: "     ", Confidence: d.Confidence})
		if i < len(decisions)-1 {
			r.println("")
		}
	}
}
