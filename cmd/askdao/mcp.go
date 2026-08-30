// [INPUT]: 依赖 deploy.go 的 resolveServerAndToken（env 成对 > credentials.json）；
//
//	标准库 net/http + encoding/json + os/exec（Windows setx）。
//
// [OUTPUT]: 对外提供 runMCP(ctx, args) — `askdao mcp` 子命令分发器，当前仅 setup。
// [POS]: cmd/askdao — KOL 本机 askdao-mcp 接入的自动化入口（onboarding 计划 M2）：
//
//	调 conductor GET /api/v1/cli/mcp-credentials 取 gateway URL+token，写入本机
//	Claude Code（~/.claude.json，literal token）与 Codex（~/.codex/config.toml，
//	bearer_token_env_var 引用 ASKDAO_MCP_TOKEN）。线上 Managed Agent 凭证由
//	Vault 服务端注入，与本命令无关 —— 这里只服务本地调试 + agent edit 扫描发现。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/askdao/askdao-cli/internal/deployflow"
)

const (
	mcpServerName = "askdao-mcp"
	mcpTokenEnv   = "ASKDAO_MCP_TOKEN"
)

// runMCP dispatches `askdao mcp <subcommand>`.
func runMCP(ctx context.Context, args []string) int {
	if len(args) < 1 {
		printMCPHelp(os.Stdout)
		return 0
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "setup":
		return runMCPSetup(ctx, rest)
	case "help", "-h", "--help":
		printMCPHelp(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "askdao mcp: unknown subcommand %q\n\n", sub)
		printMCPHelp(os.Stderr)
		return 2
	}
}

// mcpCredentials mirrors conductor's MCPCredentialsResponse.
type mcpCredentials struct {
	GatewayURL string `json:"gateway_url"`
	Token      string `json:"token"`
}

func runMCPSetup(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	printOnly := fs.Bool("print", false, "Print the config snippets instead of writing files")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	server, token, err := deployflow.ResolveServerAndToken()
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		fmt.Fprintln(os.Stderr, "  Run `askdao auth login` first.")
		return 3
	}

	if *printOnly {
		creds, err := fetchMCPCredentials(ctx, server, token)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗", err)
			return 1
		}
		printMCPSnippets(os.Stdout, creds)
		return 0
	}

	if err := applyMCPSetup(ctx, server, token); err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}
	return 0
}

// applyMCPSetup is the shared body of `askdao mcp setup` and the auto-setup
// that runs right after `askdao auth login`: fetch gateway credentials from
// conductor, then configure whichever harnesses exist on this machine.
// Progress goes to stderr; the caller decides whether a failure is fatal
// (standalone setup) or merely advisory (post-login auto-run).
func applyMCPSetup(ctx context.Context, server, bearer string) error {
	creds, err := fetchMCPCredentials(ctx, server, bearer)
	if err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot resolve home dir: %w", err)
	}

	claudePresent := pathIsDir(filepath.Join(home, ".claude")) || pathIsFile(filepath.Join(home, ".claude.json"))
	codexPresent := pathIsDir(filepath.Join(home, ".codex"))
	if !claudePresent && !codexPresent {
		return fmt.Errorf("no harness detected (neither ~/.claude nor ~/.codex exists) — install Claude Code or Codex first, or run `askdao mcp setup --print` for manual snippets")
	}

	if claudePresent {
		path := filepath.Join(home, ".claude.json")
		if err := upsertClaudeMCP(path, creds); err != nil {
			return fmt.Errorf("Claude Code config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "✓ Claude Code: %s configured in %s\n", mcpServerName, path)
	}

	if codexPresent {
		path := filepath.Join(home, ".codex", "config.toml")
		changed, err := upsertCodexMCP(path, creds.GatewayURL)
		if err != nil {
			return fmt.Errorf("Codex config: %w", err)
		}
		if changed {
			fmt.Fprintf(os.Stderr, "✓ Codex: %s configured in %s\n", mcpServerName, path)
		} else {
			fmt.Fprintf(os.Stderr, "✓ Codex: %s already configured in %s\n", mcpServerName, path)
		}
		reportTokenEnv(creds.Token)
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "askdao-mcp is set up. Restart your terminal, then verify:")
	if claudePresent {
		fmt.Fprintln(os.Stderr, "  claude  → type /mcp and check that askdao-mcp is connected")
	}
	if codexPresent {
		fmt.Fprintln(os.Stderr, "  codex   → ask it to list the askdao-mcp tools")
	}
	return nil
}

// fetchMCPCredentials calls conductor's GET /api/v1/cli/mcp-credentials.
func fetchMCPCredentials(ctx context.Context, server, bearer string) (*mcpCredentials, error) {
	url := strings.TrimRight(server, "/") + "/api/v1/cli/mcp-credentials"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach conductor at %s: %w", server, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK:
		// fallthrough to decode below
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("conductor rejected the token (401) — run `askdao auth login` again")
	case http.StatusServiceUnavailable:
		return nil, fmt.Errorf("the server has no CLI gateway token configured (503) — contact the platform operator")
	default:
		return nil, fmt.Errorf("conductor returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var creds mcpCredentials
	if err := json.Unmarshal(body, &creds); err != nil {
		return nil, fmt.Errorf("parse mcp-credentials response: %w", err)
	}
	if creds.GatewayURL == "" || creds.Token == "" {
		return nil, fmt.Errorf("mcp-credentials response is missing gateway_url or token")
	}
	return &creds, nil
}

// upsertClaudeMCP read-modify-writes ~/.claude.json, setting only
// mcpServers["askdao-mcp"] and preserving every other key. The file holds a lot
// of unrelated Claude Code state — a malformed parse aborts rather than
// clobbering it. Claude Code's user scope keeps the token out of any git repo,
// so the literal bearer goes in directly (no env indirection needed).
func upsertClaudeMCP(path string, creds *mcpCredentials) error {
	doc := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%s is not valid JSON (%v) — fix or remove it, then rerun", path, err)
		}
	case os.IsNotExist(err):
		// fresh file
	default:
		return err
	}

	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[mcpServerName] = map[string]any{
		"type": "http",
		"url":  creds.GatewayURL,
		"headers": map[string]any{
			"Authorization": "Bearer " + creds.Token,
		},
	}
	doc["mcpServers"] = servers

	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(encoded, '\n'))
}

