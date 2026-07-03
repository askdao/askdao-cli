package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/askdao/askdao-cli/internal/deploy"
)

func TestDeploy_RefusesWithoutToken(t *testing.T) {
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")
	t.Setenv("ASKDAO_CONDUCTOR_URL", "http://localhost:1")
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "")
	// Belt-and-suspenders: isolate credentials.json so the "URL-set / no-token"
	// branch in resolveServerAndToken is what fires, not the credentials
	// fallback path.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "test-agent"})
	got := out()
	if code == 0 {
		t.Errorf("deploy should refuse with no conductor token configured, got 0")
	}
	if !strings.Contains(got, "ASKDAO_CONDUCTOR_TOKEN") {
		t.Errorf("expected token hint in output, got:\n%s", got)
	}
}

func TestDeploy_EndToEnd_HappyPath(t *testing.T) {
	root := withWorkdir(t)
	writeAgentDirWithSkill(t, root, "kol-agent", "my-skill")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cli/deploy" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		if !strings.Contains(r.FormValue("agent_yaml"), "apiVersion: askdao.ai/v1") {
			t.Errorf("agent_yaml field = %q", r.FormValue("agent_yaml"))
		}
		if got := r.FormValue("harness_id"); got != "anthropic_managed_agents" {
			t.Errorf("harness_id = %q", got)
		}
		fhs := r.MultipartForm.File["my-skill"]
		if len(fhs) != 1 {
			t.Errorf("skill file part 'my-skill': got %d, want 1", len(fhs))
			return
		}
		f, _ := fhs[0].Open()
		zb, _ := io.ReadAll(f)
		_ = f.Close()
		zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
		if err != nil {
			t.Errorf("skill part is not a valid zip: %v", err)
			return
		}
		found := false
		for _, zf := range zr.File {
			if zf.Name == "my-skill/SKILL.md" {
				found = true
			}
		}
		if !found {
			t.Errorf("skill zip missing my-skill/SKILL.md")
		}
		writeJSON(w, http.StatusOK, deploy.DeployResponse{
			AgentID:                "agt_abc",
			AnthropicAgentID:       "agent_x",
			AnthropicEnvironmentID: "env_x",
			GroupID:                "grp_def",
			GroupLink:              "https://askdao.ai/k/usr_1/g/grp_def",
			Skills: []map[string]interface{}{{
				"type": "custom_local", "path": "my-skill",
				"anthropic_skill_id": "skill_xyz", "anthropic_skill_version": "1",
			}},
			TranslationReport: deploy.TranslationReport{Harness: "anthropic_managed_agents"},
			Created:           true,
		})
	}))
	defer srv.Close()
	t.Setenv("ASKDAO_CONDUCTOR_URL", srv.URL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "kol-agent"})
	got := out()
	if code != 0 {
		t.Fatalf("deploy exit = %d\n--- output ---\n%s", code, got)
	}
	for _, want := range []string{"Created new agent.", "agt_abc", "grp_def", "https://askdao.ai/k/usr_1/g/grp_def", "my-skill", "skill_xyz"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestDeploy_EndToEnd_UpdateMode(t *testing.T) {
	// Update-mode: the server signals an in-place agent update via
	// `created: false` + previous_managed_version. The cli should print the
	// "Updated existing agent (vN → vN+1)" banner instead of "Created".
	root := withWorkdir(t)
	writeAgentDirWithSkill(t, root, "kol-agent", "my-skill")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		prev := 1
		writeJSON(w, http.StatusOK, deploy.DeployResponse{
			AgentID:                "agt_abc",
			AnthropicAgentID:       "agent_x",
			AnthropicEnvironmentID: "env_x",
			GroupID:                "grp_def",
			GroupLink:              "https://askdao.ai/k/usr_1/g/grp_def",
			TranslationReport:      deploy.TranslationReport{Harness: "anthropic_managed_agents"},
			Created:                false,
			PreviousManagedVersion: &prev,
		})
	}))
	defer srv.Close()
	t.Setenv("ASKDAO_CONDUCTOR_URL", srv.URL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "kol-agent"})
	got := out()
	if code != 0 {
		t.Fatalf("deploy exit = %d\n--- output ---\n%s", code, got)
	}
	for _, want := range []string{"Updated existing agent (v1 → v2).", "agt_abc", "grp_def"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestDeploy_KolProfileRequired_GuidesToWeb(t *testing.T) {
	// D 改造：KOL profile 归 askdao.ai 云端，不再在本地 CLI prompt bio / 自动 PATCH。
	// 遇 409 kol_profile_required → 引导去 askdao.ai/dashboard/subscription + exit 1，不 retry，
	// 不调 /kol-profile（对齐 edit.go 的 studioDeployError）。
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")

	var deployCalls, patchCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/cli/deploy":
			atomic.AddInt32(&deployCalls, 1)
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"detail": map[string]interface{}{
					"reason": "kol_profile_required",
					"fields": []string{"kol_join_mode", "kol_bio"},
					"hint":   "PATCH /api/v1/users/me/kol-profile with kol_join_mode='free'",
				},
			})
		case "/api/v1/users/me/kol-profile":
			atomic.AddInt32(&patchCalls, 1)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("ASKDAO_CONDUCTOR_URL", srv.URL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "test-agent"})
	got := out()
	if code != 1 {
		t.Fatalf("deploy exit = %d, want 1\n--- output ---\n%s", code, got)
	}
	if deployCalls != 1 {
		t.Errorf("deploy called %d times, want 1 (no retry)", deployCalls)
	}
	if patchCalls != 0 {
		t.Errorf("kol-profile PATCH called %d times, want 0 (setup delegated to web)", patchCalls)
	}
	if !strings.Contains(got, "askdao.ai/dashboard/subscription") {
		t.Errorf("output should guide to askdao.ai/dashboard/subscription (the page that sets kol_join_mode)\n--- output ---\n%s", got)
	}
}

