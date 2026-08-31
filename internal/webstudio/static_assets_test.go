// [INPUT]: 依赖 net/http/httptest + 同包 buildMux/studioHTML/studioCSS/studioJS
// [OUTPUT]: TestStaticAssetsServed —— studio.html 拆分（css/js 各自 go:embed）后三条静态路由回归
// [POS]: internal/webstudio 静态资产路由测试；守护拆分后 html 引用与路由一一对应
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package webstudio

import (
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticAssetsServed(t *testing.T) {
	mux := buildMux(Options{Data: &StudioData{}}, make(chan error, 1))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cases := []struct {
		path        string
		contentType string
		contains    string
	}{
		{"/", "text/html", `<link rel="stylesheet" href="/studio.css">`},
		{"/", "text/html", `<script src="/studio.js">`},
		{"/studio.css", "text/css", ".chat-card"}, // 第二段内联 style 已并入 css
		{"/studio.js", "text/javascript", "const STEPS"},
	}
	for _, c := range cases {
		resp, err := srv.Client().Get(srv.URL + c.path)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: status %d", c.path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, c.contentType) {
			t.Errorf("GET %s: content-type %q, want prefix %q", c.path, ct, c.contentType)
		}
		if !strings.Contains(string(body), c.contains) {
			t.Errorf("GET %s: body missing %q", c.path, c.contains)
		}
	}

	// html 里不应再有内联 <style>/<script> 块（防止将来又长回单文件）
	html := string(studioHTML)
	if strings.Contains(html, "<style>") {
		t.Error("studio.html still contains an inline <style> block")
	}
	if strings.Contains(html, "<script>") {
		t.Error("studio.html still contains an inline <script> block")
	}
}

// TestNoClientCronSolver guards the B10 convergence: next-run math must come
// from conductor (/api/cron-preview), never a client-side croniter clone
// (cli#78: hand-rolled solvers drift from what actually fires).
func TestNoClientCronSolver(t *testing.T) {
	for _, sym := range []string{"cronNextRuns", "cronFieldSet", "wallInstants", "cronMinIntervalSeconds"} {
		if bytes.Contains(studioJS, []byte(sym)) {
			t.Errorf("studio.js re-grew client cron solver symbol %q", sym)
		}
	}
}
