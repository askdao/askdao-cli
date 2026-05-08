package providers

import (
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestInferAptPackages_DedupesAcrossDeps(t *testing.T) {
	pkgs := map[string][]types.Package{
		"pip": {
			// Two distinct deps that both demand libpq-dev + gcc — output should
			// list each apt package once.
			{Name: "psycopg2-binary", IsProd: true},
			{Name: "asyncpg", IsProd: true},
		},
	}
	got := InferAptPackages(pkgs)
	count := map[string]int{}
	for _, p := range got {
		count[p.Name]++
	}
	if count["libpq-dev"] != 1 || count["gcc"] != 1 {
		t.Errorf("dedup failed: %+v", got)
	}
}

func TestInferAptPackages_SkipsDevDeps(t *testing.T) {
	pkgs := map[string][]types.Package{
		"pip": {
			{Name: "psycopg2", IsProd: false}, // dev only — must NOT trigger libpq-dev
		},
	}
	if got := InferAptPackages(pkgs); len(got) != 0 {
		t.Errorf("dev-only deps should produce no apt entries, got %+v", got)
	}
}

func TestInferAptPackages_PEP503Normalization(t *testing.T) {
	// Map keys are lowercase-dash; ensure case + underscore variants still match.
	pkgs := map[string][]types.Package{
		"pip": {
			{Name: "Psycopg2_Binary", IsProd: true},
		},
	}
	got := InferAptPackages(pkgs)
	if len(got) == 0 {
		t.Errorf("PEP 503 normalization broken: %+v", got)
	}
}

func TestInferAptPackages_NodeAndPipMix(t *testing.T) {
	pkgs := map[string][]types.Package{
		"pip": {{Name: "Pillow", IsProd: true}},
		"npm": {{Name: "sharp", IsProd: true}},
	}
	got := InferAptPackages(pkgs)
	names := map[string]bool{}
	for _, p := range got {
		names[p.Name] = true
	}
	if !names["libjpeg-dev"] || !names["libvips-dev"] {
		t.Errorf("expected libjpeg-dev (Pillow) + libvips-dev (sharp), got %+v", got)
	}
}

func TestInferAptPackages_Empty(t *testing.T) {
	if got := InferAptPackages(nil); got != nil {
		t.Errorf("nil input should return nil, got %+v", got)
	}
}
