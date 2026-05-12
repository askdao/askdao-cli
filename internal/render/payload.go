// [INPUT]: 依赖 标准库 (fmt, os, path/filepath, sort), internal/types 的 DeploymentPayload / PayloadEntry / SkillRef / ProjectArchetype
// [OUTPUT]: 对外提供 RenderPayload —— 把部署清单渲染成 KOL 友好的 WILL UPLOAD / SKILL REFERENCES / EXCLUDED 三段
// [POS]: render 层的部署清单视图；被 cmd/askdao 的 bundle.go（full=true）与 detect.go（full=false 精简版）共用
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
func RenderPayload(r *Renderer, root string, p types.DeploymentPayload, a types.ProjectArchetype, full bool) {
	arch := a.Kind
	if arch == "" {
		arch = "unknown"
	}

	if !full {
		r.printf("Archetype: %s (conf %.2f)\n", arch, a.Confidence)
		r.printf("Deployment payload: %d files, %s will upload · %d skill ref(s) · %d path(s) excluded\n",
			p.TotalFiles, humanSize(p.TotalBytes), len(p.SkillReferences), len(p.Excludes))
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
		r.printf("  %s %-48s %10s   %s\n", r.green("+"), e.Path, humanSize(e.Bytes), r.dim(e.Kind))
		if isDirEntry(e.Path) {
			if cs := childSummary(filepath.Join(root, filepath.FromSlash(e.Path))); cs != "" {
				r.println("      " + r.dim(cs))
			}
		}
	}
	r.println("")

	// SKILL REFERENCES
	if len(p.SkillReferences) > 0 {
		r.printf("%s  (%d — re-installed from registry, not uploaded)\n", r.bold("SKILL REFERENCES"), len(p.SkillReferences))
		for _, ref := range p.SkillReferences {
			tag := r.dim(ref.Resolvable)
			if ref.Resolvable == "unknown" {
				tag = r.yellow("resolvable: unknown")
			}
			r.printf("  %s %-20s %s @ %s   %s\n", r.blue("→"), ref.Name, orDash(ref.Source), shortHash(ref.LockedHash), tag)
		}
		r.println("")
	}

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

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func shortHash(h string) string {
	if len(h) <= 9 {
		return h
	}
	return h[:9] + "…"
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
