// [INPUT]: 标准库 net/http / mime/multipart / encoding/json / context / io / time / bytes / errors / fmt
// [OUTPUT]: Client（Deploy / SetupKol）+ DeployInput / DeployResponse / TranslationReport / TranslationWarning / KolProfilePatch / KolProfileRequired / VisibilityDowngrade + ErrKolProfileRequired / ErrBlockingWarnings / ErrVisibilityDowngradeConfirm + DefaultDeployPath / DefaultKolProfilePath / DefaultTimeout
// [POS]: internal/deploy 的服务端 HTTP 客户端 —— `askdao agent deploy` 调 POST /api/v1/cli/deploy（multipart/form-data）+ PATCH /api/v1/users/me/kol-profile；cmd/askdao/deploy.go 消费；DeployResponse 与 409 形态对齐服务端契约（CI diff 校验）
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	// DefaultDeployPath is the conductor REST path for `askdao agent deploy`.
	DefaultDeployPath = "/api/v1/cli/deploy"
	// DefaultKolProfilePath is the conductor REST path for the KOL profile PATCH.
	DefaultKolProfilePath = "/api/v1/users/me/kol-profile"
	// DefaultTimeout caps a single deploy call. Generous because a deploy syncs
	// custom skills server-side and creates an Anthropic environment + agent —
	// all behind one synchronous request.
	DefaultTimeout = 180 * time.Second
)

// DeployInput is the multipart/form-data payload for POST /api/v1/cli/deploy.
type DeployInput struct {
	// AgentYAML is the raw agent.yml bytes — sent verbatim, NOT re-marshaled, so
	// KOL comments / field order / unknown-to-CLI fields survive (the server's
	// spec model ignores unknown fields for forward compat).
	AgentYAML []byte
	// Detection is the raw detection.json bytes; optional (nil/empty → omitted).
	Detection []byte
	// HarnessID overrides the harness; optional (server defaults to
	// anthropic_managed_agents).
	HarnessID string
	// Force deploys even if the translation report has blocking (REJECTED /
	// deploy-fatal) warnings.
	Force bool
	// ConfirmVisibilityDowngrade acknowledges taking an approved shared/public
	// agent private (the conductor 409s with
	// visibility_downgrade_requires_confirm otherwise). Distinct from Force,
	// which only overrides translation warnings.
	ConfirmVisibilityDowngrade bool
	// SkillZips maps a form file-field name (the basename of a custom_local
	// skill's path) to the zip bytes of that skill's directory.
	SkillZips map[string][]byte
}

// DeployResponse mirrors the server's deploy response contract.
//
// Update-mode:
//   - Created=true  → first deploy under this (owner, yaml.metadata.name).
//   - Created=false → in-place update of an existing agent; AgentID and
//     GroupID are reused, PreviousManagedVersion records the pre-bump
//     Anthropic agent version (e.g. 1 → 2).
type DeployResponse struct {
	AgentID                string                   `json:"agent_id"`
	AnthropicAgentID       string                   `json:"anthropic_agent_id"`
	AnthropicEnvironmentID string                   `json:"anthropic_environment_id"`
	GroupID                string                   `json:"group_id"`
	GroupLink              string                   `json:"group_link"`
	Skills                 []map[string]interface{} `json:"skills"`
	TranslationReport      TranslationReport        `json:"translation_report"`
	Created                bool                     `json:"created"`
	PreviousManagedVersion *int                     `json:"previous_managed_version,omitempty"`
	// ScheduleWarning (#303): non-empty when the schedule block fires more
	// often than every 15 minutes — a cost-risk hint, never a deploy blocker.
	// cmd layer must surface it prominently.
	ScheduleWarning string `json:"schedule_warning,omitempty"`
}

// TranslationReport mirrors conductor's app.agents.adapters.TranslationReport
// JSON shape. severity/action are lowercase enum *values* (the conductor model
// sets use_enum_values=True), e.g. "high" / "ignored".
type TranslationReport struct {
	Harness             string               `json:"harness"`
	TranslationWarnings []TranslationWarning `json:"translation_warnings"`
}

