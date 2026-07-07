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
	d := BuildStudioData(&types.AgentSpec{Metadata: types.Metadata{Name: "x"}}, det, "Anthropic Managed Agents", false)

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

// TestBuildStudioData_RestorePrior guards issue #1: re-editing an existing yaml
// must tick exactly what the spec declares — not the default policy. The fixture
// inverts every default so a bug (ignoring the spec) can't pass by coincidence.
func TestBuildStudioData_RestorePrior(t *testing.T) {
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
	// Prior selection inverts every default: a user skill + a stdio MCP are ticked
	// (default would drop both), while the default-on project skill / builtin /
	// compatible MCP are absent (default would tick all three).
	spec := &types.AgentSpec{
		Metadata: types.Metadata{Name: "x"},
		Skills: []types.Skill{
			{Type: "custom_local", Path: "/home/u/.claude/skills/glob-b", Scope: "user"},
		},
		MCPServers: []types.MCPServer{
			{Name: "stdio-srv", Type: "stdio"},
		},
	}
	d := BuildStudioData(spec, det, "Anthropic Managed Agents", true)

	wantSkill := map[string]bool{"proj-a": false, "glob-b": true, "xlsx": false}
	for _, c := range d.SkillCandidates {
		if w, ok := wantSkill[c.Name]; ok && c.Checked != w {
			t.Errorf("restore: skill %q checked=%v want %v", c.Name, c.Checked, w)
		}
	}
	wantMCP := map[string]bool{"url-srv": false, "stdio-srv": true}
	for _, c := range d.MCPCandidates {
		if w, ok := wantMCP[c.Name]; ok && c.Checked != w {
			t.Errorf("restore: mcp %q checked=%v want %v", c.Name, c.Checked, w)
		}
	}
}

func TestServerHandlers(t *testing.T) {
	var saved, deployed bool
	done := make(chan error, 1)
	opts := Options{
		Data:   &StudioData{Spec: &types.AgentSpec{Metadata: types.Metadata{Name: "x"}}},
		OnSave: func(*types.AgentSpec) error { saved = true; return nil },
		OnDeploy: func(*types.AgentSpec) (*DeployResult, error) {
			deployed = true
			return &DeployResult{Message: "Created agent agt_1", GroupLink: "https://askdao.ai/g/x", AgentID: "agt_1", Created: true}, nil
		},
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
	var dep map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&dep)
	if dep["group_link"] != "https://askdao.ai/g/x" || dep["agent_id"] != "agt_1" || dep["created"] != true {
		t.Errorf("/api/deploy must surface structured group_link/agent_id/created: %v", dep)
	}
	select {
	case <-done:
	default:
		t.Errorf("/api/deploy should signal done")
	}
}

func TestDoneSavesSpec(t *testing.T) {
	var savedSpec *types.AgentSpec
	done := make(chan error, 1)
	opts := Options{
		Data:   &StudioData{Spec: &types.AgentSpec{Metadata: types.Metadata{Name: "x"}}},
		OnSave: func(s *types.AgentSpec) error { savedSpec = s; return nil },
	}
	srv := httptest.NewServer(buildMux(opts, done))
	defer srv.Close()

	// Save & finish must persist edits (display_name/avatar) before exiting,
	// otherwise deploy reads a stale yaml missing those fields.
	body := `{"metadata":{"name":"y","display_name":"Y","avatar":"icon:star"}}`
	r, err := http.Post(srv.URL+"/api/done", "application/json", bytes.NewBufferString(body))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("/api/done status=%v err=%v", r.StatusCode, err)
	}
	if savedSpec == nil {
		t.Fatal("/api/done must call OnSave to persist edits")
	}
	if savedSpec.Metadata.DisplayName != "Y" || savedSpec.Metadata.Avatar != "icon:star" {
		t.Errorf("/api/done dropped display_name/avatar: %+v", savedSpec.Metadata)
	}
	select {
	case <-done:
	default:
		t.Errorf("/api/done should signal done")
	}
}

