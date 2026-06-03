// [INPUT]: 依赖 testing / os / path/filepath / archive/zip / net/http(test) + internal/deploy + internal/types
// [OUTPUT]: TestResolveSkillDir（四分支路径解析单测）+ TestDeploy_UserScopeAbsolutePath（全局 skill 绝对路径 e2e 回归）
// [POS]: cmd/askdao 的 skill 打包真相源测试；锁住 resolveSkillDir 的 ~ / 绝对 / Scope=="user" / project 相对四分支，
//
//	并回归 runDeploy 复用 packageSkills 后能正确打包工作台勾选的全局（user-scope）skill —— 旧内联循环此处会
//	因 filepath.Join(dir, absPath) 拼错路径报 "directory not found"。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/types"
)

// TestResolveSkillDir locks the four path-resolution branches shared by the CLI
// (`agent deploy`) and the web studio (`agent edit` one-stop deploy). The web
// studio writes global (user-scope) skills into the yaml with an ABSOLUTE path
// (api.go derives it from filepath.Dir of an absolute Source), so the absolute
// + Scope=="user" branches are the ones that actually fire in production — and
// the ones the old inline deploy loop silently mishandled.
func TestResolveSkillDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	const dir = "/proj"
	abs := filepath.Join(string(filepath.Separator), "abs", "skills", "foo")

	cases := []struct {
		name  string
		skill types.Skill
		want  string
	}{
		{
			name:  "tilde expands to home regardless of scope",
			skill: types.Skill{Path: "~/.claude/skills/foo"},
			want:  filepath.Join(home, ".claude", "skills", "foo"),
		},
		{
			name:  "absolute path returned verbatim (the studio global-skill case)",
			skill: types.Skill{Path: abs, Scope: "user"},
			want:  abs,
		},
		{
			name:  "relative path with user scope kept as-is (documented CWD-relative)",
			skill: types.Skill{Path: "bar", Scope: "user"},
			want:  filepath.FromSlash("bar"),
		},
		{
			name:  "project-relative path joins dir",
			skill: types.Skill{Path: ".agents/skills/foo", Scope: "project"},
			want:  filepath.Join(dir, ".agents", "skills", "foo"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSkillDir(dir, tc.skill)
			if err != nil {
				t.Fatalf("resolveSkillDir: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveSkillDir(%q, %+v) = %q, want %q", dir, tc.skill, got, tc.want)
			}
		})
	}
}

// TestDeploy_UserScopeAbsolutePath is the regression for the CLI/studio packaging
// divergence: a yaml referencing a global skill by ABSOLUTE path + scope=user
// (exactly what the web studio writes) must package correctly through `askdao
// agent deploy`. The old inline loop did filepath.Join(dir, absPath) → mangled
// path → "directory not found"; routing through packageSkills fixes it.
func TestDeploy_UserScopeAbsolutePath(t *testing.T) {
	root := withWorkdir(t)

	// Global skill lives OUTSIDE the project dir, referenced by absolute path.
	globalSkillDir := filepath.Join(t.TempDir(), "global-skill")
	if err := os.MkdirAll(globalSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalSkillDir, "SKILL.md"),
		[]byte("---\nname: global-skill\ndescription: a global test skill\n---\nDo a global thing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	projDir := filepath.Join(root, "kol-agent")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillsBlock := "  - {type: custom_local, path: " + globalSkillDir + ", scope: user}\n"
	if err := os.WriteFile(filepath.Join(projDir, "askdao-agent.yml"),
		[]byte(minimalAgentYAML("kol-agent", skillsBlock)), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotSkillZip bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		fhs := r.MultipartForm.File["global-skill"]
		if len(fhs) != 1 {
			t.Errorf("skill file part 'global-skill': got %d, want 1", len(fhs))
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
		for _, zf := range zr.File {
			if zf.Name == "global-skill/SKILL.md" {
				gotSkillZip = true
			}
		}
		writeJSON(w, http.StatusOK, deploy.DeployResponse{
			AgentID:                "agt_g",
			AnthropicAgentID:       "agent_g",
			AnthropicEnvironmentID: "env_g",
			TranslationReport:      deploy.TranslationReport{Harness: "anthropic_managed_agents"},
			Created:                true,
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
		t.Fatalf("deploy with a user-scope absolute skill path should exit 0, got %d\n--- output ---\n%s", code, got)
	}
	if !gotSkillZip {
		t.Errorf("conductor never received the global-skill zip (the inline-loop bug)")
	}
	if !strings.Contains(got, "global-skill") {
		t.Errorf("output should mention the packaged global-skill\n--- output ---\n%s", got)
	}
}
