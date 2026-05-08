package providers

import (
	"sort"

	"github.com/askdao/askdao-cli/internal/types"
)

// pythonAptMap is the ported nixpacks reverse map (python.rs:240-268) plus
// askdao-cli additions. Keys are PEP 503 normalized (lowercase, `_`/`.` → `-`).
//
// Each entry's `apt` slice is the apt-get install list. The reason string is
// surfaced verbatim into detection.json inferred_apt_packages[].reason.
var pythonAptMap = map[string]aptEntry{
	"psycopg2":        {pkgs: []string{"libpq-dev", "gcc"}, reason: "Python deps psycopg2/asyncpg need postgres headers"},
	"psycopg2-binary": {pkgs: []string{"libpq-dev", "gcc"}, reason: "Python deps psycopg2/asyncpg need postgres headers"},
	"psycopg":         {pkgs: []string{"libpq-dev", "gcc"}, reason: "Python deps psycopg2/asyncpg need postgres headers"},
	"asyncpg":         {pkgs: []string{"libpq-dev", "gcc"}, reason: "Python deps psycopg2/asyncpg need postgres headers"},
	"mysqlclient":     {pkgs: []string{"libmysqlclient-dev", "gcc"}, reason: "mysqlclient needs mysql C headers"},
	"pymysql":         {pkgs: []string{"libmysqlclient-dev"}, reason: "PyMySQL prefers mysql client headers when available"},
	"pydub":           {pkgs: []string{"ffmpeg"}, reason: "pydub shells out to ffmpeg"},
	"pdf2image":       {pkgs: []string{"poppler-utils"}, reason: "pdf2image shells out to pdftoppm/pdftocairo"},
	"cairo":           {pkgs: []string{"libcairo2-dev"}, reason: "cairo bindings need libcairo2 headers"},
	"pycairo":         {pkgs: []string{"libcairo2-dev"}, reason: "pycairo bindings need libcairo2 headers"},
	"pillow":          {pkgs: []string{"libjpeg-dev", "zlib1g-dev"}, reason: "Pillow image processing"},
	"lxml":            {pkgs: []string{"libxml2-dev", "libxslt-dev"}, reason: "lxml needs libxml2/libxslt headers"},
	"cryptography":    {pkgs: []string{"libssl-dev", "libffi-dev"}, reason: "cryptography needs OpenSSL + libffi headers"},
	"pycurl":          {pkgs: []string{"libcurl4-openssl-dev"}, reason: "pycurl needs libcurl headers"},
	"weasyprint":      {pkgs: []string{"libpango-1.0-0", "libpangoft2-1.0-0", "libharfbuzz0b"}, reason: "WeasyPrint needs Pango + HarfBuzz"},
}

// npmAptMap mirrors nixpacks node/mod.rs:144-173. The puppeteer entry pulls
// the full Chromium runtime stack — 12 apt packages — which is intentional;
// fewer breaks headless rendering at runtime.
var npmAptMap = map[string]aptEntry{
	"prisma":         {pkgs: []string{"openssl"}, reason: "Prisma engine needs OpenSSL for TLS"},
	"@prisma/client": {pkgs: []string{"openssl"}, reason: "Prisma client engine needs OpenSSL"},
	"sharp":          {pkgs: []string{"libvips-dev"}, reason: "Sharp uses libvips for image transforms"},
	"canvas":         {pkgs: []string{"libuuid1", "libgl1"}, reason: "node-canvas needs libuuid + libGL"},
	"puppeteer": {
		pkgs: []string{
			"libnss3", "libatk1.0-0", "libatk-bridge2.0-0", "libcups2",
			"libgbm1", "libasound2t64", "libpangocairo-1.0-0", "libxss1",
			"libgtk-3-0", "libxshmfence1", "libglu1-mesa", "chromium",
		},
		reason: "Puppeteer launches a full Chromium runtime (12 apt deps for headless rendering)",
	},
	"playwright": {
		pkgs: []string{
			"libnss3", "libatk1.0-0", "libatk-bridge2.0-0", "libcups2",
			"libgbm1", "libasound2t64", "libpangocairo-1.0-0", "libxss1",
			"libgtk-3-0", "libxshmfence1",
		},
		reason: "Playwright drives Chromium/Firefox/WebKit and shares puppeteer's apt stack",
	},
}

// aptEntry is one row of the reverse map.
type aptEntry struct {
	pkgs   []string
	reason string
}

// InferAptPackages walks the prod-flagged dep list of every supported
// ecosystem and returns the deduped apt package list. Each apt entry retains
// the first reason that triggered it; multiple deps that map to the same apt
// package are collapsed.
//
// Always-bundled compile baseline (`gcc` for python projects) is added by the
// caller, not here — this function only emits packages with a concrete cause.
func InferAptPackages(pkgs map[string][]types.Package) []types.InferredAptPackage {
	if len(pkgs) == 0 {
		return nil
	}

	seen := map[string]string{} // apt_pkg -> reason
	maps := []struct {
		ecosystem string
		table     map[string]aptEntry
	}{
		{"pip", pythonAptMap},
		{"npm", npmAptMap},
	}

	for _, m := range maps {
		for _, p := range pkgs[m.ecosystem] {
			if !p.IsProd {
				continue
			}
			key := normalizeDepName(m.ecosystem, p.Name)
			entry, ok := m.table[key]
			if !ok {
				continue
			}
			for _, apt := range entry.pkgs {
				if _, exists := seen[apt]; !exists {
					seen[apt] = entry.reason
				}
			}
		}
	}

	out := make([]types.InferredAptPackage, 0, len(seen))
	for apt, reason := range seen {
		out = append(out, types.InferredAptPackage{Name: apt, Reason: reason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
