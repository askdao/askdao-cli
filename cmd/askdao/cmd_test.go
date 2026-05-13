package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/types"
)

// withWorkdir cd's into a temp dir for the duration of a test, then restores
// the previous working directory. Commands like `agent show` and
// `agent deploy` resolve files relative to cwd, so they need a controlled root.
func withWorkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return dir
}

func writeMinimalAgent(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	askdao := filepath.Join(dir, ".askdao")
	if err := os.MkdirAll(askdao, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := types.AgentSpec{
		APIVersion: types.AgentSpecAPIVersion,
		Kind:       types.AgentSpecKind,
		Metadata: types.Metadata{
			Name:        name,
			Version:     "0.1.0",
			Visibility:  "private",
			PersonaFile: "persona.md",
		},
		Persona: types.Persona{
			ModelClass: "balanced",
			ModelPreferences: []types.ModelPreference{
				{Provider: "anthropic", ID: "claude-sonnet-4-6"},
			},
			SystemPrompt: "You are a test agent.",
		},
		Capabilities: types.Capabilities{
			Shell:         types.Capability{Enabled: true, Permission: "allow"},
			Filesystem:    types.Capability{Enabled: true, Permission: "allow"},
			Web:           types.Capability{Enabled: true, Permission: "allow"},
			CodeExecution: types.Capability{Enabled: true, Permission: "allow"},
		},
		PreferredHarness: "anthropic_managed_agents",
	}
	data, _ := yaml.Marshal(&spec)
	if err := os.WriteFile(filepath.Join(dir, "agent.yml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Recommendation snapshot identical to agent.yml — deploy diff is empty.
	if err := os.WriteFile(filepath.Join(askdao, "recommendation.yml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "persona.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect_SmokeOnTempProject(t *testing.T) {
	root := withWorkdir(t)
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, restore := captureStdout(t)
	defer restore()
	if code := runDetect(context.Background(), []string{"--summary"}); code != 0 {
		t.Fatalf("runDetect exit = %d", code)
	}
	got := out()
	if !strings.Contains(got, "Languages:") || !strings.Contains(got, "Python") {
		t.Errorf("detect summary missing expected fields:\n%s", got)
	}
}

func TestShow_DefaultRendersMidDensityCard(t *testing.T) {
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")

	out, restore := captureStdout(t)
	defer restore()
	if code := runShow(context.Background(), []string{"test-agent"}); code != 0 {
		t.Fatalf("runShow exit = %d", code)
	}
	got := out()
	for _, want := range []string{"PERSONA", "test-agent", "CAPABILITIES", "RUNTIME", "SUBSCRIBER ONBOARDING"} {
		if !strings.Contains(got, want) {
			t.Errorf("show output missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestShow_FullPipesEntireYaml(t *testing.T) {
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")

	out, restore := captureStdout(t)
	defer restore()
	if code := runShow(context.Background(), []string{"test-agent", "--full"}); code != 0 {
		t.Fatalf("runShow --full exit = %d", code)
	}
	if !strings.Contains(out(), "apiVersion: askdao.ai/v1") {
		t.Errorf("--full should pipe the raw yaml, got: %s", out())
	}
}

func TestShow_MissingAgentDirReturnsError(t *testing.T) {
	withWorkdir(t)
	if code := runShow(context.Background(), []string{"does-not-exist"}); code == 0 {
		t.Errorf("show should fail when agent dir is missing, got 0")
	}
}

func TestDeploy_RefusesWithoutConductorURL(t *testing.T) {
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")
	t.Setenv("ASKDAO_CONDUCTOR_URL", "")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "test-agent"})
	if code == 0 {
		t.Errorf("deploy should refuse with no conductor URL configured, got 0")
	}
	if !strings.Contains(out(), "ASKDAO_CONDUCTOR_URL") {
		t.Errorf("expected setup hint in output, got:\n%s", out())
	}
}

func TestDeploy_WithEditedSpecShowsDiff(t *testing.T) {
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")
	// Stop at the diff preview: leave the conductor URL unset so the run can't
	// reach a real endpoint even if the dev's shell has one configured.
	t.Setenv("ASKDAO_CONDUCTOR_URL", "")

	// Edit agent.yml — change the model id.
	editedSpec := types.AgentSpec{}
	original, err := os.ReadFile(filepath.Join(root, "test-agent", "agent.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(original, &editedSpec); err != nil {
		t.Fatal(err)
	}
	editedSpec.Persona.ModelPreferences[0].ID = "claude-haiku-4-5"
	editedYAML, _ := yaml.Marshal(&editedSpec)
	if err := os.WriteFile(filepath.Join(root, "test-agent", "agent.yml"), editedYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	out, restore := captureStdout(t)
	defer restore()
	_ = runDeploy(context.Background(), []string{"--dir", "test-agent"})
	got := out()
	if !strings.Contains(got, "claude-sonnet-4-6") || !strings.Contains(got, "claude-haiku-4-5") {
		t.Errorf("deploy diff should include before+after, got:\n%s", got)
	}
}

// captureStdout temporarily redirects os.Stdout to a pipe and returns a
// reader function that drains everything written so far.
func captureStdout(t *testing.T) (func() string, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				done <- string(buf)
				return
			}
		}
	}()

	get := func() string {
		_ = w.Close()
		s := <-done
		// Reopen for any subsequent writes within the same test.
		r2, w2, _ := os.Pipe()
		os.Stdout = w2
		// Drain the new pipe in a goroutine so it doesn't block the program.
		go func() { _, _ = os.ReadFile(r2.Name()) }()
		_ = w2.Close()
		return s
	}
	restore := func() { os.Stdout = prev }
	return get, restore
}
