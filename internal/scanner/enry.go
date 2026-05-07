package scanner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	enry "github.com/go-enry/go-enry/v2"

	"github.com/askdao/askdao-cli/internal/types"
)

// maxEnryFileBytes caps how much of a single file we feed enry. Linguist
// itself samples ~16 KiB; reading everything blows up walltime for repos with
// minified bundles or vendored binaries.
const maxEnryFileBytes int64 = 16 * 1024

// DetectLanguages walks root and returns byte-level language percentages, using
// go-enry/v2 (the Go port of GitHub Linguist). Files vendored, generated,
// documentation-only, or under excluded directories are ignored — same
// classification Linguist applies for repository language stats.
func DetectLanguages(root string, excludes []string) ([]types.DetectedLanguage, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}
	excludeMatchers, err := compileGlobs(excludes)
	if err != nil {
		return nil, err
	}

	bytesByLang := map[string]int64{}
	filesByLang := map[string]int{}
	var totalBytes int64

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Permission errors and dangling symlinks shouldn't fail the whole scan.
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(rel, d.Name(), excludeMatchers) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if matchAny(excludeMatchers, rel) {
			return nil
		}

		// Linguist-style filters before we even read the file.
		if enry.IsVendor(rel) || enry.IsDocumentation(rel) || enry.IsConfiguration(rel) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		size := info.Size()
		if size == 0 {
			return nil
		}

		content, err := readFileLimited(path, maxEnryFileBytes)
		if err != nil {
			return nil
		}
		if enry.IsGenerated(rel, content) {
			return nil
		}
		lang := enry.GetLanguage(rel, content)
		if lang == "" {
			return nil
		}
		if t := enry.GetLanguageType(lang); t != enry.Programming && t != enry.Markup {
			// Skip Data / Prose so language % matches what KOLs see on GitHub.
			return nil
		}

		bytesByLang[lang] += size
		filesByLang[lang]++
		totalBytes += size
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	out := make([]types.DetectedLanguage, 0, len(bytesByLang))
	for lang, b := range bytesByLang {
		pct := 0.0
		if totalBytes > 0 {
			pct = float64(b) / float64(totalBytes) * 100.0
		}
		out = append(out, types.DetectedLanguage{
			Language:   lang,
			Bytes:      b,
			Percentage: roundPct(pct),
			Files:      filesByLang[lang],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Language < out[j].Language
	})
	return out, nil
}

func shouldSkipDir(rel, name string, excludes []globMatcher) bool {
	switch name {
	case ".git", "node_modules", ".venv", "venv", "__pycache__", "dist", "build", "target", ".next", ".nuxt":
		return true
	}
	if rel == "." {
		return false
	}
	return matchAny(excludes, rel)
}

func readFileLimited(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, max)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

func roundPct(v float64) float64 {
	// Two decimal places — Detection.percentage is human-facing.
	return float64(int64(v*100+0.5)) / 100.0
}
