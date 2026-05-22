package webstudio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestBuildStudioData_DefaultSelection(t *testing.T) {
	det := &types.Detection{
		DetectedSkills: []types.DetectedSkill{
			{SkillName: "proj-a", Source: ".claude/skills/proj-a/SKILL.md", Scope: "project", Harness: "claude", IsLocalOriginal: true},
			{SkillName: "glob-b", Source: "/home/u/.claude/skills/glob-b/SKILL.md", Scope: "user", Harness: "claude", IsLocalOriginal: true},
			{ImpliedAnthropicSkills: []types.ImpliedAnthropicSkill{{SkillID: "xlsx"}}},
		},
		DetectedMCPConfigs: []types.DetectedMCPConfig{
			{Source: ".mcp.json", Scope: "project", Servers: []types.MCPServerConfig{
				{Name: "url-srv", Type: "url", AnthropicCompatible: true},
				{Name: "stdio-srv", Type: "stdio", AnthropicCompatible: false},
			}},
		},
	}
	d := BuildStudioData(&types.AgentSpec{Metadata: types.Metadata{Name: "x"}}, det, "Anthropic Managed Agents")

	wantSkill := map[string]bool{"proj-a": true, "glob-b": false, "xlsx": true}
	seen := map[string]bool{}
	for _, c := range d.SkillCandidates {
		seen[c.Name] = true
		if w, ok := wantSkill[c.Name]; ok && c.Checked != w {
			t.Errorf("skill %q checked=%v want %v", c.Name, c.Checked, w)
		}
	}
	for k := range wantSkill {
		if !seen[k] {
			t.Errorf("skill candidate %q missing", k)
		}
	}
	for _, c := range d.MCPCandidates {
		if c.Name == "url-srv" && !c.Checked {
			t.Errorf("url MCP should default checked")
		}
		if c.Name == "stdio-srv" && c.Checked {
			t.Errorf("stdio MCP should default unchecked")
		}
	}
}

func TestServerHandlers(t *testing.T) {
	var saved, deployed bool
	done := make(chan error, 1)
	opts := Options{
		Data:     &StudioData{Spec: &types.AgentSpec{Metadata: types.Metadata{Name: "x"}}},
		OnSave:   func(*types.AgentSpec) error { saved = true; return nil },
		OnDeploy: func(*types.AgentSpec) (string, error) { deployed = true; return "Created agent agt_1", nil },
	}
	srv := httptest.NewServer(buildMux(opts, done))
	defer srv.Close()

	r, err := http.Get(srv.URL + "/api/spec")
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("/api/spec status=%v err=%v", r.StatusCode, err)
	}
	var data StudioData
	_ = json.NewDecoder(r.Body).Decode(&data)
	if data.Spec == nil || data.Spec.Metadata.Name != "x" {
		t.Errorf("/api/spec did not return the spec")
	}

	body := `{"metadata":{"name":"y"}}`
	r, _ = http.Post(srv.URL+"/api/save", "application/json", bytes.NewBufferString(body))
	if r.StatusCode != 200 || !saved {
		t.Errorf("/api/save not handled: status=%d saved=%v", r.StatusCode, saved)
	}

	r, _ = http.Post(srv.URL+"/api/deploy", "application/json", bytes.NewBufferString(body))
	if r.StatusCode != 200 || !deployed {
		t.Errorf("/api/deploy not handled: status=%d deployed=%v", r.StatusCode, deployed)
	}
	select {
	case <-done:
	default:
		t.Errorf("/api/deploy should signal done")
	}
}

func TestObserveEndpoint(t *testing.T) {
	done := make(chan error, 1)
	srv := httptest.NewServer(buildMux(Options{Data: &StudioData{}}, done))
	defer srv.Close()

	post := func(body string) int {
		r, err := http.Post(srv.URL+"/api/observe", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatalf("POST /api/observe: %v", err)
		}
		r.Body.Close()
		return r.StatusCode
	}

	cases := []string{
		`{"tool_name":"Skill","tool_input":{"skill":"spelling-homework-generator"}}`,
		`{"tool_name":"mcp__askdao-voice__elevenlabs_text_to_speech","tool_input":{}}`,
		`{"tool_name":"mcp__askdao-voice__elevenlabs_search_voices","tool_input":{}}`, // same server -> deduped
		`{"tool_name":"Skill","tool_input":{"name":"browse"}}`,                        // R2: fall back to name
		`{"tool_name":"Bash","tool_input":{"command":"ls"}}`,                          // non-target -> ignored
		`{"tool_name":"Skill","tool_input":{}}`,                                       // no name -> ignored
		`not even json`,                                                               // garbage -> still 200
	}
	for _, c := range cases {
		if code := post(c); code != 200 {
			t.Errorf("POST must always 200 (non-blocking), got %d for %s", code, c)
		}
	}

	r, _ := http.Get(srv.URL + "/api/observe")
	var got ObservedData
	_ = json.NewDecoder(r.Body).Decode(&got)
	r.Body.Close()

	if want := []string{"browse", "spelling-homework-generator"}; !reflect.DeepEqual(got.Skills, want) {
		t.Errorf("skills = %v, want %v", got.Skills, want)
	}
	if want := []string{"askdao-voice"}; !reflect.DeepEqual(got.MCPServers, want) {
		t.Errorf("mcp_servers = %v, want %v", got.MCPServers, want)
	}
}

func TestObserveConcurrent(t *testing.T) {
	done := make(chan error, 1)
	srv := httptest.NewServer(buildMux(Options{Data: &StudioData{}}, done))
	defer srv.Close()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"tool_name":"Skill","tool_input":{"skill":"skill-%d"}}`, i)
			if r, err := http.Post(srv.URL+"/api/observe", "application/json", bytes.NewBufferString(body)); err == nil {
				r.Body.Close()
			}
		}(i)
	}
	wg.Wait()

	r, _ := http.Get(srv.URL + "/api/observe")
	var got ObservedData
	_ = json.NewDecoder(r.Body).Decode(&got)
	r.Body.Close()
	if len(got.Skills) != n {
		t.Errorf("concurrent POST: got %d skills, want %d", len(got.Skills), n)
	}
}

// TestServeOnReady verifies OnReady fires with the bound port before Serve blocks.
func TestServeOnReady(t *testing.T) {
	gotPort := make(chan int, 1)
	go func() {
		_ = Serve(Options{
			Data:      &StudioData{},
			NoBrowser: true,
			OnReady:   func(port int) { gotPort <- port },
		})
	}()
	select {
	case port := <-gotPort:
		if port <= 0 {
			t.Errorf("OnReady port = %d, want > 0", port)
		}
		// Unblock the backgrounded Serve so it shuts down cleanly.
		if r, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/api/done", port), "application/json", nil); err == nil {
			r.Body.Close()
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady did not fire within 5s")
	}
}