// TranslationWarning is one lossy-translation event from the adapter.
type TranslationWarning struct {
	Field             string      `json:"field"`
	Severity          string      `json:"severity"` // "high" | "medium" | "low"
	Action            string      `json:"action"`   // "ignored" | "partial" | "rejected"
	Reason            string      `json:"reason"`
	Value             interface{} `json:"value,omitempty"`
	Count             *int        `json:"count,omitempty"`
	FallbackAttempted *string     `json:"fallback_attempted,omitempty"`
}

// KolProfilePatch is the JSON body for PATCH /api/v1/users/me/kol-profile.
// Only the fields the CLI sets are listed; the conductor accepts more.
type KolProfilePatch struct {
	Name        string `json:"name,omitempty"`
	Image       string `json:"image,omitempty"`
	KolJoinMode string `json:"kol_join_mode,omitempty"`
	KolBio      string `json:"kol_bio,omitempty"`
}

// KolProfileRequired is the parsed `detail` of a 409 kol_profile_required.
// SetupURL is the server-authoritative page that clears the gate (M4) —
// callers render it when present and fall back to a hardcoded URL only for
// older conductors, so web route changes never strand the CLI hint again.
type KolProfileRequired struct {
	Reason   string   `json:"reason"`
	Fields   []string `json:"fields"`
	Hint     string   `json:"hint"`
	SetupURL string   `json:"setup_url"`
}

// ErrKolProfileRequired is returned by Deploy when the conductor needs the
// caller's KOL profile filled in before deploying. Detect with errors.As.
type ErrKolProfileRequired struct {
	Detail KolProfileRequired
}

func (e *ErrKolProfileRequired) Error() string {
	if e.Detail.Hint != "" {
		return "conductor requires KOL profile before deploy: " + e.Detail.Hint
	}
	return "conductor requires KOL profile before deploy"
}

// VisibilityDowngrade is the parsed `detail` of a 409
// visibility_downgrade_requires_confirm — the conductor refuses to take an
// approved shared/public agent private without an explicit acknowledgement.
type VisibilityDowngrade struct {
	Reason              string `json:"reason"`
	CurrentVisibility   string `json:"current_visibility"`
	RequestedVisibility string `json:"requested_visibility"`
	AgentName           string `json:"agent_name"`
}

// ErrVisibilityDowngradeConfirm is returned by Deploy when the agent.yml
// explicitly sets visibility: private on an agent that is currently approved
// and publicly serving (shared/public). Callers warn the user about the
// consequences (subscribers and showcase pages lose access immediately; going
// public again requires platform re-review) and, once confirmed, retry with
// DeployInput.ConfirmVisibilityDowngrade=true. Detect with errors.As.
type ErrVisibilityDowngradeConfirm struct {
	Detail VisibilityDowngrade
}

func (e *ErrVisibilityDowngradeConfirm) Error() string {
	name := e.Detail.AgentName
	if name == "" {
		name = "this agent"
	}
	return fmt.Sprintf(
		"taking %s private requires confirmation (currently %s and approved); re-run with --confirm-downgrade",
		name, e.Detail.CurrentVisibility,
	)
}

// ErrBlockingWarnings is returned by Deploy when the translation report has
// blocking (REJECTED / deploy-fatal) warnings and DeployInput.Force was not
// set. Severity does not gate deploy — only TranslationAction.REJECTED does.
// Detect with errors.As.
type ErrBlockingWarnings struct {
	Report TranslationReport
}

func (e *ErrBlockingWarnings) Error() string {
	return fmt.Sprintf(
		"translation report has blocking (deploy-fatal) warnings (%d total); re-run with --force to deploy anyway",
		len(e.Report.TranslationWarnings),
	)
}

// Client calls the conductor `agent deploy` endpoints over HTTP.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	// AuthToken is the bearer token. Required for deploy — the conductor
	// requires authentication on /cli/deploy and /users/me/kol-profile.
	AuthToken string
}

// NewClient builds a Client with the default timeout. baseURL must be set
// (e.g. "https://api.askdao.ai").
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTPClient: &http.Client{Timeout: DefaultTimeout}}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: DefaultTimeout}
}

