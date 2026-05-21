package webstudio

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
