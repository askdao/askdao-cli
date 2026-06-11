// [INPUT]: 依赖 mcp.go 的 runMCPSetup/upsertClaudeMCP/upsertCodexMCP；httptest 假 conductor
// [OUTPUT]: cases for mcp setup — 双 harness 写入 / 幂等 / 保留既有 JSON 键 /
//
//	malformed claude.json 拒绝 clobber / --print 零写入 / 503 友好报错
//
// [POS]: cmd/askdao 测试 — 守 `askdao mcp setup` 的本机配置写入行为
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeConductor returns a test server answering /api/v1/cli/mcp-credentials.
func fakeConductor(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cli/mcp-credentials" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setupHomeAndEnv(t *testing.T, serverURL string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows os.UserHomeDir
	t.Setenv("ASKDAO_CONDUCTOR_URL", serverURL)
	t.Setenv("ASKDAO_CONDUCTOR_TOKEN", "cli_test_token")
	t.Setenv("ASKDAO_MCP_TOKEN", "")
	return home
}

const credsBody = `{"gateway_url":"https://mcp.askdao.ai/mcp","token":"mcp_secret_1"}`

func TestMCPSetup_WritesClaudeAndCodex(t *testing.T) {
	srv := fakeConductor(t, http.StatusOK, credsBody)
	home := setupHomeAndEnv(t, srv.URL)

	// Pre-existing state: claude.json with unrelated keys, codex dir with config.
	mustWriteFile(t, filepath.Join(home, ".claude.json"), `{"theme":"dark","mcpServers":{"other":{"type":"stdio","command":"x"}}}`)
	mustWriteFile(t, filepath.Join(home, ".codex", "config.toml"), "model = \"gpt-5.1\"\n")

	if code := runMCPSetup(context.Background(), nil); code != 0 {
		t.Fatalf("exit code = %d", code)
	}

	// Claude: askdao-mcp upserted, unrelated keys + servers preserved.
	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("claude.json no longer valid JSON: %v", err)
	}
	if doc["theme"] != "dark" {
		t.Errorf("unrelated key clobbered: %+v", doc)
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Errorf("pre-existing server dropped: %+v", servers)
	}
	entry := servers["askdao-mcp"].(map[string]any)
	if entry["type"] != "http" || entry["url"] != "https://mcp.askdao.ai/mcp" {
		t.Errorf("askdao-mcp entry wrong: %+v", entry)
	}
	authz := entry["headers"].(map[string]any)["Authorization"]
	if authz != "Bearer mcp_secret_1" {
		t.Errorf("authorization header = %v", authz)
	}

	// Codex: block appended, original content intact.
	toml, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(toml)
	if !strings.Contains(s, "model = \"gpt-5.1\"") {
		t.Errorf("original codex config clobbered:\n%s", s)
	}
	if !strings.Contains(s, "[mcp_servers.askdao-mcp]") ||
		!strings.Contains(s, `url = "https://mcp.askdao.ai/mcp"`) ||
		!strings.Contains(s, `bearer_token_env_var = "ASKDAO_MCP_TOKEN"`) {
		t.Errorf("codex block missing:\n%s", s)
	}
}

func TestMCPSetup_Idempotent(t *testing.T) {
	srv := fakeConductor(t, http.StatusOK, credsBody)
	home := setupHomeAndEnv(t, srv.URL)
	mustWriteFile(t, filepath.Join(home, ".codex", "config.toml"), "")
	mustWriteFile(t, filepath.Join(home, ".claude.json"), "{}")

	for i := 0; i < 2; i++ {
		if code := runMCPSetup(context.Background(), nil); code != 0 {
			t.Fatalf("run %d exit code = %d", i, code)
		}
	}

	toml, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if n := strings.Count(string(toml), "[mcp_servers.askdao-mcp]"); n != 1 {
		t.Errorf("codex block appended %d times:\n%s", n, string(toml))
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("claude.json invalid after second run: %v", err)
	}
	if n := len(doc["mcpServers"].(map[string]any)); n != 1 {
		t.Errorf("expected exactly 1 server, got %d", n)
	}
}

func TestMCPSetup_MalformedClaudeJSONNotClobbered(t *testing.T) {
	srv := fakeConductor(t, http.StatusOK, credsBody)
	home := setupHomeAndEnv(t, srv.URL)
	mustWriteFile(t, filepath.Join(home, ".claude.json"), "{broken")

	if code := runMCPSetup(context.Background(), nil); code != 1 {
		t.Fatalf("expected exit 1 on malformed claude.json, got %d", code)
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if string(raw) != "{broken" {
		t.Errorf("malformed file was modified: %q", string(raw))
	}
}

func TestMCPSetup_PrintWritesNothing(t *testing.T) {
	srv := fakeConductor(t, http.StatusOK, credsBody)
	home := setupHomeAndEnv(t, srv.URL)
	mustWriteFile(t, filepath.Join(home, ".codex", "config.toml"), "")

	read, restore := captureStdout(t)
	code := runMCPSetup(context.Background(), []string{"--print"})
	restore()
	out := read()
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "claude mcp add") || !strings.Contains(out, "[mcp_servers.askdao-mcp]") {
		t.Errorf("snippets missing from --print output:\n%s", out)
	}
	toml, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(string(toml), "askdao-mcp") {
		t.Errorf("--print must not write files")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Errorf("--print must not create ~/.claude.json")
	}
}

func TestMCPSetup_Unconfigured503(t *testing.T) {
	srv := fakeConductor(t, http.StatusServiceUnavailable, `{"detail":"not configured"}`)
	setupHomeAndEnv(t, srv.URL)

	if code := runMCPSetup(context.Background(), nil); code != 1 {
		t.Fatalf("expected exit 1 on 503, got %d", code)
	}
}

func TestMCPSetup_NoHarnessDetected(t *testing.T) {
	srv := fakeConductor(t, http.StatusOK, credsBody)
	setupHomeAndEnv(t, srv.URL) // empty home: no .claude, no .codex

	if code := runMCPSetup(context.Background(), nil); code != 1 {
		t.Fatalf("expected exit 1 when no harness present, got %d", code)
	}
}

// mustWriteFile creates parents and writes content.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