func TestScanRoutes(t *testing.T) {
	var picked, ran, cancelled bool
	done := make(chan error, 1)
	opts := Options{
		OnScanPick:   func() (string, error) { picked = true; return "/tmp/proj", nil },
		OnScanRun:    func() (*StudioData, error) { ran = true; return &StudioData{Spec: &types.AgentSpec{Metadata: types.Metadata{Name: "scanned"}}}, nil },
		OnScanCancel: func() error { cancelled = true; return nil },
	}
	srv := httptest.NewServer(buildMux(opts, done))
	defer srv.Close()

	// pick returns the chosen dir immediately (frontend shows it before scanning).
	r, err := http.Post(srv.URL+"/api/scan/pick", "application/json", nil)
	if err != nil || r.StatusCode != 200 || !picked {
		t.Fatalf("/api/scan/pick status=%v picked=%v err=%v", r.StatusCode, picked, err)
	}
	var pick map[string]string
	_ = json.NewDecoder(r.Body).Decode(&pick)
	if pick["dir"] != "/tmp/proj" {
		t.Errorf("/api/scan/pick must return {dir}: %v", pick)
	}

	// run scans the pending folder and returns fresh StudioData.
	r, _ = http.Post(srv.URL+"/api/scan/run", "application/json", nil)
	if r.StatusCode != 200 || !ran {
		t.Errorf("/api/scan/run not handled: status=%d ran=%v", r.StatusCode, ran)
	}
	var data StudioData
	_ = json.NewDecoder(r.Body).Decode(&data)
	if data.Spec == nil || data.Spec.Metadata.Name != "scanned" {
		t.Errorf("/api/scan/run did not return scanned StudioData: %+v", data.Spec)
	}

	// cancel aborts the in-flight run.
	r, _ = http.Post(srv.URL+"/api/scan/cancel", "application/json", nil)
	if r.StatusCode != 200 || !cancelled {
		t.Errorf("/api/scan/cancel not handled: status=%d cancelled=%v", r.StatusCode, cancelled)
	}
}

// scan routes must NOT exist when the callbacks are nil (the CLI agent-edit path).
func TestScanRoutesAbsentForCLI(t *testing.T) {
	done := make(chan error, 1)
	srv := httptest.NewServer(buildMux(Options{Data: &StudioData{}}, done))
	defer srv.Close()
	for _, p := range []string{"/api/scan/pick", "/api/scan/run", "/api/scan/cancel"} {
		r, _ := http.Post(srv.URL+p, "application/json", nil)
		if r.StatusCode != 404 {
			t.Errorf("%s should be 404 when callback nil, got %d", p, r.StatusCode)
		}
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
		`{"tool_name":"Skill","tool_input":{"skill":"content-generator"}}`,
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

	if want := []string{"browse", "content-generator"}; !reflect.DeepEqual(got.Skills, want) {
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

func TestDefaultAvatarForCategory(t *testing.T) {
	cases := map[string]string{
		"education":  "icon:graduation-cap",
		"finance":    "icon:trending-up",
		"tech":       "icon:code",
		"health":     "icon:heart-pulse",
		"creative":   "icon:palette",
		"design":     "icon:palette",
		"business":   "icon:briefcase",
		"data":       "icon:database",
		"lifestyle":  "icon:leaf",
		"  Finance ": "icon:trending-up", // trim + lowercase
		"FINANCE":    "icon:trending-up",
		"unknown":    "icon:bot", // fallback
		"":           "icon:bot",
	}
	for cat, want := range cases {
		if got := DefaultAvatarForCategory(cat); got != want {
			t.Errorf("DefaultAvatarForCategory(%q) = %q, want %q", cat, got, want)
		}
	}
}

// TestCategoryDefaultIconsInWhitelist guards that every category default icon
// (and the "bot" fallback) is a real lucide name in AvatarIcons — otherwise the
// frontend can't render it. Prevents drift when editing categoryDefaultIcon.
func TestCategoryDefaultIconsInWhitelist(t *testing.T) {
	valid := map[string]bool{}
	for _, ic := range AvatarIcons {
		valid[ic.Name] = true
	}
	for cat, ic := range categoryDefaultIcon {
		if !valid[ic] {
			t.Errorf("categoryDefaultIcon[%q]=%q not in AvatarIcons whitelist", cat, ic)
		}
	}
	if !valid["bot"] {
		t.Error(`fallback icon "bot" not in AvatarIcons whitelist`)
	}
}
