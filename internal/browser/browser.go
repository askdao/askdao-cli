// [INPUT]: 标准库 (io, os/exec, runtime)
// [OUTPUT]: 对外提供 Open(url) error —— 跨平台打开默认浏览器
// [POS]: internal/browser 唯一成员。此前 webstudio/server、askdao-studio/app、askdao/auth
//        三处各写一份 darwin/windows/xdg-open 三分支，收敛于此；caller 自行决定是否吞错
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package browser

import (
	"io"
	"os/exec"
	"runtime"
)

// Open launches the platform default browser at url. Best-effort: the browser
// process is started detached and its stdio discarded.
func Open(url string) error {
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
