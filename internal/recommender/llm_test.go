package recommender

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func sampleRequest() RecommendRequest {
	return RecommendRequest{
		AgentName:        "demo",
		PreferredHarness: "anthropic_managed_agents",
		Detection: &types.Detection{
			SchemaVersion: types.DetectionSchemaVersion,
			DetectedPackages: map[string][]types.Package{
				"pip": {
					{Name: "fastapi", Version: "0.135.1", IsProd: true},
					{Name: "pytest", Version: "9.0.2", IsProd: false}, // dev → must not surface
				},
			},
			InferredAptPackages: []types.InferredAptPackage{
				{Name: "libpq-dev", Reason: "asyncpg"},
			},
			DetectedRequiredSecrets: []types.DetectedRequiredSecret{
				{Name: "GITHUB_TOKEN", Required: true,
					UsedByGuess: &types.SecretUsedByGuess{MCPServer: "github"}},
				{Name: "DATABASE_URL", Required: false},
			},
			DetectedMCPConfigs: []types.DetectedMCPConfig{
				{Source: ".mcp.json", Servers: []types.MCPServerConfig{
					{Name: "github", Type: "url", URL: "https://api.githubcopilot.com/mcp/", AnthropicCompatible: true},
					{Name: "filesystem", Type: "stdio", AnthropicCompatible: false},
				}},
			},
		},
		ProviderSummary: []ProviderSummary{
			{
				Name:       "python",
				Runtime:    ProviderSummaryRuntime{Kind: "python", Version: "3.12"},
				Frameworks: []types.DetectedFramework{{Name: "FastAPI", Confidence: 0.9}},
				Confidence: 0.9,
				SystemApt:  []types.InferredAptPackage{{Name: "libpq-dev", Reason: "asyncpg"}},
			},
		},
		Policy: types.DetectedToolRiskHints{
			RecommendedDefaultPolicy: "always_allow",
			ProductionSignals:        []types.ToolRiskSignal{{Signal: "deploy.yml", Evidence: ".github/workflows/deploy.yml"}},
			ToolOverridesRecommended: []types.ToolPolicyOverride{
				{Tool: "bash", Policy: "always_ask"},
			},
		},
	}
}

