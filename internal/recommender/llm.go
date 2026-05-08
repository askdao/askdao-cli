package recommender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/askdao/askdao-cli/internal/types"
)

// DefaultRecommendPath is the conductor REST path the CLI POSTs to. Per
// decision 9.1 the request goes through conductor (no direct BYOK from the
// CLI); design.md §6.2 documents the endpoint contract.
const DefaultRecommendPath = "/api/v1/cli/recommend"

// DefaultTimeout caps any single conductor recommend call. Generous because
// upstream LLM calls dominate latency.
const DefaultTimeout = 90 * time.Second

// LLMClient is the recommender's thin abstraction over conductor's recommend
// endpoint. ConductorClient is the production HTTP impl; MockClient is the
// hermetic test impl that returns canned data.
type LLMClient interface {
	Recommend(ctx context.Context, req RecommendRequest) (*RecommendResponse, error)
}

// RecommendRequest is the conductor request envelope. It carries the full
// detection.json plus per-provider FrameworkPlan summaries (the recommender
// won't import internal/providers because that risks an import cycle in
// conductor mocks; we accept loose ProviderSummary structs instead).
type RecommendRequest struct {
	AgentName        string                      `json:"agent_name"`
	GoalHint         string                      `json:"goal_hint,omitempty"`
	PreferredHarness string                      `json:"preferred_harness,omitempty"`
	Detection        *types.Detection            `json:"detection"`
	ProviderSummary  []ProviderSummary           `json:"provider_summary,omitempty"`
	Policy           types.DetectedToolRiskHints `json:"policy"`
}

// ProviderSummary mirrors a provider Plan in a JSON-friendly shape. Only the
// fields conductor needs for recommendation are surfaced.
type ProviderSummary struct {
	Name       string                          `json:"name"`
	Frameworks []types.DetectedFramework       `json:"frameworks,omitempty"`
	SystemApt  []types.InferredAptPackage      `json:"system_apt,omitempty"`
	Runtime    ProviderSummaryRuntime          `json:"runtime"`
	Confidence float64                         `json:"confidence,omitempty"`
	External   []types.DetectedExternalService `json:"external_services,omitempty"`
}

// ProviderSummaryRuntime is the version pin hint a provider extracted.
type ProviderSummaryRuntime struct {
	Kind    string `json:"kind"`
	Version string `json:"version,omitempty"`
}

// RecommendResponse is conductor's reply: a fully-populated AgentSpec draft
// plus the reasoning trace for KOL review.
type RecommendResponse struct {
	Spec               types.AgentSpec           `json:"spec"`
	ReasoningSummary   string                    `json:"reasoning_summary,omitempty"`
	ReasoningDecisions []types.ReasoningDecision `json:"reasoning_decisions,omitempty"`
}

// ConductorClient calls the real conductor recommend endpoint over HTTP.
type ConductorClient struct {
	BaseURL    string
	HTTPClient *http.Client
	AuthToken  string // optional bearer; conductor sessions today are open within VPC
}

// NewConductorClient builds a ConductorClient with sensible defaults. baseURL
// must be set (e.g. "https://conductor.askdao.internal").
func NewConductorClient(baseURL string) *ConductorClient {
	return &ConductorClient{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: DefaultTimeout},
	}
}

// Recommend implements LLMClient. The error returned wraps the conductor
// stderr / status code so KOLs see actionable diagnostics in `askdao agent
// init`.
func (c *ConductorClient) Recommend(ctx context.Context, req RecommendRequest) (*RecommendResponse, error) {
	if c.BaseURL == "" {
		return nil, errors.New("recommender: ConductorClient.BaseURL is empty")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("recommender: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+DefaultRecommendPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.AuthToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("recommender: conductor unreachable: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("recommender: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("recommender: conductor returned %d: %s",
			resp.StatusCode, truncate(string(respBody), 240))
	}
	var out RecommendResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("recommender: decode response: %w", err)
	}
	if out.Spec.APIVersion != types.AgentSpecAPIVersion {
		return nil, fmt.Errorf("recommender: unexpected apiVersion %q (want %q)",
			out.Spec.APIVersion, types.AgentSpecAPIVersion)
	}
	return &out, nil
}

// MockClient returns canned data without calling out. The default canned
// response is a minimal-but-valid AgentSpec built from the request context.
// Tests can override Override to inject specific responses.
type MockClient struct {
	Override func(req RecommendRequest) (*RecommendResponse, error)
}

// Recommend implements LLMClient.
func (m *MockClient) Recommend(_ context.Context, req RecommendRequest) (*RecommendResponse, error) {
	if m.Override != nil {
		return m.Override(req)
	}
	return DefaultMockRecommend(req), nil
}

