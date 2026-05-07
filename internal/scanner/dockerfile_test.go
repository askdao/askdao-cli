package scanner

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestParseDockerfile_MultiStage(t *testing.T) {
	df, err := ParseDockerfile(filepath.Join("testdata", "multi_stage.Dockerfile"))
	if err != nil {
		t.Fatalf("ParseDockerfile: %v", err)
	}
	if !df.Exists {
		t.Fatal("expected Exists=true")
	}
	if got := len(df.Stages); got != 2 {
		t.Fatalf("expected 2 stages, got %d", got)
	}

	// Stage 1: builder.
	s1 := df.Stages[0]
	if s1.From != "node:20" {
		t.Errorf("stage[0].From = %q, want node:20", s1.From)
	}
	if s1.As == nil || *s1.As != "builder" {
		t.Errorf("stage[0].As mismatch: %+v", s1.As)
	}

	// Stage 2: runner — final stage drives BaseImage / FinalStageName.
	if df.BaseImage != "python:3.12-slim" {
		t.Errorf("BaseImage = %q, want python:3.12-slim", df.BaseImage)
	}
	if df.FinalStageName != nil {
		t.Errorf("FinalStageName should be nil for unnamed final stage, got %v", *df.FinalStageName)
	}

	// WORKDIR / ENV / EXPOSE / USER / CMD.
	if df.Workdir != "/app" {
		t.Errorf("Workdir = %q", df.Workdir)
	}
	if df.EnvVars["PYTHONUNBUFFERED"] != "1" {
		t.Errorf("EnvVars = %v", df.EnvVars)
	}
	if !reflect.DeepEqual(df.ExposedPorts, []int{8000}) {
		t.Errorf("ExposedPorts = %v", df.ExposedPorts)
	}
	if len(df.Users) != 1 || df.Users[0].Name != "appuser" {
		t.Errorf("Users = %+v", df.Users)
	}
	wantCmd := []string{"uvicorn", "app.main:app", "--host", "0.0.0.0"}
	if !reflect.DeepEqual(df.Cmd, wantCmd) {
		t.Errorf("Cmd = %v, want %v", df.Cmd, wantCmd)
	}
	if !reflect.DeepEqual(df.BuildArgs, []string{"BUILD_REV"}) {
		t.Errorf("BuildArgs = %v", df.BuildArgs)
	}

	// RUN extraction.
	wantApt := []string{"libpq-dev", "gcc"}
	if !reflect.DeepEqual(df.ExtractedAptPackages, wantApt) {
		t.Errorf("ExtractedAptPackages = %v, want %v", df.ExtractedAptPackages, wantApt)
	}
	wantPip := []string{"fastapi==0.135.1", "sqlalchemy==2.0.48"}
	if !reflect.DeepEqual(df.ExtractedPipPackages, wantPip) {
		t.Errorf("ExtractedPipPackages = %v, want %v", df.ExtractedPipPackages, wantPip)
	}
	// extracted_setup_commands: git clone survives, npm ci/build survives,
	// apt-get update / rm -rf /var/lib/apt are filtered as housekeeping.
	if !containsSubstr(df.ExtractedSetupCommands, "git clone") {
		t.Errorf("ExtractedSetupCommands missing git clone: %v", df.ExtractedSetupCommands)
	}
	if !containsSubstr(df.ExtractedSetupCommands, "npm ci") {
		t.Errorf("ExtractedSetupCommands missing npm ci: %v", df.ExtractedSetupCommands)
	}
	for _, c := range df.ExtractedSetupCommands {
		if c == "apt-get update" || c == "rm -rf /var/lib/apt/lists/*" {
			t.Errorf("housekeeping leaked into setup_commands: %q", c)
		}
	}

	// Anthropic warnings: multi-stage + USER + EXPOSE present, no entrypoint.
	got := warningFields(df.AnthropicCompatibleWarnings)
	want := map[string]bool{"stages": true, "users": true, "exposed_ports": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("warnings = %v, want %v", got, want)
	}
}

func TestParseDockerfile_SingleStage(t *testing.T) {
	df, err := ParseDockerfile(filepath.Join("testdata", "single_stage.Dockerfile"))
	if err != nil {
		t.Fatalf("ParseDockerfile: %v", err)
	}
	if len(df.Stages) != 1 {
		t.Fatalf("want 1 stage, got %d", len(df.Stages))
	}
	if df.BaseImage != "python:3.12-slim" {
		t.Errorf("BaseImage = %q", df.BaseImage)
	}
	// ENV space-separated form: ENV LOG_LEVEL info.
	if df.EnvVars["LOG_LEVEL"] != "info" {
		t.Errorf("EnvVars = %v", df.EnvVars)
	}
	if !reflect.DeepEqual(df.ExtractedPipPackages, []string{"fastapi"}) {
		t.Errorf("ExtractedPipPackages = %v", df.ExtractedPipPackages)
	}
	// Single stage → no multi-stage warning.
	for _, w := range df.AnthropicCompatibleWarnings {
		if w.Field == "stages" {
			t.Errorf("unexpected multi-stage warning on single-stage Dockerfile")
		}
	}
}

func TestParseDockerfile_Missing(t *testing.T) {
	df, err := ParseDockerfile(filepath.Join("testdata", "does-not-exist.Dockerfile"))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if df == nil || df.Exists {
		t.Errorf("missing file → expected Exists=false, got %+v", df)
	}
}

func containsSubstr(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}

func warningFields(ws []types.DockerCompatWarning) map[string]bool {
	m := map[string]bool{}
	for _, w := range ws {
		m[w.Field] = true
	}
	return m
}
