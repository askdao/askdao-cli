package providers

import (
	"path/filepath"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestPython_DetectAndPM(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pyproject.toml"), `[project]
name = "demo"
`)
	mustWriteFile(t, filepath.Join(root, "uv.lock"), "")
	app, _ := NewApp(root)
	env := NewEnv(nil)

	pp := &PythonProvider{}
	ok, err := pp.Detect(app, env)
	if err != nil || !ok {
		t.Fatalf("Detect = (%v, %v); want (true, nil)", ok, err)
	}
	if got := pp.SelectPackageManager(app, env); got != PMUv {
		t.Errorf("SelectPackageManager = %q, want uv", got)
	}
}

func TestPython_PackageManagerPriority(t *testing.T) {
	cases := []struct {
		files []string
		want  PythonPackageManager
	}{
		{[]string{"uv.lock", "pyproject.toml", "requirements.txt"}, PMUv},
		{[]string{"poetry.lock", "pyproject.toml"}, PMPoetry},
		{[]string{"pdm.lock", "pyproject.toml"}, PMPDM},
		{[]string{"requirements.txt"}, PMPip},
		{[]string{"Pipfile"}, PMPipenv},
		{[]string{"pyproject.toml"}, PMSetuptools},
		{[]string{"main.py"}, PMNone},
	}
	for _, tc := range cases {
		root := t.TempDir()
		for _, f := range tc.files {
			mustWriteFile(t, filepath.Join(root, f), "")
		}
		app, _ := NewApp(root)
		got := (&PythonProvider{}).SelectPackageManager(app, NewEnv(nil))
		if got != tc.want {
			t.Errorf("files=%v: got %q, want %q", tc.files, got, tc.want)
		}
	}
}

func TestPython_PackageManagerEnvOverride(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "uv.lock"), "")
	app, _ := NewApp(root)
	env := NewEnv(map[string]string{"PYTHON_PACKAGE_MANAGER": "poetry"})
	got := (&PythonProvider{}).SelectPackageManager(app, env)
	if got != PMPoetry {
		t.Errorf("env override should win: got %q", got)
	}
}

func TestPython_PlanFastAPI(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pyproject.toml"), "")
	mustWriteFile(t, filepath.Join(root, "app.py"), "from fastapi import FastAPI\n")

	pp := &PythonProvider{
		Pkgs: map[string][]types.Package{
			"pip": {
				{Name: "fastapi", IsProd: true},
				{Name: "uvicorn", IsProd: true},
				{Name: "asyncpg", IsProd: true},
				{Name: "sqlalchemy", IsProd: true},
				{Name: "alembic", IsProd: true},
				{Name: "anthropic", IsProd: true},
				{Name: "pytest", IsProd: false}, // dev — must not contribute apt deps
			},
		},
	}
	app, _ := NewApp(root)
	plan, err := pp.Plan(app, NewEnv(nil))
	if err != nil || plan == nil {
		t.Fatalf("Plan = (%v, %v)", plan, err)
	}

	// Frameworks: FastAPI + SQLAlchemy + Alembic.
	want := map[string]bool{"FastAPI": true, "SQLAlchemy": true, "Alembic": true}
	got := map[string]bool{}
	for _, fw := range plan.Frameworks {
		got[fw.Name] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("expected %s in plan.Frameworks: %+v", k, plan.Frameworks)
		}
	}

	// asyncpg → libpq-dev + gcc reverse-mapped.
	apt := plan.SystemPkgs["apt"]
	aptNames := map[string]bool{}
	for _, p := range apt {
		aptNames[p.Name] = true
	}
	if !aptNames["libpq-dev"] || !aptNames["gcc"] {
		t.Errorf("expected libpq-dev + gcc in apt list, got %+v", apt)
	}

	// External services: PostgreSQL + Anthropic.
	gotSvc := map[string]bool{}
	for _, s := range plan.ExternalSvc {
		gotSvc[s.Service] = true
	}
	if !gotSvc["PostgreSQL"] || !gotSvc["Anthropic API"] {
		t.Errorf("expected PostgreSQL + Anthropic API, got %+v", plan.ExternalSvc)
	}
}

func TestPython_PlanDjango(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "manage.py"), "import django\n")
	mustWriteFile(t, filepath.Join(root, "settings.py"), "DATABASES = {'default': {'ENGINE': 'django.db.backends.postgresql'}}\n")
	pp := &PythonProvider{
		Pkgs: map[string][]types.Package{
			"pip": {{Name: "django", IsProd: true}, {Name: "psycopg2-binary", IsProd: true}},
		},
	}
	app, _ := NewApp(root)
	plan, _ := pp.Plan(app, NewEnv(nil))
	if plan == nil {
		t.Fatal("plan is nil")
	}

	hasDjango := false
	for _, fw := range plan.Frameworks {
		if fw.Name == "Django" {
			hasDjango = true
		}
	}
	if !hasDjango {
		t.Errorf("Django not detected: %+v", plan.Frameworks)
	}

	hasPG := false
	for _, s := range plan.ExternalSvc {
		if s.Service == "PostgreSQL" {
			hasPG = true
		}
	}
	if !hasPG {
		t.Errorf("PostgreSQL not detected: %+v", plan.ExternalSvc)
	}
}

func TestPython_DetectFalseForEmptyProject(t *testing.T) {
	app, _ := NewApp(t.TempDir())
	ok, _ := (&PythonProvider{}).Detect(app, NewEnv(nil))
	if ok {
		t.Error("empty project should not detect as python")
	}
}
