// Package render produces the KOL review card UX defined in design.md §3.1
// (v0.5 mid-density). Five concerns split across files:
//
//	summary.go    — 7-section mid-density card
//	reasoning.go  — inline `↳ Why:` reason lines + confidence
//	warnings.go   — translation warning blocks (HIGH always shown)
//	diff.go       — AgentSpec field-level diff for `agent deploy`
//	lists.go      — generic "前 N + and M more" truncation helper
//
// All renderers funnel through a single Renderer carrying the io.Writer +
// color toggle so cmd-layer can pass --no-color or pipe to a file.
//
// [INPUT]: types.AgentSpec / RecommendResponse / TranslationWarning slices
// [OUTPUT]: KOL-friendly text on Renderer.Out (default os.Stdout)
// [POS]: L4 / cmd 之间的渲染层；不持状态、不读 IO 之外的任何东西
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package render

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI color codes — only emitted when Renderer.Color is true. We hand-roll
// the few we need rather than pulling in a dep; the set is small and stable.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiGrey   = "\x1b[90m"
)

// Renderer is the render layer's only stateful bit. It carries the destination
// writer and a color toggle. All public render functions take a *Renderer so
// tests can capture output to a bytes.Buffer with Color=false for stable
// golden comparisons.
type Renderer struct {
	Out   io.Writer
	Color bool
	// Width is the target line width for box drawing rules; 0 falls back to 64
	// (matches design.md §3.1 examples). Truncation logic uses Width as a hint.
	Width int
}

// New returns a Renderer bound to os.Stdout with color enabled and the
// standard 63-column rule width matching design.md §3.1.
func New() *Renderer {
	return &Renderer{Out: os.Stdout, Color: true, Width: 63}
}

// NewPlain is the test-friendly constructor: writes to w, no ANSI codes.
func NewPlain(w io.Writer) *Renderer {
	return &Renderer{Out: w, Color: false, Width: 63}
}

func (r *Renderer) printf(format string, args ...any) {
	fmt.Fprintf(r.Out, format, args...)
}

func (r *Renderer) println(s string) {
	fmt.Fprintln(r.Out, s)
}

// color wraps s in an ANSI sequence when Color is on; passthrough otherwise.
func (r *Renderer) color(code, s string) string {
	if !r.Color {
		return s
	}
	return code + s + ansiReset
}

func (r *Renderer) red(s string) string    { return r.color(ansiRed, s) }
func (r *Renderer) green(s string) string  { return r.color(ansiGreen, s) }
func (r *Renderer) yellow(s string) string { return r.color(ansiYellow, s) }
func (r *Renderer) grey(s string) string   { return r.color(ansiGrey, s) }
func (r *Renderer) bold(s string) string   { return r.color(ansiBold, s) }
func (r *Renderer) dim(s string) string    { return r.color(ansiDim, s) }

// width returns the target rule width, defaulting to 63.
func (r *Renderer) width() int {
	if r.Width > 0 {
		return r.Width
	}
	return 63
}

// rule prints a `═══...═══` divider matching design.md §3.1 box drawing.
func (r *Renderer) rule() {
	r.println(strings.Repeat("═", r.width()))
}

// section prints a `═══` rule + centered-style title + closing rule.
func (r *Renderer) section(title string) {
	r.rule()
	r.printf(" %s\n", r.bold(title))
	r.rule()
}
