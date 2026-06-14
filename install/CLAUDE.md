# install/
> L2 | 父级: ../CLAUDE.md

一键安装脚本。canonical 在本目录（开源可审计 = 信任锚点），经 askdao.ai 的 Next.js rewrite 反向代理分发（`https://askdao.ai/install.{sh,ps1,cmd}` → `raw.githubusercontent.com/askdao/askdao-cli/main/install/*`，单一真相源不复制）。三脚本共同逻辑：GitHub `releases/latest` 302 重定向取版本（走 github.com，**不碰 api.github.com**，规避匿名 60/hr/IP 限流——共享 CGNAT 出口下尤其致命；`ASKDAO_VERSION` 可钉版本）→ 按 OS/arch 下载 GoReleaser 资产 → checksums.txt SHA256 校验 → 安装 → PATH 处理。后续升级走 `askdao update`，不重跑脚本。

## 成员清单

- **install.sh**: Unix 安装器（macOS/Linux/WSL），bash + set -euo pipefail，装入 `~/.local/bin`（`ASKDAO_INSTALL_DIR` 可覆盖），不在 PATH 只提示不静默改 rc。`curl -fsSL https://askdao.ai/install.sh | bash`
- **install.ps1**: Windows 安装器，PowerShell 5.1 兼容，装入 `%LOCALAPPDATA%\askdao\bin` + 幂等追加用户 PATH（只读写用户级）+ 更新当前会话 PATH；升级时旧 exe 先 rename `.old` 再上位。`irm https://askdao.ai/install.ps1 | iex`
- **install.cmd**: CMD thin wrapper，转调 PowerShell 执行 install.ps1

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