func TestDeploy_BlockingWarnings_NoForce(t *testing.T) {
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"detail": map[string]interface{}{
				"reason": "translation_report contains REJECTED (deploy-fatal) warnings; set force=true to deploy anyway",
				"translation_report": map[string]interface{}{
					"harness": "anthropic_managed_agents",
					"translation_warnings": []map[string]interface{}{
						{"field": "custom_tools[0].name", "severity": "high", "action": "rejected", "reason": "custom tool name is invalid"},
					},
				},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("ASKDAO_CONDUCTOR_URL", srv.URL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "test-agent"})
	got := out()
	if code == 0 {
		t.Fatalf("deploy with blocking warnings and no --force should fail, got 0\n--- output ---\n%s", got)
	}
	for _, want := range []string{"HIGH", "custom_tools[0].name", "--force"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestDeploy_Force_OverridesBlockingWarnings(t *testing.T) {
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		if r.FormValue("force") != "true" {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"detail": map[string]interface{}{
					"reason":             "translation_report contains REJECTED (deploy-fatal) warnings; set force=true to deploy anyway",
					"translation_report": map[string]interface{}{"harness": "anthropic_managed_agents", "translation_warnings": []map[string]interface{}{{"field": "custom_tools[0].name", "severity": "high", "action": "rejected", "reason": "x"}}},
				},
			})
			return
		}
		writeJSON(w, http.StatusOK, deploy.DeployResponse{
			AgentID: "agt_forced", AnthropicAgentID: "agent_z", AnthropicEnvironmentID: "env_z",
			GroupID: "grp_z", GroupLink: "https://askdao.ai/k/usr_3/g/grp_z",
			TranslationReport: deploy.TranslationReport{
				Harness:             "anthropic_managed_agents",
				TranslationWarnings: []deploy.TranslationWarning{{Field: "custom_tools[0].name", Severity: "high", Action: "rejected", Reason: "x"}},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("ASKDAO_CONDUCTOR_URL", srv.URL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "test-agent", "--force"})
	got := out()
	if code != 0 {
		t.Fatalf("deploy --force should succeed, got %d\n--- output ---\n%s", code, got)
	}
	if !strings.Contains(got, "agt_forced") {
		t.Errorf("output missing agt_forced\n--- output ---\n%s", got)
	}
}

func TestDeploy_VisibilityDowngrade_RefusedWithoutConfirm(t *testing.T) {
	// 降级确认闸：conductor 对「approved 公开 agent 显式降 private」返 409
	// visibility_downgrade_requires_confirm。go test 下 stdin 非 TTY →
	// 拒绝降级 + 指引 --confirm-downgrade + exit 1，不静默重试。
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")

	var deployCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&deployCalls, 1)
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"detail": map[string]interface{}{
				"reason":               "visibility_downgrade_requires_confirm",
				"current_visibility":   "public",
				"requested_visibility": "private",
				"agent_name":           "storyteller",
			},
		})
	}))
	defer srv.Close()
	t.Setenv("ASKDAO_CONDUCTOR_URL", srv.URL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "test-agent"})
	got := out()
	if code != 1 {
		t.Fatalf("deploy exit = %d, want 1\n--- output ---\n%s", code, got)
	}
	if deployCalls != 1 {
		t.Errorf("deploy called %d times, want 1 (no unconfirmed retry)", deployCalls)
	}
	for _, want := range []string{"storyteller", "public", "--confirm-downgrade", "review"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestDeploy_ConfirmDowngradeFlag_Passes(t *testing.T) {
	// --confirm-downgrade 直接在首个请求带 confirm_visibility_downgrade=true
	//（CI / 非交互场景的确认通道）。
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		if r.FormValue("confirm_visibility_downgrade") != "true" {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"detail": map[string]interface{}{
					"reason":               "visibility_downgrade_requires_confirm",
					"current_visibility":   "public",
					"requested_visibility": "private",
					"agent_name":           "storyteller",
				},
			})
			return
		}
		writeJSON(w, http.StatusOK, deploy.DeployResponse{
			AgentID: "agt_downgraded", AnthropicAgentID: "agent_d", AnthropicEnvironmentID: "env_d",
			GroupID: "grp_d",
			TranslationReport: deploy.TranslationReport{Harness: "anthropic_managed_agents"},
		})
	}))
	defer srv.Close()
	t.Setenv("ASKDAO_CONDUCTOR_URL", srv.URL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "test-agent", "--confirm-downgrade"})
	got := out()
	if code != 0 {
		t.Fatalf("deploy --confirm-downgrade should succeed, got %d\n--- output ---\n%s", code, got)
	}
	if !strings.Contains(got, "agt_downgraded") {
		t.Errorf("output missing agt_downgraded\n--- output ---\n%s", got)
	}
}

