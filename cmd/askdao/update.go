// [INPUT]: 依赖 internal/selfupdate 的 Updater；main.version（GoReleaser ldflags 注入）
// [OUTPUT]: 对外提供 runUpdate —— `askdao update [--force]` 自升级到最新 Release
// [POS]: cmd/askdao 的 update 子命令；首装走 install/ 脚本，升级走这里
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/askdao/askdao-cli/internal/selfupdate"
)

func runUpdate(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	force := fs.Bool("force", false, "reinstall even if already up to date (or running a -dev build)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `USAGE:
    askdao update [--force]

Self-update askdao to the latest GitHub release. --force reinstalls the
latest release even when the version already matches.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// GoReleaser snapshot/source builds carry a -dev suffix — they didn't come
	// from a release archive, so an in-place swap would silently discard local
	// changes. Developers rebuild with `go install ./cmd/askdao` instead.
	if strings.HasSuffix(version, "-dev") && !*force {
		fmt.Fprintf(os.Stderr, "askdao update: running a dev build (%s); use `go install ./cmd/askdao` or --force\n", version)
		return 1
	}

	err := selfupdate.New().Run(ctx, version, *force)
	if errors.Is(err, selfupdate.ErrUpToDate) {
		fmt.Printf("askdao is %v\n", err)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "askdao update: %v\n", err)
		return 1
	}
	return 0
}
