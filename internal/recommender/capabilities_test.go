package recommender

import (
	"reflect"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestDefaultCapabilities(t *testing.T) {
	caps := DefaultCapabilities(types.DetectedToolRiskHints{})

	// All four enabled — code_execution especially (the LLM used to emit false).
	if !caps.Shell.Enabled || !caps.Filesystem.Enabled || !caps.Web.Enabled || !caps.CodeExecution.Enabled {
		t.Errorf("all capabilities must be enabled: %+v", caps)
	}

	// Regular scope enums, not LLM free text.
	want := map[string][]string{
		"shell":          {"read", "write", "execute"},
		"filesystem":     {"read", "write"},
		"web":            {"fetch"},
		"code_execution": {"javascript", "shell"},
	}
	if !reflect.DeepEqual(caps.Shell.Scopes, want["shell"]) {
		t.Errorf("shell.scopes = %v, want %v", caps.Shell.Scopes, want["shell"])
	}
	if !reflect.DeepEqual(caps.Filesystem.Scopes, want["filesystem"]) {
		t.Errorf("filesystem.scopes = %v, want %v", caps.Filesystem.Scopes, want["filesystem"])
	}
	if !reflect.DeepEqual(caps.Web.Scopes, want["web"]) {
		t.Errorf("web.scopes = %v, want %v", caps.Web.Scopes, want["web"])
	}
	if !reflect.DeepEqual(caps.CodeExecution.Scopes, want["code_execution"]) {
		t.Errorf("code_execution.scopes = %v, want %v", caps.CodeExecution.Scopes, want["code_execution"])
	}

	// Non-prod shell auto-runs; prod shell is gated.
	if caps.Shell.Permission != "always_allow" {
		t.Errorf("non-prod shell.permission = %q, want always_allow", caps.Shell.Permission)
	}
	prod := DefaultCapabilities(types.DetectedToolRiskHints{ProductionSignals: []types.ToolRiskSignal{{}}})
	if prod.Shell.Permission != "ask_for_dangerous" {
		t.Errorf("prod shell.permission = %q, want ask_for_dangerous", prod.Shell.Permission)
	}
	// Non-shell caps always auto-run regardless of policy.
	if prod.Filesystem.Permission != "always_allow" || prod.Web.Permission != "always_allow" || prod.CodeExecution.Permission != "always_allow" {
		t.Errorf("non-shell permission must be always_allow: %+v", prod)
	}
}
