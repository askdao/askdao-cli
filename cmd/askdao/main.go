// Package main is the entry point for the askdao-cli binary.
//
// [INPUT]: 命令行参数（subcommand router 解析）
// [OUTPUT]: askdao binary
// [POS]: cmd/askdao 唯一入口；subcommand 实现散在同包其他文件
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

const version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		printRootHelp(os.Stdout)
		return
	}
	ctx := context.Background()
	cmd := os.Args[1]

	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("askdao-cli %s\n", version)
	case "help", "-h", "--help":
		printRootHelp(os.Stdout)
	case "detect":
		os.Exit(runDetect(ctx, os.Args[2:]))
	case "bundle":
		os.Exit(runBundle(ctx, os.Args[2:]))
	case "agent":
		os.Exit(runAgent(ctx, os.Args[2:]))
	case "auth":
		os.Exit(runAuth(ctx, os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "askdao: unknown command %q\n\n", cmd)
		printRootHelp(os.Stderr)
		os.Exit(2)
	}
}

// runAgent dispatches the `askdao agent <subcommand>` family.
func runAgent(ctx context.Context, args []string) int {
	if len(args) < 1 {
		printAgentHelp(os.Stdout)
		return 0
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "init":
		return runInit(ctx, rest)
	case "show":
		return runShow(ctx, rest)
	case "deploy":
		return runDeploy(ctx, rest)
	case "help", "-h", "--help":
		printAgentHelp(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "askdao agent: unknown subcommand %q\n\n", sub)
		printAgentHelp(os.Stderr)
		return 2
	}
}

func printRootHelp(w io.Writer) {
	fmt.Fprintf(w, `askdao-cli %s

Local CLI for AskDAO — bootstrap AI agents from your project directory.

USAGE:
    askdao <command> [args]

COMMANDS:
    auth login                     Browser-bound login (OAuth 2.0 Device Code Flow)
    auth status                    Show the currently-logged-in identity
    auth logout                    Delete local credentials
    detect [path]                  Print detection report (L1-L3, no LLM)
    bundle [path]                  Preview the deployment payload (what gets uploaded)
    agent init [name] [--auto]     Generate askdao-agent.yml at project root
    agent show [flags]             Show the mid-density review card
    agent deploy [--harness id]    Push agent to conductor
    version                        Print version
    help                           Show this help

Run 'askdao <command> --help' for command-specific flags.
`, version)
}

func printAgentHelp(w io.Writer) {
	fmt.Fprint(w, `USAGE:
    askdao agent <subcommand> [args]

SUBCOMMANDS:
    init [name] [--auto] [--from path] [--harness id]
        Generate askdao-agent.yml at the project root and .askdao/ for tool
        products (recommendation.yml + detection.json). With --auto, runs the
        L1-L4 pipeline + interactive review. `+"`name`"+` is optional and
        defaults to the project directory name.

    show [--full|--reasoning|--warnings|--persona|--deps|--mcp] [--dir path]
        Render the mid-density review card. Flags drill into one section.

    deploy [--harness id] [--force] [--bio text] [--dir path]
        Read <dir>/askdao-agent.yml + recommendation snapshot, diff against the
        original recommendation, and push spec
        to the chosen harness.
`)
}
