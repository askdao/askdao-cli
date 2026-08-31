package recommender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	neturl "net/url"
	"time"

	"github.com/askdao/askdao-cli/internal/types"
)

// DefaultRecommendPath is the conductor REST path the CLI POSTs to. Per
// decision 9.1 the request goes through conductor (no direct BYOK from the
// CLI); design.md §6.2 documents the endpoint contract.
const DefaultRecommendPath = "/api/v1/cli/recommend"

// DefaultModelClassesPath is the conductor REST path for the model-class catalog
// (tier label / concrete model / cost) the studio step-2 selector renders, so
// concrete model ids live only in conductor (zero client re-download on a swap).
const DefaultModelClassesPath = "/api/v1/cli/model-classes"

// DefaultAppConfigPath is the conductor REST path for server-driven client
// config (currently the built-in assistant's agent id) the desktop app reads at
// startup, so that id lives server-side (swap + redeploy conductor, no client
// re-release) instead of a fragile client-side env var.
const DefaultAppConfigPath = "/api/v1/cli/config"

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
	out, err := doJSON[RecommendResponse](ctx, c, http.MethodPost, DefaultRecommendPath, req)
	if err != nil {
		return nil, err
	}
	if out.Spec.APIVersion != types.AgentSpecAPIVersion {
		return nil, fmt.Errorf("recommender: unexpected apiVersion %q (want %q)",
			out.Spec.APIVersion, types.AgentSpecAPIVersion)
	}
	return &out, nil
}