func TestMockClient_DefaultRecommend(t *testing.T) {
	mock := &MockClient{}
	resp, err := mock.Recommend(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if resp.Spec.APIVersion != types.AgentSpecAPIVersion {
		t.Errorf("apiVersion = %q", resp.Spec.APIVersion)
	}
	if resp.Spec.Metadata.Name != "demo" {
		t.Errorf("metadata.name = %q", resp.Spec.Metadata.Name)
	}
	if resp.Spec.PreferredHarness != "anthropic_managed_agents" {
		t.Errorf("preferred_harness = %q", resp.Spec.PreferredHarness)
	}

	// shell policy should follow production signal heuristic.
	if got := resp.Spec.Capabilities.Shell.Permission; got != "ask_for_dangerous" {
		t.Errorf("shell.permission = %q, want ask_for_dangerous", got)
	}

	// Only fastapi (prod) should be in pip; dev pytest filtered out.
	pip := resp.Spec.Workspace.Packages.Pip
	if len(pip) != 1 || pip[0] != "fastapi==0.135.1" {
		t.Errorf("pip = %+v, want [fastapi==0.135.1]", pip)
	}

	// MCP servers: only the url-typed github server makes it through (stdio
	// filesystem is filtered as Anthropic-incompatible).
	if len(resp.Spec.MCPServers) != 1 || resp.Spec.MCPServers[0].Name != "github" {
		t.Errorf("mcp_servers = %+v", resp.Spec.MCPServers)
	}

	// Vault hints split: GITHUB_TOKEN required, DATABASE_URL optional.
	if len(resp.Spec.VaultHints.RequiredCredentials) != 1 ||
		resp.Spec.VaultHints.RequiredCredentials[0].Name != "GITHUB_TOKEN" {
		t.Errorf("required creds = %+v", resp.Spec.VaultHints.RequiredCredentials)
	}
	if resp.Spec.VaultHints.RequiredCredentials[0].UsedBy["mcp_server"] != "github" {
		t.Errorf("UsedBy.mcp_server cross-link missing: %+v",
			resp.Spec.VaultHints.RequiredCredentials[0].UsedBy)
	}
	if len(resp.Spec.VaultHints.OptionalCredentials) != 1 ||
		resp.Spec.VaultHints.OptionalCredentials[0].Name != "DATABASE_URL" {
		t.Errorf("optional creds = %+v", resp.Spec.VaultHints.OptionalCredentials)
	}

	// libpq-dev should appear via InferredAptPackages.
	hasLibpq := false
	for _, a := range resp.Spec.Workspace.Packages.Apt {
		if a == "libpq-dev" {
			hasLibpq = true
		}
	}
	if !hasLibpq {
		t.Errorf("apt list missing libpq-dev: %+v", resp.Spec.Workspace.Packages.Apt)
	}

	// Reasoning trace should include both runtime + policy decisions.
	if len(resp.ReasoningDecisions) < 2 {
		t.Errorf("expected ≥2 reasoning decisions, got %+v", resp.ReasoningDecisions)
	}
}

func TestBuildVaultHints_ExcludesConfigParams(t *testing.T) {
	det := &types.Detection{
		DetectedRequiredSecrets: []types.DetectedRequiredSecret{
			{Name: "OPENAI_API_KEY", PurposeGuess: "OpenAI API authentication", Required: true},
			{Name: "DATABASE_URL", PurposeGuess: "PostgreSQL connection string", Required: false},
			{Name: "DEFAULT_LANGUAGE", PurposeGuess: types.UnknownSecretPurpose, Required: false},
			{Name: "DALLE3_SIZE", PurposeGuess: types.UnknownSecretPurpose, Required: false},
		},
	}
	hints := BuildVaultHints(det)
	// Keys matching a credential rule are kept (split by required); config params
	// carrying UnknownSecretPurpose are dropped entirely.
	if len(hints.RequiredCredentials) != 1 || hints.RequiredCredentials[0].Name != "OPENAI_API_KEY" {
		t.Errorf("required = %+v, want [OPENAI_API_KEY]", hints.RequiredCredentials)
	}
	if len(hints.OptionalCredentials) != 1 || hints.OptionalCredentials[0].Name != "DATABASE_URL" {
		t.Errorf("optional = %+v, want [DATABASE_URL]", hints.OptionalCredentials)
	}
	all := append(append([]types.VaultCredential{}, hints.RequiredCredentials...), hints.OptionalCredentials...)
	for _, c := range all {
		if c.Name == "DEFAULT_LANGUAGE" || c.Name == "DALLE3_SIZE" {
			t.Errorf("config param %q leaked into vault_hints", c.Name)
		}
	}
}

func TestMockClient_OverrideRespected(t *testing.T) {
	called := false
	mock := &MockClient{Override: func(req RecommendRequest) (*RecommendResponse, error) {
		called = true
		return &RecommendResponse{Spec: types.AgentSpec{
			APIVersion: types.AgentSpecAPIVersion,
			Kind:       types.AgentSpecKind,
			Metadata:   types.Metadata{Name: req.AgentName + "-override"},
		}}, nil
	}}
	resp, err := mock.Recommend(context.Background(), RecommendRequest{AgentName: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("override was not invoked")
	}
	if resp.Spec.Metadata.Name != "x-override" {
		t.Errorf("metadata.name = %q", resp.Spec.Metadata.Name)
	}
}

func TestConductorClient_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DefaultRecommendPath {
			t.Errorf("path = %q, want %q", r.URL.Path, DefaultRecommendPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header = %q", got)
		}
		var req RecommendRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// Use the same canned-builder the MockClient uses so the test exercises
		// the JSON shape without needing a separate response fixture.
		resp := DefaultMockRecommend(req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewConductorClient(server.URL)
	client.AuthToken = "test-token"
	resp, err := client.Recommend(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if resp.Spec.Metadata.Name != "demo" {
		t.Errorf("metadata.name = %q", resp.Spec.Metadata.Name)
	}
}

func TestConductorClient_Non2xxIsErrorWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("recommend: detection.schema_version unknown"))
	}))
	defer server.Close()

	client := NewConductorClient(server.URL)
	_, err := client.Recommend(context.Background(), sampleRequest())
	if err == nil {
		t.Fatal("expected error from 400")
	}
	if got := err.Error(); !contains(got, "400") || !contains(got, "schema_version unknown") {
		t.Errorf("error should surface status + body, got %q", got)
	}
}

func TestConductorClient_RejectsBadAPIVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		spec := types.AgentSpec{APIVersion: "askdao.ai/v999", Kind: types.AgentSpecKind}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RecommendResponse{Spec: spec})
	}))
	defer server.Close()

	client := NewConductorClient(server.URL)
	_, err := client.Recommend(context.Background(), sampleRequest())
	if err == nil || !contains(err.Error(), "apiVersion") {
		t.Errorf("expected apiVersion error, got %v", err)
	}
}