// upsertCodexMCP appends an [mcp_servers.askdao-mcp] table to
// ~/.codex/config.toml when absent. The file is the KOL's hand-maintained
// config (comments, ordering) — a TOML round-trip would destroy it, so we only
// ever append, never rewrite. Presence is checked textually on the table
// header; an existing entry is left untouched (idempotent, returns false).
// The token is referenced via bearer_token_env_var, Codex's official auth
// mechanism — see reportTokenEnv for how the env var itself gets set.
func upsertCodexMCP(path, gatewayURL string) (bool, error) {
	header := "[mcp_servers." + mcpServerName + "]"
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(raw), header) {
		return false, nil
	}
	block := fmt.Sprintf(
		"\n# Added by `askdao mcp setup` — askdao platform MCP gateway\n%s\nurl = %q\nbearer_token_env_var = %q\n",
		header, gatewayURL, mcpTokenEnv,
	)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(block); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

// reportTokenEnv makes sure ASKDAO_MCP_TOKEN reaches the user's environment —
// Codex reads the bearer through it. Windows gets `setx` (persists the user
// env without touching shell profiles); elsewhere we print the export line
// rather than editing the user's shell profile behind their back.
func reportTokenEnv(token string) {
	if os.Getenv(mcpTokenEnv) == token {
		return
	}
	if runtime.GOOS == "windows" {
		if err := exec.Command("setx", mcpTokenEnv, token).Run(); err == nil {
			fmt.Fprintf(os.Stderr, "✓ %s set via setx (takes effect in new terminals)\n", mcpTokenEnv)
			return
		}
		fmt.Fprintf(os.Stderr, "! Could not run setx — set it manually:\n    setx %s \"%s\"\n", mcpTokenEnv, token)
		return
	}
	fmt.Fprintf(os.Stderr, "! Add this to your shell profile (e.g. ~/.zshrc) for Codex:\n    export %s=%q\n", mcpTokenEnv, token)
}

// printMCPSnippets emits manual-setup material for --print: the Claude Code
// one-liner and the Codex TOML block, plus the env var line.
func printMCPSnippets(w io.Writer, creds *mcpCredentials) {
	fmt.Fprintf(w, `# Claude Code (writes user scope ~/.claude.json):
claude mcp add --transport http %s %s --header "Authorization: Bearer %s" --scope user

# Codex — append to ~/.codex/config.toml:
[mcp_servers.%s]
url = %q
bearer_token_env_var = %q

# Environment variable consumed by Codex:
export %s=%q
`, mcpServerName, creds.GatewayURL, creds.Token,
		mcpServerName, creds.GatewayURL, mcpTokenEnv,
		mcpTokenEnv, creds.Token)
}

// atomicWrite mirrors internal/auth's temp+rename pattern so a mid-write kill
// never leaves a truncated config behind.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once rename succeeded
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func pathIsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func pathIsFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func printMCPHelp(w io.Writer) {
	fmt.Fprint(w, `USAGE:
    askdao mcp <subcommand> [args]

SUBCOMMANDS:
    setup [--print]
        Fetch the askdao MCP gateway URL + token from conductor (requires
        `+"`askdao auth login`"+`) and configure the local harnesses:
          - Claude Code: upserts mcpServers.askdao-mcp in ~/.claude.json
          - Codex: appends [mcp_servers.askdao-mcp] to ~/.codex/config.toml
            and sets the ASKDAO_MCP_TOKEN env var (setx on Windows)
        --print emits the config snippets instead of writing any file.
`)
}
