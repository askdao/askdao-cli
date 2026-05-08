package render

import (
	"fmt"
	"strings"
)

// TruncateOpts tunes how RenderItemList wraps a slice into "前 N + and M more"
// shape (design.md §3.1 字段三档分类的"列计数 + 入口"档位).
type TruncateOpts struct {
	// MaxShown is the maximum number of items printed inline. Defaults to 8
	// when zero (matches the §3.1 Python deps example).
	MaxShown int
	// Indent is prepended to every printed bullet line.
	Indent string
	// Bullet sets the leading marker; "•" by default.
	Bullet string
	// MoreEntry is the trailing "...and N more   [D] view all" line. Empty
	// disables the trailing line entirely.
	MoreEntry string
}

// RenderItemList prints up to MaxShown items, two per line — column-aligned —
// and a trailing summary when more items exist.
//
// Two-column layout matches the design.md §3.1 example:
//
//   - fastapi==0.135.1          • alembic==1.18.4
//   - sqlalchemy==2.0.48         • anthropic==0.97.0
//     ... and 20 more   [D] view all   [F] view 14 filtered
func RenderItemList(r *Renderer, items []string, opts TruncateOpts) {
	if len(items) == 0 {
		return
	}
	maxShown := opts.MaxShown
	if maxShown <= 0 {
		maxShown = 8
	}
	indent := opts.Indent
	if indent == "" {
		indent = "    "
	}
	bullet := opts.Bullet
	if bullet == "" {
		bullet = "•"
	}

	shown := items
	hidden := 0
	if len(items) > maxShown {
		shown = items[:maxShown]
		hidden = len(items) - maxShown
	}

	// Compute column width from the longest first-column item; rounded up to
	// an even spacing for visual consistency.
	colWidth := 0
	for i := 0; i < len(shown); i += 2 {
		if w := len(shown[i]); w > colWidth {
			colWidth = w
		}
	}
	colWidth += 4 // breathing room before second column

	for i := 0; i < len(shown); i += 2 {
		if i+1 < len(shown) {
			padded := fmt.Sprintf("%-*s", colWidth, shown[i])
			r.printf("%s%s %s   %s %s\n", indent, bullet, padded, bullet, shown[i+1])
		} else {
			r.printf("%s%s %s\n", indent, bullet, shown[i])
		}
	}

	if hidden > 0 {
		trailer := opts.MoreEntry
		if trailer == "" {
			trailer = fmt.Sprintf("... and %d more", hidden)
		} else {
			trailer = fmt.Sprintf("... and %d more   %s", hidden, trailer)
		}
		r.printf("%s%s\n", indent, r.dim(trailer))
	}
}

// RenderKVList prints `Label : Value` pairs left-padded to a common column
// width — used by the PERSONA / RUNTIME blocks where each line is one field.
func RenderKVList(r *Renderer, indent string, labelWidth int, pairs [][2]string) {
	for _, p := range pairs {
		r.printf("%s%-*s : %s\n", indent, labelWidth, p[0], p[1])
	}
}

// JoinTruncated returns `a, b, c, … and N more` for inline contexts (used in
// Capability scope summaries that don't get their own bullet block).
func JoinTruncated(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	rest := len(items) - max
	return fmt.Sprintf("%s, … and %d more", strings.Join(items[:max], ", "), rest)
}
