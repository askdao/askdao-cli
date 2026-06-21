package scanner

import (
	"path"
	"path/filepath"
	"strings"
)

// globMatcher carries both the original pattern (for diagnostics) and a
// normalized form usable with path.Match.
type globMatcher struct {
	pattern      string
	normalized   string
	isDoubleStar bool
}

func compileGlobs(patterns []string) ([]globMatcher, error) {
	out := make([]globMatcher, 0, len(patterns))
	for _, p := range patterns {
		norm := strings.TrimPrefix(p, "./")
		out = append(out, globMatcher{
			pattern:      p,
			normalized:   norm,
			isDoubleStar: strings.Contains(norm, "**"),
		})
	}
	return out, nil
}

// matchAny tests rel against each pattern. We support the syft-style
// `./vendor/**` convention by splitting on "**" and treating it as a
// recursive prefix match.
func matchAny(matchers []globMatcher, rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, m := range matchers {
		if m.isDoubleStar {
			prefix := strings.SplitN(m.normalized, "**", 2)[0]
			prefix = strings.TrimSuffix(prefix, "/")
			if prefix == "" || rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
			continue
		}
		if ok, _ := path.Match(m.normalized, rel); ok {
			return true
		}
		if ok, _ := path.Match(m.normalized, filepath.Base(rel)); ok {
			return true
		}
	}
	return false
}
