// [INPUT]: 标准库 bufio / bytes / context / encoding/json / errors / fmt / io / net/http / strings
// [OUTPUT]: Client（Stream）+ Request + NewClient + DefaultChatPath
// [POS]: internal/chat 的服务端流式聊天客户端 —— 桌面 app 部署后测试聊天调 POST /api/v1/chat（SSE），逐帧原样透传给上层（webstudio /api/chat → 前端）；cmd/askdao-studio/app.go 经 OnChat 回调消费。与 internal/deploy 同纪律：stdlib only，不 import internal/types
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DefaultChatPath is the conductor REST path for the chat SSE stream.
const DefaultChatPath = "/api/v1/chat"

// Request is the JSON body for POST /api/v1/chat — the private-chat subset
// (no X-Group-Id / mention_agent; those are group-only). SessionID is empty on
// the first turn and carried back from the previous turn's done frame
// (sdk_session_id / ov_session_id) for multi-turn continuity.
type Request struct {
	Message   string `json:"message"`
	AgentID   string `json:"agent_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// Client streams conductor's chat SSE. Assemble it the same way as
// deploy.Client: auth.Load() → BaseURL (creds.Server) + AuthToken (cli_ bearer).
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	// AuthToken is the bearer token (cli_ ...). Required — /chat needs auth.
	AuthToken string
}

// NewClient builds a Client. baseURL e.g. "https://api.askdao.ai". There is
// deliberately NO request timeout — a chat turn streams for as long as the
// agent runs; the caller bounds it by cancelling ctx.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTPClient: &http.Client{}}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{}
}

var dataPrefix = []byte("data: ")

// Stream POSTs req to conductor /api/v1/chat and invokes onFrame with each raw
// SSE data-frame JSON, verbatim — the caller (and ultimately the frontend) owns
// `.type` dispatch (text_delta / done / error / ...), so this client stays a
// dumb pipe. It skips SSE comment lines (": heartbeat") and blank separators,
// strips the "data: " prefix, and returns when the stream closes, ctx is
// cancelled, or onFrame returns an error (e.g. the downstream writer closed).
// A non-2xx status is a non-streaming error carrying the (truncated) body.
func (c *Client) Stream(ctx context.Context, req Request, onFrame func(raw []byte) error) error {
	if c.BaseURL == "" {
		return errors.New("chat: Client.BaseURL is empty")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+DefaultChatPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.AuthToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("chat: conductor unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return fmt.Errorf("chat: conductor returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	sc := bufio.NewScanner(resp.Body)
	// Frames are single lines (json.dumps escapes newlines) but can be large
	// (artifact/meta frames); lift the 64 KiB default token cap to 1 MiB.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] == ':' { // blank separator or ": heartbeat" comment
			continue
		}
		if !bytes.HasPrefix(line, dataPrefix) {
			continue
		}
		raw := bytes.TrimSpace(line[len(dataPrefix):])
		if len(raw) == 0 {
			continue
		}
		// Scanner reuses its buffer across Scan calls — copy before handing off.
		frame := make([]byte, len(raw))
		copy(frame, raw)
		if err := onFrame(frame); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil { // ctx cancel surfaces as a read error — clean stop
			return nil
		}
		return fmt.Errorf("chat: read stream: %w", err)
	}
	return nil
}
