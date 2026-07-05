// [INPUT]: 依赖 testing / os / path/filepath / archive/zip / net/http(test) + internal/deploy + internal/types
// [OUTPUT]: TestDeploy_UserScopeAbsolutePath — 全局 skill 绝对路径 e2e 回归（runDeploy 复用 internal/deploy.PackageSkills 正确打包工作台勾选的 user-scope skill）
// [POS]: cmd/askdao 的 deploy e2e 回归；skill 打包单元测试（resolveSkillDir 四分支 + frontmatter 校验）随打包逻辑提取到 internal/deploy/skills_test.go。
//
//	旧内联循环此处会因 filepath.Join(dir, absPath) 拼错路径报 "directory not found"，本 e2e 守住修复。
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
)

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
