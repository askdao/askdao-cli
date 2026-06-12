:: [INPUT]: 依赖 https://askdao.ai/install.ps1（真正的安装逻辑）
:: [OUTPUT]: CMD 一键安装入口 — 转调 PowerShell 执行 install.ps1
:: [POS]: install/ 的 CMD thin wrapper，与 install.ps1 / install.sh 并列
:: [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
::
:: Usage: curl -fsSL https://askdao.ai/install.cmd -o install.cmd && install.cmd && del install.cmd
@echo off
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://askdao.ai/install.ps1 | iex"
