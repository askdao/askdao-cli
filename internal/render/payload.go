// [INPUT]: 依赖 标准库 (fmt, os, path/filepath, sort), internal/types 的 DeploymentPayload / PayloadEntry / ProjectArchetype
// [OUTPUT]: 对外提供 RenderPayload —— 把部署清单渲染成 KOL 友好的 WILL UPLOAD / EXCLUDED 两段
// [POS]: render 层的部署清单视图；被 cmd/askdao 的 bundle.go（full=true）与 detect.go（full=false 精简版）共用。
//
//	v0.7 起没有独立的 SKILL REFERENCES section —— 所有 custom skill 一律上传，
//	vendored 与原生通过 inline origin tag 区分（详见 docs/design.md §9.10、§9.14）。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package render

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/askdao/askdao-cli/internal/types"
)

// RenderPayload prints the deployment payload card. When full is true it lists
// every include/exclude path with sizes (and a one-line child summary for
// included directories); when false it prints the compact three-line digest
// used inside `askdao detect --summary`.
//
// Skill rows in the WILL UPLOAD section carry an inline origin tag:
//
//   - .agents/skills/spelling-homework-generator/  412 KB / 47 files   skill (repo-native)
//   - .agents/skills/asr/                            6 KB /  1 files   skill (vendored: marswaveai/skills @ 4f9d213)
//
// The tag is sourced from `PayloadEntry.Reason` for `Kind=="skill"` entries;
// non-skill rows render the bare `Kind` only.
func RenderPayload(r *Renderer, root string, p types.DeploymentPayload, a types.ProjectArchetype, full bool) {
	arch := a.Kind
	if arch == "" {
		arch = "unknown"
	}

	nativeCount, vendoredCount := countSkillOrigins(p.Includes)

	if !full {
		r.printf("Archetype: %s (conf %.2f)\n", arch, a.Confidence)
		if nativeCount+vendoredCount > 0 {
			r.printf("Deployment payload: %d files, %s will upload (%d skills: %d native + %d vendored) · %d path(s) excluded\n",
				p.TotalFiles, humanSize(p.TotalBytes), nativeCount+vendoredCount, nativeCount, vendoredCount, len(p.Excludes))
		} else {
			r.printf("Deployment payload: %d files, %s will upload · %d path(s) excluded\n",
				p.TotalFiles, humanSize(p.TotalBytes), len(p.Excludes))
		}
		r.println("  " + r.dim("run `askdao bundle` for the full list"))
		return
	}

	r.section(fmt.Sprintf("DEPLOYMENT PAYLOAD  (archetype: %s, conf %.2f)", arch, a.Confidence))
	r.println("")
	for _, e := range a.Evidence {
		r.println("  " + r.dim("· "+e))
	}
	if len(a.Evidence) > 0 {
		r.println("")
	}

	// WILL UPLOAD
	r.printf("%s  (%d files, %s)\n", r.bold("WILL UPLOAD"), p.TotalFiles, humanSize(p.TotalBytes))
	if len(p.Includes) == 0 {
		r.println("  " + r.dim("(nothing — is this an empty directory?)"))
	}
	for _, e := range p.Includes {
		r.printf("  %s %-48s %10s / %3d files   %s\n",
			r.green("+"), e.Path, humanSize(e.Bytes), e.Files, formatKind(r, e))
		if isDirEntry(e.Path) && e.Kind != "skill" {
			// Children summary helps for top-level project dirs; for skills the
			// origin tag already conveys the relevant context — keep it tight.
			if cs := childSummary(filepath.Join(root, filepath.FromSlash(e.Path))); cs != "" {
				r.println("      " + r.dim(cs))
			}
		}
	}
	r.println("")

	// EXCLUDED
	if len(p.Excludes) > 0 {
		r.printf("%s  (reasons)\n", r.bold("EXCLUDED"))
		for _, e := range p.Excludes {
			r.printf("  %s %-40s %s\n", r.red("-"), e.Path, r.dim(e.Reason))
		}
		r.println("")
	}

	r.println(r.dim(fmt.Sprintf("ignore sources: %v", p.IgnoreSources)))
}

// formatKind returns the right-column kind label for a payload entry.
//
//   - For Kind="skill": "skill (<origin tag>)" — the tag comes from Reason
//     (e.g. "repo-native", "vendored: marswaveai/skills @ 4f9d213").
//   - For everything else: just the dimmed Kind.
func formatKind(r *Renderer, e types.PayloadEntry) string {
	if e.Kind == "skill" {
		tag := e.Reason
		if tag == "" {
			tag = "origin unknown"
		}
		return r.dim(fmt.Sprintf("skill (%s)", tag))
	}
	return r.dim(e.Kind)
}

// countSkillOrigins splits include entries with Kind="skill" by origin tag.
// repo-native vs vendored is decided by Reason prefix (matches the inline tag
// scanner.payload.go emits).
func countSkillOrigins(includes []types.PayloadEntry) (native, vendored int) {
	for _, e := range includes {
		if e.Kind != "skill" {
			continue
		}
		if e.Reason == "repo-native" {
			native++
			continue
		}
		if len(e.Reason) >= 8 && e.Reason[:8] == "vendored" {
			vendored++
			continue
		}
		// Forward-compat: future origins fall into "other" — count toward
		// native so total still adds up to len(skill entries). The render
		// surface will still display whatever tag is in Reason.
		native++
	}
	return native, vendored
}

func isDirEntry(path string) bool {
	return len(path) > 0 && path[len(path)-1] == '/'
}

// childSummary lists a directory's immediate children with file counts, e.g.
// "SKILL.md, scripts/ (3), assets/ (1)". Empty when the dir can't be read.
func childSummary(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type item struct {
		name  string
		isDir bool
		files int
	}
	var items []item
	for _, e := range entries {
		it := item{name: e.Name(), isDir: e.IsDir()}
		if e.IsDir() {
			_, it.files = dirFileCount(filepath.Join(dir, e.Name()))
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].isDir != items[j].isDir {
			return !items[i].isDir // files first
		}
		return items[i].name < items[j].name
	})
	out := ""
	for i, it := range items {
		if i > 0 {
			out += ", "
		}
		if it.isDir {
			out += fmt.Sprintf("%s/ (%d)", it.name, it.files)
		} else {
			out += it.name
		}
		if i == 7 && len(items) > 8 {
			out += fmt.Sprintf(", … (%d more)", len(items)-8)
			break
		}
	}
	return out
}

func dirFileCount(dir string) (int64, int) {
	var bytes int64
	var files int
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			b, f := dirFileCount(filepath.Join(dir, e.Name()))
			bytes += b
			files += f
			continue
		}
		if info, err := e.Info(); err == nil {
			bytes += info.Size()
			files++
		}
	}
	return bytes, files
}

// humanSize renders a byte count compactly for the payload card.
func humanSize(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
