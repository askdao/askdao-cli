package providers

import (
	"path/filepath"
	"testing"
)

func TestGo_DetectAndRuntime(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.23.4\n")
	mustWriteFile(t, filepath.Join(root, "main.go"), "package main\nfunc main() {}\n")

	app, _ := NewApp(root)
	gp := &GoProvider{}

	ok, err := gp.Detect(app, NewEnv(nil))
	if err != nil || !ok {
		t.Fatalf("Detect = (%v, %v); want true", ok, err)
	}
	plan, err := gp.Plan(app, NewEnv(nil))
	if err != nil || plan == nil {
		t.Fatalf("Plan = (%v, %v)", plan, err)
	}
	if plan.Runtime.Kind != "go" || plan.Runtime.Version != "1.23.4" {
		t.Errorf("Runtime = %+v, want {go 1.23.4}", plan.Runtime)
	}
	// No cgo → no apt entries.
	if len(plan.SystemPkgs["apt"]) != 0 {
		t.Errorf("non-cgo project should have empty apt list, got %+v", plan.SystemPkgs["apt"])
	}
}

func TestGo_CgoPullsToolchain(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(root, "bridge.go"), `package main

/*
#include <stdio.h>
*/
import "C"

func main() {}
`)
	app, _ := NewApp(root)
	plan, _ := (&GoProvider{}).Plan(app, NewEnv(nil))
	apt := plan.SystemPkgs["apt"]
	gotNames := map[string]bool{}
	for _, p := range apt {
		gotNames[p.Name] = true
	}
	if !gotNames["gcc"] || !gotNames["pkg-config"] {
		t.Errorf("cgo should pull gcc + pkg-config, got %+v", apt)
	}
}

func TestGo_ExternalServices(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), `module example.com/demo

go 1.23

require (
	github.com/jackc/pgx/v5 v5.0.0
	github.com/redis/go-redis/v9 v9.0.0
	github.com/anthropics/anthropic-sdk-go v0.97.0
)
`)
	app, _ := NewApp(root)
	plan, _ := (&GoProvider{}).Plan(app, NewEnv(nil))
	got := map[string]bool{}
	for _, s := range plan.ExternalSvc {
		got[s.Service] = true
	}
	for _, want := range []string{"PostgreSQL", "Redis", "Anthropic API"} {
		if !got[want] {
			t.Errorf("missing %s: got %+v", want, plan.ExternalSvc)
		}
	}
}

func TestGo_Workspace(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.work"), "go 1.23\nuse ./svc\n")
	mustWriteFile(t, filepath.Join(root, "svc", "go.mod"), "module example.com/svc\ngo 1.23\n")
	app, _ := NewApp(root)
	gp := &GoProvider{}
	ok, _ := gp.Detect(app, NewEnv(nil))
	if !ok {
		t.Error("go.work alone should trigger Detect")
	}
	md, _ := gp.Metadata(app, NewEnv(nil))
	if md["workspace"] != "go.work" {
		t.Errorf("Metadata.workspace = %q", md["workspace"])
	}
}

func TestGo_DetectFalseForRustOnly(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Cargo.toml"), `[package]
name = "x"
`)
	app, _ := NewApp(root)
	ok, _ := (&GoProvider{}).Detect(app, NewEnv(nil))
	if ok {
		t.Error("rust-only project should not detect as go")
	}
}
