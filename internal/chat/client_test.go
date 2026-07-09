package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseServer returns a test server that streams the given raw SSE text frames
// (each already terminated with the blank line), flushing between them. check,
// if set, inspects the inbound request + decoded body.
func sseServer(t *testing.T, frames []string, check func(r *http.Request, body []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if check != nil {
			check(r, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, f := range frames {
			_, _ = w.Write([]byte(f))
			if fl != nil {
				fl.Flush()
			}
		}
	}))
}

func frameType(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("frame not json: %s (%v)", raw, err)
	}
	return m
}

func TestStream_HappyPath_SkipsHeartbeatAndDispatches(t *testing.T) {
	frames := []string{
		": heartbeat\n\n",
		`data: {"type":"text_delta","text":"Hello"}` + "\n\n",
		`data: {"type":"text_delta","text":" world"}` + "\n\n",
		`data: {"type":"done","stop_reason":"end_turn","sdk_session_id":"sesn_abc","ov_session_id":"ov_1"}` + "\n\n",
	}
	var gotAuth string
	var gotBody []byte
	srv := sseServer(t, frames, func(r *http.Request, body []byte) {
		gotAuth = r.Header.Get("Authorization")
		gotBody = body
	})
	defer srv.Close()

	cl := &Client{BaseURL: srv.URL, HTTPClient: srv.Client(), AuthToken: "cli_tok"}
	var got [][]byte
	err := cl.Stream(context.Background(), Request{Message: "hi", AgentID: "agt_1"}, func(raw []byte) error {
		got = append(got, append([]byte(nil), raw...))
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// heartbeat comment skipped → exactly the 3 data frames survive.
	if len(got) != 3 {
		t.Fatalf("want 3 frames, got %d: %q", len(got), got)
	}
	if m := frameType(t, got[0]); m["type"] != "text_delta" || m["text"] != "Hello" {
		t.Errorf("frame0 = %v", m)
	}
	if m := frameType(t, got[2]); m["type"] != "done" || m["sdk_session_id"] != "sesn_abc" {
		t.Errorf("frame2 = %v", m)
	}
	if gotAuth != "Bearer cli_tok" {
		t.Errorf("auth header = %q", gotAuth)
	}
	var sent Request
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("body not json: %s", gotBody)
	}
	if sent.Message != "hi" || sent.AgentID != "agt_1" || sent.SessionID != "" {
		t.Errorf("sent body = %+v (first turn should omit session_id)", sent)
	}
}

func TestStream_MultiTurn_CarriesSessionID(t *testing.T) {
	var sent Request
	srv := sseServer(t, []string{`data: {"type":"done","stop_reason":"end_turn"}` + "\n\n"}, func(r *http.Request, body []byte) {
		_ = json.Unmarshal(body, &sent)
	})
	defer srv.Close()
	cl := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := cl.Stream(context.Background(), Request{Message: "again", AgentID: "agt_1", SessionID: "sesn_abc"}, func(raw []byte) error { return nil })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if sent.SessionID != "sesn_abc" {
		t.Errorf("second turn session_id = %q, want sesn_abc", sent.SessionID)
	}
}

func TestStream_OnFrameError_StopsEarly(t *testing.T) {
	frames := []string{
		`data: {"type":"text_delta","text":"a"}` + "\n\n",
		`data: {"type":"text_delta","text":"b"}` + "\n\n",
	}
	srv := sseServer(t, frames, nil)
	defer srv.Close()
	cl := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	stop := errors.New("writer closed")
	n := 0
	err := cl.Stream(context.Background(), Request{Message: "x"}, func(raw []byte) error {
		n++
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("want stop error, got %v", err)
	}
	if n != 1 {
		t.Errorf("onFrame called %d times, want 1 (stop after first)", n)
	}
}

func TestStream_NonOK_ReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such agent", http.StatusForbidden)
	}))
	defer srv.Close()
	cl := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := cl.Stream(context.Background(), Request{Message: "x", AgentID: "agt_x"}, func(raw []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "no such agent") {
		t.Fatalf("want 403 body error, got %v", err)
	}
}

func TestStream_EmptyBaseURL(t *testing.T) {
	cl := &Client{}
	err := cl.Stream(context.Background(), Request{Message: "x"}, func(raw []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "BaseURL is empty") {
		t.Fatalf("want empty-baseurl error, got %v", err)
	}
}
