# internal/selfupdate/
> L2 | 父级: ../../CLAUDE.md

`askdao update` 的自升级引擎（stdlib only）。与 `install/` 脚本共享 GoReleaser 资产命名契约（`askdao_{ver}_{os}_{arch}.{tar.gz|zip}` + checksums.txt）：脚本管首装，本包管后续升级。

## 成员清单

- **selfupdate.go**: `Updater`（Repo/APIBase/DownloadBase/Client/Out/ExePath/GOOS/GOARCH 全部可注入，零值落真实 GitHub 端点）。`Run(ctx, currentVersion, force)`：latest API 取版本 → 同版本且非 force 返回 `ErrUpToDate` → 下载本平台 archive + checksums.txt SHA256 校验 → 提取二进制 → 原子换装（temp 同目录保证同文件系统；**Windows 运行中 exe 不能覆盖但能 rename**：旧 exe 先挪 `.old` 再上位，`.old` best-effort 删 + 下次 Run 开头清扫）
- **selfupdate_test.go**: httptest 假 GitHub 全链路（AssetName 平台映射 / checksum ± / zip+tar.gz 提取 / Run e2e 换装 / up-to-date / force 同版本重装 / 坏 checksum 不动二进制），不出网、不碰真实可执行文件

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
