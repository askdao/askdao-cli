# internal/recommender/
> L2 | 父级: ../../CLAUDE.md

L4 推荐器 —— askdao-cli 的「模糊推断」边界。L1-L3 全确定性，本目录只有「策略 heuristic」和「conductor HTTP 客户端」两件事（LLM 走 conductor 中转，不 BYOK）。实现细节见各文件头注释；历史变更见 [../../CHANGELOG.md](../../CHANGELOG.md)。

## 成员清单

- **policy.go** — 生产/用户数据信号探测 → tool risk hints（默认 policy 恒 always_allow，只对命中信号的 bash/write 加 always_ask override）
- **fs.go** — os.Stat/ReadDir 薄包装（测试可替换钩子）
- **llm.go** — LLMClient 接口 + ConductorClient（/cli/recommend + 模型目录/配置各 Fetch* 及其 graceful fallback wrapper；HTTP 往返收敛在泛型 `doJSON[T]`，降级收敛在 `fetchOr[T]`）+ MockClient（离线/单测参考实现，含 vault hints 构造与 harness 优先级推导）
- **capabilities.go** — 确定性 capabilities 生成（4 槽固定 + 规范 scopes 词表 + production 信号收紧 shell 权限）
- **\*\_test.go** — policy 边界 + Mock/Conductor 客户端序列化往返与错误路径

## 设计约束

- **LLM 走 conductor 中转**：本目录绝不直接 import LLM SDK
- **不依赖 internal/providers**：用 ProviderSummary 中间结构传字段，避免跨包循环
- **mock-first**：CI 不依赖 conductor 在线；`ASKDAO_CONDUCTOR_URL` 未设时 cmd 层默认走 Mock
- **policy 层只标 override 不翻 default**：全局收紧与否的决策权留给 LLM
- **apiVersion 钉死校验**：响应 schema 版本不符直接 reject，防服务端静默升级
- **客户端零硬编码 model id**：真 id 由 conductor 从 model_class 解析或 Studio 从 catalog 填

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
