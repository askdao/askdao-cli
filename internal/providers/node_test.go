package providers

import (
	"path/filepath"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestNode_DetectAndPM(t *testing.T) {
	cases := []struct {
		files []string
		want  NodePackageManager
	}{
		{[]string{"package.json", "pnpm-lock.yaml"}, PMPnpm},
		{[]string{"package.json", "yarn.lock"}, PMYarn},
		{[]string{"package.json", "bun.lockb"}, PMBun},
		{[]string{"package.json", "package-lock.json"}, PMNpm},
		{[]string{"package.json"}, PMNpm},
	}
	for _, tc := range cases {
		root := t.TempDir()
		for _, f := range tc.files {
			mustWriteFile(t, filepath.Join(root, f), `{}`)
		}
		app, _ := NewApp(root)
		ok, _ := (&NodeProvider{}).Detect(app, NewEnv(nil))
		if !ok {
			t.Errorf("files=%v: Detect should be true", tc.files)
			continue
		}
		got := (&NodeProvider{}).SelectPackageManager(app, NewEnv(nil))
		if got != tc.want {
			t.Errorf("files=%v: got %q, want %q", tc.files, got, tc.want)
		}
	}
}

func TestNode_PlanNextJS(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "package.json"), `{
      "dependencies": {"next": "^15", "react": "^19", "react-dom": "^19"}
    }`)
	mustWriteFile(t, filepath.Join(root, "next.config.js"), "module.exports = {}\n")

	np := &NodeProvider{
		Pkgs: map[string][]types.Package{
			"npm": {
				{Name: "next", IsProd: true},
				{Name: "react", IsProd: true},
				{Name: "react-dom", IsProd: true},
			},
		},
	}
	app, _ := NewApp(root)
	plan, err := np.Plan(app, NewEnv(nil))
	if err != nil || plan == nil {
		t.Fatalf("Plan = (%v, %v)", plan, err)
	}
	gotFW := map[string]bool{}
	for _, fw := range plan.Frameworks {
		gotFW[fw.Name] = true
	}
	if !gotFW["Next.js"] || !gotFW["React"] {
		t.Errorf("expected Next.js + React, got %+v", plan.Frameworks)
	}
	// No native libs → apt list should be empty (no puppeteer / sharp / canvas).
	if len(plan.SystemPkgs["apt"]) != 0 {
		t.Errorf("vanilla Next.js project should have no inferred apt: %+v", plan.SystemPkgs["apt"])
	}
}

func TestNode_PuppeteerPullsTwelveAptDeps(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "package.json"), `{"dependencies":{"puppeteer":"^22"}}`)

	np := &NodeProvider{
		Pkgs: map[string][]types.Package{
			"npm": {{Name: "puppeteer", IsProd: true}},
		},
	}
	app, _ := NewApp(root)
	plan, _ := np.Plan(app, NewEnv(nil))
	apt := plan.SystemPkgs["apt"]
	if len(apt) != 12 {
		t.Errorf("expected 12 apt deps for puppeteer, got %d (%+v)", len(apt), apt)
	}
	want := map[string]bool{"libnss3": true, "chromium": true, "libgbm1": true}
	for _, p := range apt {
		delete(want, p.Name)
	}
	if len(want) > 0 {
		t.Errorf("missing expected apt entries: %v", want)
	}
}

func TestNode_PackageJSONFallback(t *testing.T) {
	// When Pkgs is nil, hasNpmDep should still match via package.json declarations.
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "package.json"), `{"dependencies":{"express":"^4"}}`)

	np := &NodeProvider{Pkgs: nil}
	app, _ := NewApp(root)
	plan, _ := np.Plan(app, NewEnv(nil))
	gotFW := map[string]bool{}
	for _, fw := range plan.Frameworks {
		gotFW[fw.Name] = true
	}
	if !gotFW["Express"] {
		t.Errorf("Express should be detected from package.json fallback: %+v", plan.Frameworks)
	}
}

func TestNode_DetectFalseForPythonOnlyProject(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "main.py"), "")
	app, _ := NewApp(root)
	ok, _ := (&NodeProvider{}).Detect(app, NewEnv(nil))
	if ok {
		t.Error("python-only project should not detect as node")
	}
}
