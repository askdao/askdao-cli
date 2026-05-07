package scanner

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestScanPackages_FakeRunner(t *testing.T) {
	canned := []byte(`{
      "artifacts": [
        {"name": "fastapi", "version": "0.135.1", "type": "python"},
        {"name": "sqlalchemy", "version": "2.0.48", "type": "python"},
        {"name": "actions/checkout", "version": "v4", "type": "github-action"},
        {"name": "next", "version": "15.0.0", "type": "npm"},
        {"name": "weird-thing", "version": "1.0", "type": ""}
      ]
    }`)

	var capturedArgs []string
	fake := func(_ context.Context, args []string) ([]byte, error) {
		capturedArgs = args
		return canned, nil
	}

	got, err := ScanPackages(context.Background(), "/tmp/x", SyftOptions{
		Excludes: []string{"./extra/**"},
		Runner:   fake,
	})
	if err != nil {
		t.Fatalf("ScanPackages: %v", err)
	}

	wantPip := []types.Package{
		{Name: "fastapi", Version: "0.135.1", IsProd: true},
		{Name: "sqlalchemy", Version: "2.0.48", IsProd: true},
	}
	if !reflect.DeepEqual(got["pip"], wantPip) {
		t.Errorf("pip ecosystem = %+v, want %+v", got["pip"], wantPip)
	}
	if len(got["npm"]) != 1 || got["npm"][0].Name != "next" {
		t.Errorf("npm ecosystem = %+v", got["npm"])
	}
	if len(got["github_actions"]) != 1 {
		t.Errorf("github_actions ecosystem = %+v", got["github_actions"])
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty-type artifact should be dropped")
	}

	// Verify default exclude flowed into argv plus user-supplied one.
	joined := strings.Join(capturedArgs, " ")
	if !strings.Contains(joined, "dir:/tmp/x") {
		t.Errorf("missing dir target: %q", joined)
	}
	if !strings.Contains(joined, "--exclude ./openviking/**") {
		t.Errorf("default exclude missing: %q", joined)
	}
	if !strings.Contains(joined, "--exclude ./extra/**") {
		t.Errorf("user exclude missing: %q", joined)
	}
	if !strings.Contains(joined, "-o syft-json") {
		t.Errorf("output format missing: %q", joined)
	}
}

func TestScanPackages_RunnerError(t *testing.T) {
	fake := func(_ context.Context, _ []string) ([]byte, error) {
		return nil, errBoom
	}
	_, err := ScanPackages(context.Background(), "/tmp", SyftOptions{Runner: fake})
	if err == nil {
		t.Fatal("expected error from runner to propagate")
	}
}

func TestScanPackages_BadJSON(t *testing.T) {
	fake := func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("not-json"), nil
	}
	_, err := ScanPackages(context.Background(), "/tmp", SyftOptions{Runner: fake})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// TestScanPackages_Integration runs the real syft binary against this repo if
// installed. Skipped on machines that don't have syft on PATH.
func TestScanPackages_Integration(t *testing.T) {
	if _, err := exec.LookPath("syft"); err != nil {
		t.Skip("syft not installed; skipping integration test")
	}
	got, err := ScanPackages(context.Background(), "..", SyftOptions{})
	if err != nil {
		t.Fatalf("syft integration: %v", err)
	}
	if len(got) == 0 {
		t.Errorf("expected at least one ecosystem, got 0")
	}
}

var errBoom = &boomErr{}

type boomErr struct{}

func (*boomErr) Error() string { return "boom" }
