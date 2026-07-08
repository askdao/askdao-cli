# internal/chat/
> L2 | 父级: ../../CLAUDE.md

桌面 app「部署后测试聊天」的服务端流式客户端 —— 把 KOL 消息 POST 到 conductor `POST /api/v1/chat`（SSE），逐帧**原样透传**给上层（`cmd/askdao-studio` 经 `OnChat` 回调 → `webstudio /api/chat` → 前端 studio.html）。与 `internal/deploy` 同纪律：stdlib only，不 import `internal/types`，只收 `Request`、吐 raw 帧 `[]byte`，`.type` 分发留给前端。

## 成员清单

- **client.go** — `Client{BaseURL, HTTPClient, AuthToken}` + `NewClient(baseURL)`（**无 request timeout** —— chat turn 流多久取决于 agent 跑多久，由 ctx 取消兜底，区别于 deploy 的 180s）+ `Stream(ctx, Request, onFrame func(raw []byte) error) error`：POST `DefaultChatPath`（`/api/v1/chat`），`Authorization: Bearer`（cli_ token）+ `Accept: text/event-stream`，`bufio.Scanner` 逐行读 `resp.Body` —— 跳空行 + `:` 开头行（`: heartbeat` 注释），对 `data: ` 行剥前缀、copy（Scanner 复用 buffer）后 `onFrame`；`onFrame` 返 error（下游 writer 关闭）/ ctx 取消 / 流结束即返回；非 2xx → 错误带 status + 截断 body。Scanner buffer 抬到 1 MiB（artifact/meta 帧可能大）。`Request{Message, AgentID, SessionID}` = 私聊子集（无 X-Group-Id/mention_agent），`SessionID` 首轮空、从上轮 done 帧 `sdk_session_id`/`ov_session_id` 回填续多轮。
- **client_test.go** — `httptest` 起 mock conductor SSE：happy（跳心跳注释 + text_delta/done 帧透传 + 验 Bearer + 首轮省略 session_id）+ 多轮回填 session_id + onFrame 返错早停 + 非 2xx 带 body + 空 BaseURL。

## 设计约束

- **stdlib only + dumb pipe**：与 `internal/deploy`（决策 9.1 HTTP 客户端域）一致，只 `net/http`/`bufio`/`encoding/json` 等，连 `internal/types` 都不 import；帧原样 `[]byte` 透传、不解析 `.type`（text_delta/done/error 分发在前端 studio.html，照 ai-web `chat/route.ts` parseConductorSSE 蓝本）。
- **无 timeout 靠 ctx**：流式对话不能套 deploy 的 180s 固定超时；`http.Client{}` 无 Timeout，调用方（app.go chat 回调）拿到的是 webstudio `/api/chat` 的请求 ctx（`r.Context()`）—— WebView 关闭该 fetch 连接时流停。
- **auth 同源 deploy**：`auth.Load()` → `creds.Server`（BaseURL）+ `creds.AccessToken`（Bearer），与刚 deploy 用同一 base URL + cli_ token；`agent_id` 传 `DeployResult.AgentID`（ACL owner==caller，KOL 自己刚部署的 agent 满足）。

## 与服务端契约的对齐点

> 只引公开契约（端点路径 + 字段名 + SSE 帧形态）；服务端内部实现归私有仓。

- 请求 `Request` ↔ conductor `POST /api/v1/chat` body（`message` / `agent_id` / `session_id`）
- SSE wire ↔ `data: {json}\n\n`（无 `event:` 名字行，`.type` 区分）+ `: heartbeat` 注释帧（跳过）
- 帧 `.type`（`text_delta`/`done`/`error`/...）由前端消费，本层不 assert

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