func TestConductorClient_EmptyBaseURL(t *testing.T) {
	client := &ConductorClient{}
	_, err := client.Recommend(context.Background(), sampleRequest())
	if err == nil {
		t.Error("empty BaseURL should error")
	}
}

func TestFetchModelClasses_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DefaultModelClassesPath {
			t.Errorf("path = %q, want %q", r.URL.Path, DefaultModelClassesPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(modelClassesResponse{Classes: []types.ModelClassEntry{
			{Slug: "balanced", Label: "Balanced", ModelID: "claude-sonnet-5",
				FriendlyName: "Claude Sonnet 5", CostTier: "moderate", Recommended: true},
		}})
	}))
	defer server.Close()

	client := NewConductorClient(server.URL)
	client.AuthToken = "test-token"
	classes, err := client.FetchModelClasses(context.Background())
	if err != nil {
		t.Fatalf("FetchModelClasses: %v", err)
	}
	if len(classes) != 1 || classes[0].ModelID != "claude-sonnet-5" || !classes[0].Recommended {
		t.Errorf("classes = %+v", classes)
	}
}

func TestFetchModelClassesOrFallback_FallbackOnEmptyBaseURL(t *testing.T) {
	// No conductor configured → bundled fallback (stable labels, NO model ids).
	classes := FetchModelClassesOrFallback(context.Background(), "", "")
	if len(classes) != 3 {
		t.Fatalf("fallback should have 3 tiers, got %d", len(classes))
	}
	for _, c := range classes {
		if c.ModelID != "" {
			t.Errorf("fallback tier %q must carry no concrete model id, got %q", c.Slug, c.ModelID)
		}
	}
	if !classes[1].Recommended || classes[1].Slug != "balanced" {
		t.Errorf("fallback recommended tier = %+v, want balanced", classes[1])
	}
}

func TestFetchModelClassesOrFallback_FallbackOnServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	classes := FetchModelClassesOrFallback(context.Background(), server.URL, "")
	if len(classes) != 3 || classes[0].Slug != "high_reasoning" {
		t.Errorf("server error should degrade to fallback, got %+v", classes)
	}
}

func TestFetchAppConfig_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DefaultAppConfigPath {
			t.Errorf("path = %q, want %q", r.URL.Path, DefaultAppConfigPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(appConfigResponse{StudioAssistantAgentID: "agt_test123"})
	}))
	defer server.Close()

	client := NewConductorClient(server.URL)
	client.AuthToken = "test-token"
	id, err := client.FetchAppConfig(context.Background())
	if err != nil {
		t.Fatalf("FetchAppConfig: %v", err)
	}
	if id != "agt_test123" {
		t.Errorf("id = %q, want agt_test123", id)
	}
}

func TestFetchStudioAssistantID_EmptyOnNoBaseURL(t *testing.T) {
	// Not logged in / no server → empty so the desktop assistant degrades.
	if id := FetchStudioAssistantID(context.Background(), "", ""); id != "" {
		t.Errorf("no baseURL should return empty, got %q", id)
	}
}

func TestFetchStudioAssistantID_EmptyOnServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	if id := FetchStudioAssistantID(context.Background(), server.URL, "tok"); id != "" {
		t.Errorf("server error should degrade to empty, got %q", id)
	}
}

func TestFetchStudioAssistantID_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(appConfigResponse{StudioAssistantAgentID: "agt_xyz"})
	}))
	defer server.Close()
	if id := FetchStudioAssistantID(context.Background(), server.URL, "tok"); id != "agt_xyz" {
		t.Errorf("id = %q, want agt_xyz", id)
	}
}

func TestDefaultMockRecommend_NoHardcodedModelID(t *testing.T) {
	// Regression: the offline mock must set only model_class, never a hardcoded
	// concrete model id — conductor resolves it from model_class at deploy time.
	resp := DefaultMockRecommend(sampleRequest())
	if resp.Spec.Persona.ModelClass != "balanced" {
		t.Errorf("model_class = %q, want balanced", resp.Spec.Persona.ModelClass)
	}
	if len(resp.Spec.Persona.ModelPreferences) != 0 {
		t.Errorf("mock must not hardcode model_preferences, got %+v", resp.Spec.Persona.ModelPreferences)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
