// [INPUT]: 依赖 context + flag + fmt + os + os/exec + runtime + time + errors;
//
//	依赖 internal/auth 的 Credentials / DeviceFlow / 错误 sentinels;
//	依赖 main.go 暴露的 version 常量。
//
// [OUTPUT]: 对外提供 runAuth(ctx, args) — `askdao auth` 子命令分发器，路由到
//
//	login / status / logout。
//
// [POS]: cmd/askdao — askdao-cli OAuth 2.0 Device Code Flow 的用户界面层；
//
//	完全不持状态，编排 internal/auth 的 HTTP client + 文件持久化 +
//	浏览器打开。设计稿 docs/cli-auth-device-flow.md §6.2。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/askdao/askdao-cli/internal/auth"
)

// runAuth dispatches `askdao auth <subcommand>`.
func runAuth(ctx context.Context, args []string) int {
	if len(args) < 1 {
		printAuthHelp(os.Stdout)
		return 0
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "login":
		return runAuthLogin(ctx, rest)
	case "status":
		return runAuthStatus(ctx, rest)
	case "logout":
		return runAuthLogout(ctx, rest)
	case "help", "-h", "--help":
		printAuthHelp(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "askdao auth: unknown subcommand %q\n\n", sub)
		printAuthHelp(os.Stderr)
		return 2
	}
}

// ---------------------------------------------------------------------------
// login
// ---------------------------------------------------------------------------

func runAuthLogin(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	// Output device-name as a placeholder until conductor v2 supports it on
	// approve; v1 we send it but conductor ignores until web UI surfaces.
	_ = fs.String("name", defaultDeviceName(), "Device name shown in the web approval UI")
	serverFlag := fs.String("server", "", "Override conductor server URL (default: env $ASKDAO_CONDUCTOR_URL or "+auth.DefaultServerURL+")")
	noBrowserFlag := fs.Bool("no-browser", false, "Print the URL instead of trying to open a browser (useful on headless / SSH)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	server := pickServerURL(*serverFlag)
	clientName := fmt.Sprintf("askdao-cli/%s %s/%s", version, runtime.GOOS, runtime.GOARCH)
	df := auth.NewDeviceFlow(server, clientName)

	fmt.Fprintln(os.Stderr, "→ Requesting device code from", server, "…")
	start, err := df.Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}

	// Print code prominently so the KOL can compare against the web page.
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Your code:", start.UserCode)
	fmt.Fprintln(os.Stderr, "Open       ", start.VerificationURIComplete)
	fmt.Fprintln(os.Stderr)

	if !*noBrowserFlag {
		if err := openBrowser(start.VerificationURIComplete); err != nil {
			fmt.Fprintln(os.Stderr, "(could not open browser automatically:", err, ")")
			fmt.Fprintln(os.Stderr, "   Visit the URL above manually.")
		}
	}

	fmt.Fprintln(os.Stderr, "→ Waiting for approval (Ctrl-C to cancel) …")
	tok, err := df.PollUntilApproved(
		ctx,
		start.DeviceCode,
		time.Duration(start.Interval)*time.Second,
		time.Duration(start.ExpiresIn)*time.Second,
	)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrExpiredToken):
			fmt.Fprintln(os.Stderr, "✗ Login window expired. Run `askdao auth login` again.")
		case errors.Is(err, auth.ErrAccessDenied):
			fmt.Fprintln(os.Stderr, "✗ Authorization denied in the browser.")
		case errors.Is(err, auth.ErrAlreadyConsumed):
			fmt.Fprintln(os.Stderr, "✗ Device code was already redeemed. Run `askdao auth login` again.")
		default:
			fmt.Fprintln(os.Stderr, "✗", err)
		}
		return 1
	}

	creds := &auth.Credentials{
		Server:      server,
		UserID:      tok.UserID,
		UserEmail:   tok.UserEmail,
		AccessToken: tok.AccessToken,
	}
	if err := auth.Save(creds); err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}
	path, _ := auth.Path()
	fmt.Fprintf(os.Stderr, "✓ Logged in as %s. Token saved to %s.\n", tok.UserEmail, path)
	return 0
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func runAuthStatus(_ context.Context, _ []string) int {
	creds, err := auth.Load()
	if err != nil {
		if errors.Is(err, auth.ErrNoCredentials) {
			fmt.Fprintln(os.Stderr, "Not logged in. Run `askdao auth login`.")
			return 1
		}
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}
	path, _ := auth.Path()
	fmt.Printf("Logged in as %s\n", creds.UserEmail)
	fmt.Printf("  user_id   %s\n", creds.UserID)
	fmt.Printf("  server    %s\n", creds.Server)
	fmt.Printf("  created   %s\n", creds.CreatedAt.Local().Format(time.RFC3339))
	fmt.Printf("  file      %s\n", path)
	return 0
}

// ---------------------------------------------------------------------------
// logout
// ---------------------------------------------------------------------------

func runAuthLogout(_ context.Context, _ []string) int {
	if err := auth.Delete(); err != nil {
		if errors.Is(err, auth.ErrNoCredentials) {
			fmt.Fprintln(os.Stderr, "Already logged out.")
			return 0
		}
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "✓ Local credentials removed.")
	fmt.Fprintln(os.Stderr, "  Note: the server-side token is still valid until revoked. Use the web UI to revoke (coming in v2).")
	return 0
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// pickServerURL resolves which conductor URL to talk to. Mirrors the
// resolution order documented in docs/cli-auth-device-flow.md §6.3 for the
// login command:
//  1. --server flag
//  2. $ASKDAO_CONDUCTOR_URL env var
//  3. compiled DefaultServerURL
//
// (The credentials.json `server` field is read by deploy, not by login —
// login is what creates the credentials, so it cannot depend on them.)
func pickServerURL(flagVal string) string {
	if s := strings.TrimSpace(flagVal); s != "" {
		return s
	}
	if s := strings.TrimSpace(os.Getenv("ASKDAO_CONDUCTOR_URL")); s != "" {
		return s
	}
	return auth.DefaultServerURL
}

// defaultDeviceName best-effort returns the hostname so the web approval
// card can show "macbook-pro" without the user typing it.
func defaultDeviceName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown-device"
}

// openBrowser tries platform-appropriate commands; failure is non-fatal —
// the URL is also printed to stderr so the user can copy it manually.
func openBrowser(url string) error {
	var bin string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		bin, args = "open", []string{url}
	case "windows":
		bin, args = "cmd", []string{"/c", "start", "", url}
	default: // linux, *bsd, etc.
		bin, args = "xdg-open", []string{url}
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

// ---------------------------------------------------------------------------
// help
// ---------------------------------------------------------------------------

func printAuthHelp(w io.Writer) {
	fmt.Fprint(w, `USAGE:
    askdao auth <subcommand> [args]

SUBCOMMANDS:
    login [--server <url>] [--name <device-name>] [--no-browser]
        Browser-bound OAuth 2.0 Device Code Flow. Prints a short code,
        opens askdao.ai/cli/auth?code=... in your browser, polls until
        you click Authorize, then saves a long-lived token to
        ~/.config/askdao/credentials.json.

    status
        Show the currently-logged-in identity (email, server, file path).
        Exit code 1 when not logged in.

    logout
        Delete the local credentials file. Does NOT revoke the server-side
        token — use the web UI for revocation (coming in v2).
`)
}
