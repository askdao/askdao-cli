# internal/webstudio/
> L2 | 父级: ../../CLAUDE.md

本地 Web 工作台 —— `askdao agent edit` 拉起的 `127.0.0.1` HTTP server + `go:embed` 自包含前端。KOL 在浏览器审阅/编辑 Agent spec、按 scope 勾选 skill/MCP，一站式 Save/Deploy。写 yaml / deploy 由 cmd 层注入回调，webstudio 不依赖 pipeline、自身不发起 deploy（只用 internal/deploy 的回执类型做结果映射）。实现细节（路由清单、桌面专属端点、向导交互）见各文件头注释；历史变更见 git log / PR。

## 成员清单

- **server.go** — Options / Serve / Handler（桌面长驻宿主复用）/ 路由注册（静态三件 + logo + spec/save/deploy/done/observe 基础端点 + 按回调注入条件注册的桌面端点：scan/auth/chat/assistant/open-external/skill-validate/fix）+ openBrowser + observed 运行时集合
- **api.go** — StudioData 及各请求/响应类型 + `BuildStudioData`（spec 草稿 + detection 候选摊平成前端 JSON；勾选态二选一：编辑已有 yaml 忠实回显 / 全新草稿走默认策略）
- **deploy_result.go** — deploy 回执映射单源（DeployResponse → DeployResult 六字段 + 回执落点 agent 页 + 状态栏摘要行），两个 main 包共用
- **theme.go** — 色板 token 真相源（token 名非 hex 全链贯通）+ category 默认主题/头像
- **icons.go** — avatar icon 网格数据（lucide 子集 inner-SVG，与服务端 whitelist 同一契约端到端同步）
- **logo.png** — 品牌 logo（go:embed）
- **studio.html / studio.css / studio.js** — 前端三件（vanilla JS 零构建链，三文件各自 go:embed）：向导式 4 步（Identity → Persona → Skills & Tools → Review & Deploy）+ Kami 视觉 + 信心度徽标 + 调度选择器（「Next:」预览与费用警示走 conductor /api/cron-preview 服务端权威求解，本地零 cron 时间算术；离线隐藏预览行）+ observe 叠加层 + 统一结果对话框 + 桌面专属块（测试聊天 / AI 助手侧栏 / SKILL.md 行内校验 / 外链桥接）
- **static_assets_test.go** — 守护三静态文件路由与引用一致 + 禁止内联块回潮
- **server_test.go / api_test.go / theme/icons 测试** — httptest 路由 + 序列化 + 契约守护

## 设计约束

- **回调解耦**：webstudio 只管 HTTP + 序列化 + 回执映射；写 yaml / deploy / chat 由 cmd 注入回调（单向依赖 cmd → webstudio → deploy）
- **本地隐私**：绑 `127.0.0.1`（非 localhost，避 IPv6 拒连），数据不出本机
- **无构建链**：go:embed 静态资源；字体 CDN 联网增强、离线 fallback 系统字体
- **scope 分组默认勾选**：project skill 全勾 / user 全局 skill opt-in 不勾 / stdio MCP 不勾
- **observe 是叠加层不是唯一真相**：观测只做证据高亮 + 一键收窄建议，默认全勾保持为安全网
- **cron 字符串是唯一序列化值**：选择器只是语法糖；表达不了的形态落 Custom 原样保留，绝不近似改写；seed 不在 render 回写（未触碰不产生 yaml 键）
- **禁止客户端 cron 求解器回潮**：下次触发/最小间隔一律问 conductor（static_assets_test 守护符号不回潮）；describeCron/schedFromCron 纯映射无时间算术可留本地
- **前端注入防御**：动态内容一律 textContent/createElement 渲染

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