func TestDeploy_MissingSkillDir(t *testing.T) {
	root := withWorkdir(t)
	// askdao-agent.yml references a custom_local skill whose directory does
	// not exist on disk.
	dir := filepath.Join(root, "broken-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "askdao-agent.yml"), []byte(minimalAgentYAML("broken-agent", "  - {type: custom_local, path: .agents/skills/ghost}\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASKDAO_CONDUCTOR_URL", "http://localhost:1")
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	outStdout, restoreOut := captureStdout(t)
	defer restoreOut()
	errOut, restoreErr := captureStderr(t)
	defer restoreErr()
	code := runDeploy(context.Background(), []string{"--dir", "broken-agent"})
	_ = outStdout()
	gotErr := errOut()
	if code != 1 {
		t.Fatalf("deploy with a missing skill dir should exit 1, got %d", code)
	}
	if !strings.Contains(gotErr, "ghost") || !strings.Contains(gotErr, "directory not found") {
		t.Errorf("stderr should explain the missing skill dir, got:\n%s", gotErr)
	}
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// minimalAgentYAML returns a valid agent.yml document. skillsBlock, if non-empty,
// is appended under `skills:` (otherwise `skills: []`).
func minimalAgentYAML(name, skillsBlock string) string {
	skills := "skills: []\n"
	if skillsBlock != "" {
		skills = "skills:\n" + skillsBlock
	}
	return "apiVersion: askdao.ai/v1\n" +
		"kind: AgentSpec\n" +
		"metadata:\n  name: " + name + "\n  version: 0.1.0\n  visibility: private\n" +
		"persona:\n  model_class: balanced\n  model_preferences:\n    - {provider: anthropic, id: claude-sonnet-4-6}\n  system_prompt: \"test agent\"\n" +
		"capabilities:\n  shell: {enabled: true, permission: allow}\n  filesystem: {enabled: true, permission: allow}\n  web: {enabled: true, permission: allow}\n  code_execution: {enabled: true, permission: allow}\n" +
		"mcp_servers: []\ncustom_tools: []\n" + skills +
		"workspace:\n  networking: {mode: limited, allow_mcp_servers: true, allow_package_managers: false}\n" +
		"vault_hints: {}\npreferred_harness: anthropic_managed_agents\n"
}

func writeAgentDirWithSkill(t *testing.T, root, name, skillName string) {
	t.Helper()
	// v0.7 layout: project root is <root>/<name>/; skill lives at
	// .agents/skills/<skillName>/; askdao-agent.yml's `path` references it
	// relative to the project root. No persona.md (system_prompt is in yaml).
	dir := filepath.Join(root, name)
	skillRel := ".agents/skills/" + skillName
	skillDir := filepath.Join(dir, ".agents", "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "askdao-agent.yml"), []byte(minimalAgentYAML(name, "  - {type: custom_local, path: "+skillRel+"}\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: "+skillName+"\ndescription: a test skill\n---\nDo a thing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureStderr mirrors captureStdout for os.Stderr. The returned getter may be
// called once (a second call blocks); call it once and reuse the result.
func captureStderr(t *testing.T) (func() string, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, rerr := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if rerr != nil {
				done <- string(buf)
				return
			}
		}
	}()
	get := func() string {
		_ = w.Close()
		return <-done
	}
	restore := func() { os.Stderr = prev }
	return get, restore
}

func TestDeploy_KolProfileRequired_UsesServerSetupURL(t *testing.T) {
	// M4: conductor 下发 detail.setup_url 时优先渲染服务端地址（权威引导收敛
	// 到服务端，硬编码仅作老版本 conductor fallback —— 上一个 case 覆盖）。
	root := withWorkdir(t)
	writeMinimalAgent(t, root, "test-agent")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"detail": map[string]interface{}{
				"reason":    "kol_profile_required",
				"fields":    []string{"kol_join_mode"},
				"setup_url": "https://staging.askdao.ai/dashboard/subscription",
			},
		})
	}))
	defer srv.Close()
	t.Setenv("ASKDAO_CONDUCTOR_URL", srv.URL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "tok")

	out, restore := captureStdout(t)
	defer restore()
	code := runDeploy(context.Background(), []string{"--dir", "test-agent"})
	got := out()
	if code != 1 {
		t.Fatalf("deploy exit = %d, want 1\n--- output ---\n%s", code, got)
	}
	if !strings.Contains(got, "https://staging.askdao.ai/dashboard/subscription") {
		t.Errorf("output should use the server-handed setup_url\n--- output ---\n%s", got)
	}
}
