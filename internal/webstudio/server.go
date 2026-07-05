// [INPUT]: 标准库 (context/embed/encoding/json/fmt/net/net/http/os/exec/runtime/sort/strings/sync/time) + internal/types
// [OUTPUT]: 对外提供 Options / Serve / Handler；包内 buildMux / openBrowser / observed
// [POS]: webstudio 的本地 HTTP server —— 绑 127.0.0.1:随机端口，serve go:embed 的 studio.html +
//
//	/api/spec(GET) /api/save /api/deploy /api/done /api/observe(GET 读名单 / POST 收 hook 上报)；
//	写 yaml / deploy 由 cmd 层注入 OnSave/OnDeploy 回调解耦，OnReady(port) 在 serve 后回调供 cmd 写 hook settings；
//	阻塞至 KOL 在浏览器点 部署 或 完成。buildMux 抽出供 httptest 单测。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package webstudio

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/askdao/askdao-cli/internal/types"
)

//go:embed studio.html
var studioHTML []byte

//go:embed logo.png
var logoPNG []byte

// Options drives one studio session. Data is the payload served to the browser;
// OnSave persists the edited spec (write askdao-agent.yml); OnDeploy persists +
// pushes to conductor and returns a structured DeployResult (message + group link).
// OnReady, if set, is called with the bound port once the server is listening
// (before blocking) — the --observe path uses it to write the hook settings that
// point back here. All are injected by the cmd layer so webstudio stays free of
// pipeline/deploy deps.
type Options struct {
	Data *StudioData
	// OnSpec, if set, supplies the StudioData for GET /api/spec dynamically —
	// the desktop app returns a placeholder until a folder is scanned, then swaps
	// in the scanned draft. nil (the CLI path) → the static Data above is served.
	OnSpec func() *StudioData
	// OnScan, if set, registers POST /api/scan: the desktop app opens a folder
	// picker, runs the scan pipeline, and returns fresh StudioData. CLI leaves it
	// nil (agent edit already scanned before Serve), so the route never exists there.
	OnScan    func() (*StudioData, error)
	OnSave    func(*types.AgentSpec) error
	OnDeploy  func(*types.AgentSpec) (*DeployResult, error)
	OnReady   func(port int)
	NoBrowser bool

	// Desktop-only auth callbacks. All nil in the CLI (agent edit) path — the
	// desktop app injects them, and their presence registers the /api/auth/*
	// routes. CLI leaves them nil, so `agent edit` never exposes those routes.
	OnAuthState func() AuthState
	OnLogin     func() (LoginChallenge, error)
	OnLoginPoll func() (AuthState, error)
	OnLogout    func() error
}

// Serve starts the local studio, opens the browser, and blocks until the KOL
// confirms (Deploy success or Done). Save can fire any number of times without
// ending the session. Returns the first fatal error, or nil on clean exit.
func Serve(opts Options) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("webstudio: listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)

	done := make(chan error, 1)
	srv := &http.Server{Handler: buildMux(opts, done)}
	go func() { _ = srv.Serve(ln) }()

	// Hand the bound port to the cmd layer before blocking — --observe writes the
	// hook settings now, so the next `claude` session snapshots them at startup.
	if opts.OnReady != nil {
		opts.OnReady(port)
	}

	fmt.Printf("→ Agent studio at %s\n", url)
	if !opts.NoBrowser {
		_ = openBrowser(url)
	}
	fmt.Println("  Review and edit in the browser, then click Deploy or Done (Ctrl-C to abort).")

	err = <-done
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	return err
}

// Handler builds the studio's HTTP handler for embedding in a long-lived host —
// the desktop app wires it into the Wails AssetServer, where a deploy must NOT
// end the session. Serve (the CLI path) blocks until deploy/done; the desktop
// host owns its own lifecycle, so the done signal is created here and discarded
// (buffered cap-1, never read). Routes are identical to Serve's.
func Handler(opts Options) http.Handler {
	return buildMux(opts, make(chan error, 1))
}

