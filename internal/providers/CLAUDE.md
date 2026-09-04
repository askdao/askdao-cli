# internal/providers/
> L2 | 父级: ../../CLAUDE.md

L3 启发式层 —— 移植 railwayapp/nixpacks 的 Provider trait 抽象与探测逻辑，外加自研 dep→apt 反向映射表。Python / Node / Go / Rust 四个 provider。实现细节见各文件头注释；历史变更见 git log / PR。

## 成员清单

- **provider.go** — Provider 接口（Name/Detect/Plan/Metadata）+ App（一次 walk 缓存 file index）/ Env 抽象 + HasDep + FrameworkPlan（nixpacks BuildPlan 精简版，无 install/start phase）
- **apt_map.go** — dep→apt 双表反向映射（python + npm；Rust 私表就近放 rust.go）
- **python.go** — PythonProvider：lockfile 强信号 PM 选择链 + 多源证据加权框架规则 + 外部服务反查
- **node.go** — NodeProvider：PM 选择链 + 框架规则 + syft 优先/package.json fallback 双轨 dep 探测
- **go.go** — GoProvider：go.mod/go.work 触发 + runtime 抽取 + cgo 触发 apt + 外部服务反查
- **rust.go** — RustProvider：runtime 三级回退 + sys-crate 反向 apt 私表 + workspace evidence
- **\*\_test.go** — 接口 conformance + 各 provider PM/框架/apt/反例用例
- **testdata/** — t.TempDir fixture 为主，无静态目录

## 设计约束

- **不抄 install/start phase**：终态是声明式 environment 而非 Dockerfile recipe
- **Pkgs 由 caller 注入**：provider 不自己 spawn syft，一次扫描多 provider 共用
- **多源证据加权打分**：manifest + import 扫描 + 配置文件存在性，多于一项才算数
- **PEP 503 归一**与 scanner 同规则（各包独立实现避免 import cycle）
- **Go/Rust 不强求 Pkgs**：nil 时跳过对应表，Plan 仍能工作

## 字段输出对应

frameworks / inferred_apt_packages / external_services 由 Plan 产出；runtimes 归 scanner，provider 只产 RuntimeHint 交叉校验。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
