// [INPUT]: 标准库 (context/embed/encoding/json/fmt/net/net/http/os/exec/runtime/time) + internal/types
// [OUTPUT]: 对外提供 Options / Serve；包内 buildMux / openBrowser
// [POS]: webstudio 的本地 HTTP server —— 绑 127.0.0.1:随机端口，serve go:embed 的 studio.html +
//
//	/api/spec(GET) /api/save /api/deploy /api/done；写 yaml / deploy 由 cmd 层注入回调解耦；
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
	"time"

	"github.com/askdao/askdao-cli/internal/types"
)

//go:embed studio.html
var studioHTML []byte

//go:embed logo.png
var logoPNG []byte

// Options drives one studio session. Data is the payload served to the browser;
// OnSave persists the edited spec (write askdao-agent.yml); OnDeploy persists +
// pushes to conductor and returns a human-readable result line. Both are
// injected by the cmd layer so webstudio stays free of pipeline/deploy deps.
type Options struct {
	Data      *StudioData
	OnSave    func(*types.AgentSpec) error
	OnDeploy  func(*types.AgentSpec) (string, error)
	NoBrowser bool
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

// buildMux wires the studio routes. done is signaled (closed-over) when the KOL
// confirms via /api/deploy (success) or /api/done. Extracted from Serve so
// httptest can exercise the handlers without binding a socket.
func buildMux(opts Options, done chan error) *http.ServeMux {
	mux := http.NewServeMux()

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
		msg := "Deployed."
		if opts.OnDeploy != nil {
			msg, err = opts.OnDeploy(spec)
			if err != nil {
				writeErr(w, err)
				return
			}
		}
		writeJSON(w, map[string]string{"status": "deployed", "message": msg})
		signal(done)
	})

	mux.HandleFunc("/api/done", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "done"})
		signal(done)
	})

	return mux
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