// DefaultMockRecommend builds a deterministic AgentSpec using whatever
// information the request carries. Used by both MockClient and the
// httptest-based ConductorClient unit test as the server-side stub so
// development can proceed before conductor #11 ships.
func DefaultMockRecommend(req RecommendRequest) *RecommendResponse {
	spec := types.AgentSpec{
		APIVersion: types.AgentSpecAPIVersion,
		Kind:       types.AgentSpecKind,
		Metadata: types.Metadata{
			Name:        req.AgentName,
			Version:     "0.1.0",
			Visibility:  "private",
			PersonaFile: "persona.md",
		},
		Persona: types.Persona{
			ModelClass: "balanced",
			ModelPreferences: []types.ModelPreference{
				{Provider: "anthropic", ID: "claude-sonnet-4-6", Speed: "standard"},
			},
			SystemPrompt: "You are an AI assistant for " + req.AgentName + ".",
		},
		Capabilities: types.Capabilities{
			Shell:         types.Capability{Enabled: true, Permission: defaultShellPolicy(req.Policy)},
			Filesystem:    types.Capability{Enabled: true, Permission: "allow"},
			Web:           types.Capability{Enabled: true, Permission: "allow"},
			CodeExecution: types.Capability{Enabled: true, Permission: "allow"},
		},
		MCPServers:       extractCompatibleMCPServers(req.Detection),
		Skills:           []types.Skill{},
		Workspace:        buildWorkspace(req),
		VaultHints:       buildVaultHints(req.Detection),
		PreferredHarness: harnessFor(req),
	}

	resp := &RecommendResponse{
		Spec:             spec,
		ReasoningSummary: "Mock recommendation: heuristics-only, no LLM call. Replace with conductor endpoint when ready.",
	}
	if len(req.ProviderSummary) > 0 {
		resp.ReasoningDecisions = append(resp.ReasoningDecisions, types.ReasoningDecision{
			Decision:   "selected_runtime=" + req.ProviderSummary[0].Runtime.Kind,
			Reason:     "Provider with highest detection confidence drives runtime hint.",
			Confidence: req.ProviderSummary[0].Confidence,
		})
	}
	if len(req.Policy.ProductionSignals) > 0 {
		resp.ReasoningDecisions = append(resp.ReasoningDecisions, types.ReasoningDecision{
			Decision:   "shell.permission=ask_for_dangerous",
			Reason:     "Production signals detected → bash gated.",
			Confidence: 0.9,
		})
	}
	return resp
}

// defaultShellPolicy mirrors the policy heuristic so the mock response is
// internally consistent with what an LLM would have done.
func defaultShellPolicy(p types.DetectedToolRiskHints) string {
	if len(p.ProductionSignals) > 0 {
		return "ask_for_dangerous"
	}
	return "allow"
}

func extractCompatibleMCPServers(d *types.Detection) []types.MCPServer {
	if d == nil {
		return nil
	}
	var out []types.MCPServer
	for _, cfg := range d.DetectedMCPConfigs {
		for _, s := range cfg.Servers {
			if !s.AnthropicCompatible {
				continue
			}
			out = append(out, types.MCPServer{Name: s.Name, Type: s.Type, URL: s.URL})
		}
	}
	return out
}

func buildWorkspace(req RecommendRequest) types.Workspace {
	ws := types.Workspace{
		Workdir: "/app",
		Networking: types.Networking{
			Mode:                 "limited",
			AllowMCPServers:      true,
			AllowPackageManagers: false,
			AllowedHosts:         []string{"api.anthropic.com", "api.openai.com"},
		},
		EnvironmentVars: map[string]string{},
	}

	pip := []string{}
	npm := []string{}
	if req.Detection != nil {
		for _, p := range req.Detection.DetectedPackages["pip"] {
			if p.IsProd {
				pip = append(pip, p.Name+"=="+p.Version)
			}
		}
		for _, p := range req.Detection.DetectedPackages["npm"] {
			if p.IsProd {
				npm = append(npm, p.Name+"@"+p.Version)
			}
		}
	}

	apt := map[string]bool{}
	for _, ps := range req.ProviderSummary {
		for _, a := range ps.SystemApt {
			apt[a.Name] = true
		}
	}
	if req.Detection != nil {
		for _, a := range req.Detection.InferredAptPackages {
			apt[a.Name] = true
		}
	}
	aptList := make([]string, 0, len(apt))
	for k := range apt {
		aptList = append(aptList, k)
	}

	ws.Packages = types.WorkspacePackages{Pip: pip, Npm: npm, Apt: aptList}
	return ws
}

func buildVaultHints(d *types.Detection) types.VaultHints {
	hints := types.VaultHints{}
	if d == nil {
		return hints
	}
	for _, s := range d.DetectedRequiredSecrets {
		entry := types.VaultCredential{Name: s.Name, Purpose: s.PurposeGuess, From: s.From, Required: s.Required, Note: s.Note}
		if s.UsedByGuess != nil && s.UsedByGuess.MCPServer != "" {
			entry.UsedBy = map[string]interface{}{"mcp_server": s.UsedByGuess.MCPServer}
		}
		if s.Required {
			hints.RequiredCredentials = append(hints.RequiredCredentials, entry)
		} else {
			hints.OptionalCredentials = append(hints.OptionalCredentials, entry)
		}
	}
	return hints
}

func harnessFor(req RecommendRequest) string {
	if req.PreferredHarness != "" {
		return req.PreferredHarness
	}
	if req.Detection != nil && req.Detection.DetectedHarnessSignals.RecommendedHarness != "" {
		return req.Detection.DetectedHarnessSignals.RecommendedHarness
	}
	return "anthropic_managed_agents"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
