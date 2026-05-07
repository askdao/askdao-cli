// [INPUT]: 依赖 internal/types 的 AgentSpec schema、testdata 下 fixtures、
//          标准库 bytes/os/reflect/testing、gopkg.in/yaml.v3
// [OUTPUT]: 对外提供 TestAgentSpecRoundTripValid / TestAgentSpecRejectInvalid /
//          TestAgentSpecAPIVersionStamp 测试
// [POS]: internal/types 的 agent.yml schema 验证；保证 fixture YAML
//        marshal→unmarshal→DeepEqual 通，strict 模式拒非法字段
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package types

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestAgentSpecRoundTripValid loads testdata/valid_agent.yml, decodes into
// AgentSpec1, re-encodes to YAML, decodes again into AgentSpec2, and verifies
// DeepEqual. Catches encoding drift between struct tags and the v0.3 schema.
func TestAgentSpecRoundTripValid(t *testing.T) {
	raw, err := os.ReadFile("testdata/valid_agent.yml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var first AgentSpec
	if err := yaml.Unmarshal(raw, &first); err != nil {
		t.Fatalf("first unmarshal: %v", err)
	}

	if first.APIVersion != AgentSpecAPIVersion {
		t.Fatalf("apiVersion = %q, want %q", first.APIVersion, AgentSpecAPIVersion)
	}
	if first.Kind != AgentSpecKind {
		t.Fatalf("kind = %q, want %q", first.Kind, AgentSpecKind)
	}

	encoded, err := yaml.Marshal(&first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var second AgentSpec
	if err := yaml.Unmarshal(encoded, &second); err != nil {
		t.Fatalf("second unmarshal: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		diagnoseSpecDiff(t, first, second)
		t.Fatalf("round-trip mismatch")
	}
}

func diagnoseSpecDiff(t *testing.T, a, b AgentSpec) {
	t.Helper()
	cases := []struct {
		name string
		a, b interface{}
	}{
		{"APIVersion", a.APIVersion, b.APIVersion},
		{"Kind", a.Kind, b.Kind},
		{"Metadata", a.Metadata, b.Metadata},
		{"Persona", a.Persona, b.Persona},
		{"Capabilities", a.Capabilities, b.Capabilities},
		{"MCPServers", a.MCPServers, b.MCPServers},
		{"CustomTools", a.CustomTools, b.CustomTools},
		{"Skills", a.Skills, b.Skills},
		{"Workspace", a.Workspace, b.Workspace},
		{"VaultHints", a.VaultHints, b.VaultHints},
		{"PreferredHarness", a.PreferredHarness, b.PreferredHarness},
		{"FallbackHarnesses", a.FallbackHarnesses, b.FallbackHarnesses},
		{"HarnessSpecific", a.HarnessSpecific, b.HarnessSpecific},
		{"Memory", a.Memory, b.Memory},
		{"Guardrails", a.Guardrails, b.Guardrails},
		{"Provenance", a.Provenance, b.Provenance},
		{"Status", a.Status, b.Status},
	}
	for _, c := range cases {
		if !reflect.DeepEqual(c.a, c.b) {
			t.Logf("DIFF %s:\n  a=%#v\n  b=%#v", c.name, c.a, c.b)
		}
	}
}

// TestAgentSpecRejectInvalid loads testdata/invalid_agent.yml under strict
// decoding (KnownFields true). The fixture carries an unknown top-level key
// the decoder MUST flag.
func TestAgentSpecRejectInvalid(t *testing.T) {
	raw, err := os.ReadFile("testdata/invalid_agent.yml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	var spec AgentSpec
	if err := dec.Decode(&spec); err == nil {
		t.Fatalf("expected strict decode error for unknown top-level field, got nil")
	}
}

// TestAgentSpecAPIVersionStamp pins the apiVersion / kind constants. Bump only
// in coordination with adapters on the conductor side.
func TestAgentSpecAPIVersionStamp(t *testing.T) {
	if AgentSpecAPIVersion != "askdao.ai/v1" {
		t.Fatalf("AgentSpecAPIVersion drifted: %q", AgentSpecAPIVersion)
	}
	if AgentSpecKind != "AgentSpec" {
		t.Fatalf("AgentSpecKind drifted: %q", AgentSpecKind)
	}
}