// buildMux wires the studio routes. done is signaled (closed-over) when the KOL
// confirms via /api/deploy (success) or /api/done. Extracted from Serve so
// httptest can exercise the handlers without binding a socket.
func buildMux(opts Options, done chan error) *http.ServeMux {
	mux := http.NewServeMux()
	obs := newObserved()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(studioHTML)
	})

	mux.HandleFunc("/logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=86400")
		_, _ = w.Write(logoPNG)
	})

	mux.HandleFunc("/api/spec", func(w http.ResponseWriter, r *http.Request) {
		if opts.OnSpec != nil {
			writeJSON(w, opts.OnSpec())
			return
		}
		writeJSON(w, opts.Data)
	})

	mux.HandleFunc("/api/save", func(w http.ResponseWriter, r *http.Request) {
		spec, err := decodeSpec(r)
		if err != nil {
			writeErr(w, err)
			return
		}
		if opts.OnSave != nil {
			if err := opts.OnSave(spec); err != nil {
				writeErr(w, err)
				return
			}
		}
		writeJSON(w, map[string]string{"status": "saved"})
	})

	mux.HandleFunc("/api/deploy", func(w http.ResponseWriter, r *http.Request) {
		spec, err := decodeSpec(r)
		if err != nil {
			writeErr(w, err)
			return
		}
		if opts.OnSave != nil {
			if err := opts.OnSave(spec); err != nil {
				writeErr(w, err)
				return
			}
		}
		res := &DeployResult{Message: "Deployed."}
		if opts.OnDeploy != nil {
			res, err = opts.OnDeploy(spec)
			if err != nil {
				writeErr(w, err)
				return
			}
		}
		writeJSON(w, map[string]interface{}{"status": "deployed", "message": res.Message, "group_link": res.GroupLink, "agent_id": res.AgentID, "created": res.Created})
		signal(done)
	})

	// /api/done is "Save & finish": persist the edited spec (same as /api/save) then
	// exit without deploying. Without the OnSave call the KOL's edits — display_name,
	// avatar, everything — would be lost, leaving deploy to read a stale yaml.
	mux.HandleFunc("/api/done", func(w http.ResponseWriter, r *http.Request) {
		spec, err := decodeSpec(r)
		if err != nil {
			writeErr(w, err)
			return
		}
		if opts.OnSave != nil {
			if err := opts.OnSave(spec); err != nil {
				writeErr(w, err)
				return
			}
		}
		writeJSON(w, map[string]string{"status": "done", "message": "Saved askdao-agent.yml"})
		signal(done)
	})

	// /api/observe doubles as the Claude Code PreToolUse hook receiver (POST) and
	// the frontend poll source (GET). POST always returns 200 — observe is a
	// non-blocking overlay and must never disrupt the KOL's claude session.
	mux.HandleFunc("/api/observe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			skills, servers := obs.snapshot()
			writeJSON(w, ObservedData{Skills: skills, MCPServers: servers})
			return
		}
		var p struct {
			ToolName  string                 `json:"tool_name"`
			ToolInput map[string]interface{} `json:"tool_input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		switch {
		case p.ToolName == "Skill":
			obs.addSkill(skillNameFrom(p.ToolInput))
		case strings.HasPrefix(p.ToolName, "mcp__"):
			obs.addMCPServer(mcpServerOf(p.ToolName))
		}
		w.WriteHeader(http.StatusOK)
	})

	// Desktop-only POST /api/scan — pick a folder, run the scan pipeline, return
	// fresh StudioData (the frontend then reloads). CLI leaves OnScan nil.
	if opts.OnScan != nil {
		mux.HandleFunc("/api/scan", func(w http.ResponseWriter, r *http.Request) {
			d, err := opts.OnScan()
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, d)
		})
	}

	// Desktop-only /api/auth/* — registered only when the desktop host injects the
	// auth callbacks. The CLI (agent edit) leaves them nil, so these routes never
	// exist there and its behavior is unchanged.
	if opts.OnAuthState != nil {
		mux.HandleFunc("/api/auth/status", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, opts.OnAuthState())
		})
	}
	if opts.OnLogin != nil {
		mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
			ch, err := opts.OnLogin()
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, ch)
		})
	}
	if opts.OnLoginPoll != nil {
		mux.HandleFunc("/api/auth/poll", func(w http.ResponseWriter, r *http.Request) {
			st, err := opts.OnLoginPoll()
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, st)
		})
	}
	if opts.OnLogout != nil {
		mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
			if err := opts.OnLogout(); err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, map[string]string{"status": "logged_out"})
		})
	}

	return mux
}

// observed is the running set of skills / MCP servers seen via /api/observe during
// an --observe session. Claude Code PreToolUse hooks POST here concurrently, so
// every access is mutex-guarded.
type observed struct {
	mu         sync.Mutex
	skills     map[string]struct{}
	mcpServers map[string]struct{}
}

func newObserved() *observed {
	return &observed{skills: map[string]struct{}{}, mcpServers: map[string]struct{}{}}
}

func (o *observed) addSkill(name string) {
	if name == "" {
		return
	}
	o.mu.Lock()
	o.skills[name] = struct{}{}
	o.mu.Unlock()
}

func (o *observed) addMCPServer(name string) {
	if name == "" {
		return
	}
	o.mu.Lock()
	o.mcpServers[name] = struct{}{}
	o.mu.Unlock()
}

// snapshot returns the observed names as sorted slices, never nil (so the JSON
// renders [] not null).
func (o *observed) snapshot() (skills, servers []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return keysSorted(o.skills), keysSorted(o.mcpServers)
}

func keysSorted(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// skillNameFrom defensively extracts the skill name from a Skill tool_input:
// prefer "skill", fall back to "name", then the first string value (spike R2 —
// the Skill input schema is not an official contract).
func skillNameFrom(input map[string]interface{}) string {
	if input == nil {
		return ""
	}
	if s, ok := input["skill"].(string); ok && s != "" {
		return s
	}
	if s, ok := input["name"].(string); ok && s != "" {
		return s
	}
	for _, v := range input {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// mcpServerOf splits mcp__<server>__<tool> down to the server name.
func mcpServerOf(toolName string) string {
	rest := strings.TrimPrefix(toolName, "mcp__")
	parts := strings.SplitN(rest, "__", 2)
	return parts[0]
}

// signal delivers a non-blocking nil to done (buffered cap 1; extra sends drop).
func signal(done chan error) {
	select {
	case done <- nil:
	default:
	}
}

func decodeSpec(r *http.Request) (*types.AgentSpec, error) {
	defer r.Body.Close()
	var spec types.AgentSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		return nil, fmt.Errorf("decode spec: %w", err)
	}
	return &spec, nil
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// openBrowser best-effort opens url in the default browser (macOS open /
// Windows start / Linux xdg-open). A failure is non-fatal — the URL is printed.
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	return exec.Command(cmd, args...).Start()
}
