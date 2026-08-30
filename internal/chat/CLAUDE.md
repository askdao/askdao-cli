# internal/chat/
> L2 | 父级: ../../CLAUDE.md

桌面 app「部署后测试聊天」的服务端流式客户端 —— KOL 消息 POST 到 conductor `/api/v1/chat`（SSE），逐帧原样透传给上层（桌面 OnChat 回调 → webstudio → 前端）。实现细节见 client.go 头注释。

## 成员清单

- **client.go** — Client.Stream：SSE 逐行读 + 心跳跳过 + data 帧剥前缀 copy 后 onFrame；Request 是私聊子集（session_id 从 done 帧回填续多轮）
- **client_test.go** — httptest mock SSE：透传/多轮/早停/错误路径

## 设计约束

- **stdlib only + dumb pipe**：不 import `internal/types`；帧原样 `[]byte` 透传，`.type` 分发留给前端
- **无固定 timeout 靠 ctx**：chat turn 时长取决于 agent，WebView 关连接即停
- **auth 同源 deploy**：credentials.json 的 server + cli_ token

## 与服务端契约的对齐点

请求 body（message/agent_id/session_id）+ SSE wire（`data: {json}` + `: heartbeat` 注释帧）；帧 `.type` 本层不 assert。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