// Deploy POSTs the agent spec (+ optional detection + custom-skill zips) to
// /api/v1/cli/deploy as multipart/form-data and returns the conductor's
// DeployResponse. On a 409 it returns a typed *ErrKolProfileRequired or
// *ErrBlockingWarnings; other non-2xx responses become a generic error that
// includes the status code and (truncated) body.
func (c *Client) Deploy(ctx context.Context, in DeployInput) (*DeployResponse, error) {
	if c.BaseURL == "" {
		return nil, errors.New("deploy: Client.BaseURL is empty")
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("agent_yaml", string(in.AgentYAML)); err != nil {
		return nil, err
	}
	if len(in.Detection) > 0 {
		if err := mw.WriteField("detection", string(in.Detection)); err != nil {
			return nil, err
		}
	}
	if in.HarnessID != "" {
		if err := mw.WriteField("harness_id", in.HarnessID); err != nil {
			return nil, err
		}
	}
	if in.Force {
		if err := mw.WriteField("force", "true"); err != nil {
			return nil, err
		}
	}
	if in.ConfirmVisibilityDowngrade {
		if err := mw.WriteField("confirm_visibility_downgrade", "true"); err != nil {
			return nil, err
		}
	}
	for name, zipBytes := range in.SkillZips {
		fw, err := mw.CreateFormFile(name, name+".zip")
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(zipBytes); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+DefaultDeployPath, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("deploy: conductor unreachable: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("deploy: read response: %w", err)
	}

	if resp.StatusCode == http.StatusConflict {
		if e := classifyConflict(respBody); e != nil {
			return nil, e
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("deploy: conductor returned %d: %s", resp.StatusCode, truncate(string(respBody), 400))
	}
	var out DeployResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("deploy: decode response: %w", err)
	}
	return &out, nil
}

// conflictDetail captures the two known 409 detail shapes the conductor emits
// from /cli/deploy: kol_profile_required and blocking (REJECTED) warnings.
type conflictDetail struct {
	Reason              string             `json:"reason"`
	Fields              []string           `json:"fields"`
	Hint                string             `json:"hint"`
	SetupURL            string             `json:"setup_url"`
	CurrentVisibility   string             `json:"current_visibility"`
	RequestedVisibility string             `json:"requested_visibility"`
	AgentName           string             `json:"agent_name"`
	TranslationReport   *TranslationReport `json:"translation_report"`
}

// classifyConflict parses a FastAPI 409 body ({"detail": {...}}) into a typed
// error, or returns nil if the body doesn't match a known shape — in which case
// the caller falls back to a generic error that surfaces the raw body.
func classifyConflict(body []byte) error {
	var env struct {
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(body, &env); err != nil || len(env.Detail) == 0 {
		return nil
	}
	var d conflictDetail
	if err := json.Unmarshal(env.Detail, &d); err != nil {
		return nil // detail was a string or some other non-object shape
	}
	switch {
	case d.Reason == "kol_profile_required":
		return &ErrKolProfileRequired{Detail: KolProfileRequired{
			Reason: d.Reason, Fields: d.Fields, Hint: d.Hint, SetupURL: d.SetupURL,
		}}
	case d.Reason == "visibility_downgrade_requires_confirm":
		return &ErrVisibilityDowngradeConfirm{Detail: VisibilityDowngrade{
			Reason:              d.Reason,
			CurrentVisibility:   d.CurrentVisibility,
			RequestedVisibility: d.RequestedVisibility,
			AgentName:           d.AgentName,
		}}
	case d.TranslationReport != nil:
		return &ErrBlockingWarnings{Report: *d.TranslationReport}
	default:
		return nil
	}
}

// SetupKol PATCHes the caller's KOL profile (used by `agent deploy` to fill in
// kol_join_mode / kol_bio when the conductor returns kol_profile_required).
func (c *Client) SetupKol(ctx context.Context, patch KolProfilePatch) error {
	if c.BaseURL == "" {
		return errors.New("deploy: Client.BaseURL is empty")
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.BaseURL+DefaultKolProfilePath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("deploy: conductor unreachable: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("deploy: set KOL profile returned %d: %s", resp.StatusCode, truncate(string(respBody), 400))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
