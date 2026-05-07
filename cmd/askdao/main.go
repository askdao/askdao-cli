// Package main is the entry point for the askdao-cli binary.
//
// [INPUT]: 依赖 标准库 fmt/os
// [OUTPUT]: 对外提供 askdao binary 可执行入口
// [POS]: cmd/askdao 唯一入口，未来挂载子命令到此
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"fmt"
	"os"
)

const version = "0.0.1-dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Printf("askdao-cli %s\n", version)
			return
		case "help", "-h", "--help":
			printHelp()
			return
		}
	}
	printHelp()
}

func printHelp() {
	fmt.Printf(`askdao-cli %s

Local CLI for AskDAO — bootstrap AI agents from your project directory.

USAGE:
    askdao <command> [args]

COMMANDS:
    version         Print version
    help            Show this help

PLANNED COMMANDS (not yet implemented):
    detect          Print detection report for current directory
    agent init      Generate agent.yml skeleton (--auto for smart scan)
    agent validate  Validate agent.yml against schema
    agent deploy    Push agent + environment to Anthropic Managed Agents

This is pre-alpha software. See README.md for design status.
`, version)
}