// doJSON is the single HTTP round-trip behind every ConductorClient method:
// BaseURL check → optional JSON body → Accept/Content-Type + optional bearer →
// default client fallback → 2xx check → decode T. Each endpoint method keeps
// only its own path/query and post-decode validation.
func doJSON[T any](ctx context.Context, c *ConductorClient, method, path string, body any) (T, error) {
	var zero T
	if c.BaseURL == "" {
		return zero, errors.New("recommender: ConductorClient.BaseURL is empty")
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return zero, fmt.Errorf("recommender: marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return zero, err
	}
	httpReq.Header.Set("Accept", "application/json")
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if c.AuthToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return zero, fmt.Errorf("recommender: conductor unreachable: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, fmt.Errorf("recommender: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, fmt.Errorf("recommender: conductor returned %d: %s",
			resp.StatusCode, truncate(string(respBody), 240))
	}
	var out T
	if err := json.Unmarshal(respBody, &out); err != nil {
		return zero, fmt.Errorf("recommender: decode response: %w", err)
	}
	return out, nil
}

// fetchOr wraps the graceful degradation shared by the module-level Fetch*Or*
// helpers: no baseURL / any error → fallback; otherwise a 10s-capped call on a
// fresh client.
func fetchOr[T any](ctx context.Context, baseURL, token string, fallback T, fn func(context.Context, *ConductorClient) (T, error)) T {
	if baseURL == "" {
		return fallback
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c := NewConductorClient(baseURL)
	c.AuthToken = token
	v, err := fn(fetchCtx, c)
	if err != nil {
		return fallback
	}
	return v
}

// modelClassesResponse is the GET /cli/model-classes envelope. `models` is the
// admin-maintained whitelist (cloud#84); older conductors omit it (→ nil).
type modelClassesResponse struct {
	Classes []types.ModelClassEntry `json:"classes"`
	Models  []types.ModelEntry      `json:"models"`
}

// ModelCatalogAllHarnesses is the `harness` query value that asks conductor for
// the unified whitelist across both harnesses (studio dropdown: picking a model
// picks the harness).
const ModelCatalogAllHarnesses = "all"

// FetchModelClasses GETs conductor's offered model-class tiers (label /
// concrete model id / derived cost). Mirrors Recommend's request shape.
func (c *ConductorClient) FetchModelClasses(ctx context.Context, harness string) ([]types.ModelClassEntry, error) {
	classes, _, err := c.fetchModelClasses(ctx, harness)
	return classes, err
}

// fetchModelClasses is the shared GET behind FetchModelClasses / FetchModelCatalog.
func (c *ConductorClient) fetchModelClasses(ctx context.Context, harness string) ([]types.ModelClassEntry, []types.ModelEntry, error) {
	path := DefaultModelClassesPath
	if harness != "" && harness != "auto" {
		// 备份运行时（conductor #342）：目录按 harness 返回（openai_agents_sdk → OpenAI 官方 / SiliconFlow 等兼容端点的三档）
		path += "?harness=" + neturl.QueryEscape(harness)
	}
	out, err := doJSON[modelClassesResponse](ctx, c, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	return out.Classes, out.Models, nil
}

// FetchModelCatalog GETs conductor's unified model whitelist (`models[]`,
// ?harness=all): every enabled model across both harnesses with provider /
// harness_id / raw price, sorted most-common-first. Returns nil (no error) when
// conductor predates the field.
func (c *ConductorClient) FetchModelCatalog(ctx context.Context) ([]types.ModelEntry, error) {
	_, models, err := c.fetchModelClasses(ctx, ModelCatalogAllHarnesses)
	return models, err
}

// CronPreview mirrors conductor POST /cli/cron-preview: server-authoritative
// next-run preview (croniter semantics incl. DST) so the studio never re-solves
// cron client-side (cli#78: hand-rolled solvers drift from croniter).
type CronPreview struct {
	NextRuns           []string `json:"next_runs"`
	MinIntervalSeconds float64  `json:"min_interval_seconds"`
	Warning            bool     `json:"warning"`
}

// DefaultCronPreviewPath is conductor's cron preview endpoint.
const DefaultCronPreviewPath = "/api/v1/cli/cron-preview"

// FetchCronPreview POSTs a cron + IANA timezone and returns the next firings,
// min interval and the server-side frequent-schedule warning flag.
func (c *ConductorClient) FetchCronPreview(ctx context.Context, cron, tz string) (*CronPreview, error) {
	out, err := doJSON[CronPreview](ctx, c, http.MethodPost, DefaultCronPreviewPath,
		map[string]string{"cron": cron, "timezone": tz})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// FetchCronPreviewOrNil degrades like the other Fetch*Or* helpers: offline /
// logged-out / invalid cron (conductor 400) → nil, and the studio hides the
// "Next:" preview row (describeCron + raw cron stay, they need no time math).
func FetchCronPreviewOrNil(ctx context.Context, baseURL, token, cron, tz string) *CronPreview {
	return fetchOr[*CronPreview](ctx, baseURL, token, nil,
		func(fctx context.Context, c *ConductorClient) (*CronPreview, error) {
			return c.FetchCronPreview(fctx, cron, tz)
		})
}

// FetchModelCatalogOrEmpty is FetchModelCatalog with the same offline / logged-out
// degradation as FetchModelClassesOrFallback — but the whitelist has no bundled
// fallback (the binary carries no model ids): an empty slice tells the studio to
// render the legacy three-tier picker instead.
func FetchModelCatalogOrEmpty(ctx context.Context, baseURL, token string) []types.ModelEntry {
	return fetchOr(ctx, baseURL, token, nil,
		func(fctx context.Context, c *ConductorClient) ([]types.ModelEntry, error) {
			return c.FetchModelCatalog(fctx)
		})
}

// FetchModelClassesOrFallback fetches conductor's model-class catalog, degrading
// to the bundled minimal fallback (stable labels, NO concrete model ids) when
// conductor is unreachable/unconfigured — so `askdao agent edit` works offline
// and the concrete model is resolved server-side from model_class at deploy.
// harness selects the catalog (anthropic_managed_agents | openai_agents_sdk);
// "" / "auto" falls back to the server default (anthropic).
func FetchModelClassesOrFallback(ctx context.Context, baseURL, token, harness string) []types.ModelClassEntry {
	return fetchOr(ctx, baseURL, token, types.FallbackModelClasses(),
		func(fctx context.Context, c *ConductorClient) ([]types.ModelClassEntry, error) {
			classes, err := c.FetchModelClasses(fctx, harness)
			if err == nil && len(classes) == 0 {
				return nil, errors.New("empty catalog") // 空目录也回退 bundled fallback
			}
			return classes, err
		})
}

// appConfigResponse is the GET /cli/config envelope.
type appConfigResponse struct {
	StudioAssistantAgentID string `json:"studio_assistant_agent_id"`
}

// FetchAppConfig GETs conductor's server-driven client config and returns the
// Studio Assistant agent id (empty string if the server hasn't configured one).
// Mirrors FetchModelClasses's request shape.
func (c *ConductorClient) FetchAppConfig(ctx context.Context) (string, error) {
	out, err := doJSON[appConfigResponse](ctx, c, http.MethodGet, DefaultAppConfigPath, nil)
	if err != nil {
		return "", err
	}
	return out.StudioAssistantAgentID, nil
}

// FetchStudioAssistantID fetches the Studio Assistant agent id from conductor's
// /cli/config, returning "" on ANY failure (unreachable / unconfigured / not
// logged in) so the desktop assistant degrades to static help instead of a
// wrong-agent chat. Mirrors FetchModelClassesOrFallback's graceful shape.
func FetchStudioAssistantID(ctx context.Context, baseURL, token string) string {
	return fetchOr(ctx, baseURL, token, "",
		func(fctx context.Context, c *ConductorClient) (string, error) {
			return c.FetchAppConfig(fctx)
		})
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
// information the request carries. Used by both MockClient (the offline
// fallback when ASKDAO_CONDUCTOR_URL is unset) and the httptest-based
// ConductorClient unit test as the server-side stub.
func DefaultMockRecommend(req RecommendRequest) *RecommendResponse {
	spec := types.AgentSpec{
		APIVersion: types.AgentSpecAPIVersion,
		Kind:       types.AgentSpecKind,
		Metadata: types.Metadata{
			Name:       req.AgentName,
			Version:    "0.1.0",
			Visibility: "private",
		},
		Persona: types.Persona{
			// model_class only — conductor resolves the concrete model id from
			// the model-class catalog (or the studio selector fills it); no
			// hardcoded model id in this binary.
			ModelClass:   "balanced",
			SystemPrompt: "You are an AI assistant for " + req.AgentName + ".",
		},
		Capabilities:     DefaultCapabilities(req.Policy),
		MCPServers:       extractCompatibleMCPServers(req.Detection),
		Skills:           []types.Skill{},
		Workspace:        buildWorkspace(req),
		VaultHints:       BuildVaultHints(req.Detection),
		PreferredHarness: harnessFor(req),
	}
	syncNetworkingFromMCP(&spec)

	resp := &RecommendResponse{
		Spec:             spec,
		ReasoningSummary: "Mock recommendation: heuristics-only, no LLM call. Set ASKDAO_CONDUCTOR_URL for a real LLM-backed recommendation via conductor /cli/recommend.",
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
	return "always_allow"
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

// syncNetworkingFromMCP sets allow_mcp_servers and adds MCP server hostnames
// to allowed_hosts, deduped against any pre-existing entries.
func syncNetworkingFromMCP(spec *types.AgentSpec) {
	hasMCP := len(spec.MCPServers) > 0
	spec.Workspace.Networking.AllowMCPServers = hasMCP
	if !hasMCP {
		return
	}
	existing := map[string]bool{}
	for _, h := range spec.Workspace.Networking.AllowedHosts {
		existing[h] = true
	}
	for _, srv := range spec.MCPServers {
		if srv.URL == "" {
			continue
		}
		u, err := url.Parse(srv.URL)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := u.Hostname()
		if !existing[host] {
			spec.Workspace.Networking.AllowedHosts = append(spec.Workspace.Networking.AllowedHosts, host)
			existing[host] = true
		}
	}
}

// BuildVaultHints turns detected env keys into vault_hints, EXCLUDING config
// params (PurposeGuess == types.UnknownSecretPurpose): only keys that matched a
// credential rule become declared credentials subscribers must provide. Used as
// a deterministic hard-field override in cmd/askdao/edit.go and
// cmd/askdao-studio/app.go so both mock and conductor specs stay credential-only.
func BuildVaultHints(d *types.Detection) types.VaultHints {
	hints := types.VaultHints{}
	if d == nil {
		return hints
	}
	for _, s := range d.DetectedRequiredSecrets {
		if s.PurposeGuess == types.UnknownSecretPurpose {
			continue // configuration parameter, not a credential — never declare it
		}
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
