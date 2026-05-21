// [INPUT]: 依赖 标准库 time
// [OUTPUT]: 对外提供 AgentSpec（apiVersion: askdao.ai/v1）及其 sub-types；
//
//	AgentSpecAPIVersion / AgentSpecKind 常量
//
// [POS]: internal/types 的 L4 输出 schema 真相源；中间格式（harness-neutral）
//
//	被 recommender 写入、render 渲染、conductor adapter 消费翻译
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package types

import "time"

// AgentSpec schema constants. AgentSpecAPIVersion is the apiVersion stamp
// every persisted agent.yml must carry; consumers MUST refuse unknown values.
const (
	AgentSpecAPIVersion = "askdao.ai/v1"
	AgentSpecKind       = "AgentSpec"
)

// AgentSpec is the harness-neutral intermediate format produced by
// `askdao agent init --auto` and consumed by conductor-side adapters
// (AnthropicAdapter in Phase 1, OpenAIAdapter in Phase 2).
//
// Eight top-level blocks express semantics; harness_specific is the escape
// hatch for adapter-only fields. memory / guardrails / provenance / status
// are conductor business fields that never reach any harness API.
type AgentSpec struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind"       yaml:"kind"`

	Metadata     Metadata     `json:"metadata"     yaml:"metadata"`
	Persona      Persona      `json:"persona"      yaml:"persona"`
	Capabilities Capabilities `json:"capabilities" yaml:"capabilities"`
	MCPServers   []MCPServer  `json:"mcp_servers"  yaml:"mcp_servers"`
	CustomTools  []CustomTool `json:"custom_tools" yaml:"custom_tools"`
	Skills       []Skill      `json:"skills"       yaml:"skills"`
	Workspace    Workspace    `json:"workspace"    yaml:"workspace"`
	VaultHints   VaultHints   `json:"vault_hints"  yaml:"vault_hints"`

	PreferredHarness  string           `json:"preferred_harness"            yaml:"preferred_harness"`
	FallbackHarnesses []string         `json:"fallback_harnesses,omitempty" yaml:"fallback_harnesses,omitempty"`
	HarnessSpecific   *HarnessSpecific `json:"harness_specific,omitempty"   yaml:"harness_specific,omitempty"`

	Memory     *Memory     `json:"memory,omitempty"     yaml:"memory,omitempty"`
	Guardrails *Guardrails `json:"guardrails,omitempty" yaml:"guardrails,omitempty"`
	Provenance *Provenance `json:"provenance,omitempty" yaml:"provenance,omitempty"`
	Status     *Status     `json:"status,omitempty"     yaml:"status,omitempty"`
}

// Metadata is business identity, harness-agnostic.
//
// As of v0.7 there is no longer a `persona_file` field: the agent's persona
// lives entirely inside `Persona.SystemPrompt` (yaml literal block), giving
// the askdao-agent.yml a single source of truth for the agent's voice. See
// docs/design.md §9.15.
type Metadata struct {
	Name        string `json:"name"                      yaml:"name"`
	Description string `json:"description,omitempty"     yaml:"description,omitempty"`
	Version     string `json:"version"                   yaml:"version"`
	// Visibility ∈ {private, shared, public} (askdao-cli#28 / spec/02 §1.2 line 48):
	//   private — owner-only (default; subscribers can't list / chat)
	//   shared  — KOL 旗下订阅者可见 + 可调（默认订阅范围）
	//   public  — 开放发现（订阅者 + 未来广场可索引）
	// Conductor 端 schema (RecommendRequest / AgentSpecIn / UpdateVisibilityRequest)
	// 三档 pattern 校验；Web 工作台改值走 PATCH，CLI 改值走 deploy 重跑.
	Visibility     string   `json:"visibility,omitempty"      yaml:"visibility,omitempty"`
	ExpertiseLevel string   `json:"expertise_level,omitempty" yaml:"expertise_level,omitempty"`
	Domain         []string `json:"domain,omitempty"          yaml:"domain,omitempty"`
	// Category is the agent's product category (education / finance / health …).
	// It drives the default theme palette in the web studio and the
	// subscriber-facing Group page. Free-form, but the studio offers a preset list.
	Category string `json:"category,omitempty" yaml:"category,omitempty"`
	// ThemeColor is a preset palette TOKEN (e.g. "sunset"), NOT a raw hex value.
	// CLI / conductor / askdao-ai-web share one token→color table so the
	// subscriber Group page (/k/{kol}/g/{group}) renders the brand color. The KOL
	// picks it in the web studio; it defaults from Category.
	ThemeColor string            `json:"theme_color,omitempty" yaml:"theme_color,omitempty"`
	GroupName  string            `json:"group_name,omitempty"  yaml:"group_name,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"      yaml:"labels,omitempty"`
}

// Persona is the model + role semantic layer. ModelPreferences are tried in
// order; adapter picks the first whose runtime is supported.
type Persona struct {
	ModelClass       string            `json:"model_class"             yaml:"model_class"` // high_reasoning | balanced | fast | multimodal | coding
	ModelPreferences []ModelPreference `json:"model_preferences"       yaml:"model_preferences"`
	SystemPrompt     string            `json:"system_prompt,omitempty" yaml:"system_prompt,omitempty"`
}

// ModelPreference is one (provider, id, speed) tuple in the priority list.
type ModelPreference struct {
	Provider string `json:"provider"        yaml:"provider"` // anthropic | openai | google | ...
	ID       string `json:"id"              yaml:"id"`
	Speed    string `json:"speed,omitempty" yaml:"speed,omitempty"` // standard | fast
}

// Capabilities are semantic abilities + permission policy. Adapters translate
// these to provider-specific tool configs.
type Capabilities struct {
	Shell         Capability `json:"shell"          yaml:"shell"`
	Filesystem    Capability `json:"filesystem"     yaml:"filesystem"`
	Web           Capability `json:"web"            yaml:"web"`
	CodeExecution Capability `json:"code_execution" yaml:"code_execution"`
}

// Capability is the (enabled, permission, optional scopes) tuple shared by all
// capability slots.
type Capability struct {
	Enabled    bool     `json:"enabled"           yaml:"enabled"`
	Permission string   `json:"permission"        yaml:"permission"` // allow | always_allow | always_ask | ask_for_dangerous
	Scopes     []string `json:"scopes,omitempty"  yaml:"scopes,omitempty"`
}

// MCPServer is a standard Model Context Protocol server entry. Both adapters
// pass these through 1:1.
type MCPServer struct {
	Name    string `json:"name"              yaml:"name"`
	Type    string `json:"type"              yaml:"type"` // url | stdio
	URL     string `json:"url,omitempty"     yaml:"url,omitempty"`
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
}

// CustomTool is a schema-neutral tool declaration; the handler shape is
// adapter-specific (Anthropic tools[type=custom] vs OpenAI function_tool).
type CustomTool struct {
	Name        string                 `json:"name"                  yaml:"name"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Schema      map[string]interface{} `json:"schema,omitempty"      yaml:"schema,omitempty"`
	Handler     map[string]interface{} `json:"handler,omitempty"     yaml:"handler,omitempty"`
}

// Skill comes from one of three sources: builtin (provider-hosted), local
// custom directory, or git_repo reference.
// Skill is one entry of AgentSpec.skills.
//
//   - Type=builtin: Anthropic-hosted skill (xlsx, pdf, etc). Provider=anthropic,
//     ID=short name. No upload required.
//   - Type=custom_local: a directory in the KOL project. Path is the skill
//     directory's path **relative to the KOL project root** (e.g.
//     ".agents/skills/tts"). At deploy time the entire directory is recursively
//     zipped — SKILL.md plus scripts/, assets/, references/ subdirs and all
//     binary files — and uploaded. The zip's top-level dir is filepath.Base(Path)
//     so the upstream harness directory (.claude/skills/, .agents/skills/, ...)
//     is *not* leaked into the archive (harness-neutral invariant, see
//     docs/design.md §9.14).
//   - Type=git_repo: harness-agnostic concept (a skill fetched from GitHub at
//     runtime). NOT supported by Anthropic Managed Agents (no public skill
//     registry; see harness-design/investigations/managed-agents-skill-installation.md).
//     Kept in the schema for future runtimes (E2B sandbox + Claude Agent SDK
//     etc.) that may consume this shape. The Anthropic adapter emits a HIGH
//     translation warning and skips the entry.
type Skill struct {
	Type     string `json:"type"               yaml:"type"`               // builtin | custom_local | git_repo
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"` // anthropic | ...
	ID       string `json:"id,omitempty"       yaml:"id,omitempty"`       // builtin only
	Path     string `json:"path,omitempty"     yaml:"path,omitempty"`     // custom_local: skill dir path
	// Scope is "project" (default; Path is relative to the project root) or
	// "user" (a global skill under the home dir, e.g. ~/.claude/skills/<name>;
	// Path is absolute or ~-prefixed). deploy resolves Path accordingly before ZipDir.
	Scope string `json:"scope,omitempty"    yaml:"scope,omitempty"` // project | user
	Repo  string `json:"repo,omitempty"     yaml:"repo,omitempty"`  // git_repo only
	Ref   string `json:"ref,omitempty"      yaml:"ref,omitempty"`   // git_repo only
}

// Workspace is the runtime environment configuration. The v0.4 fields
// (BaseImage / Workdir / SetupCommands / Users / ExposedPorts / StartupCommand)
// are consumed by OpenAI adapter and IGNORED+warned by Anthropic adapter via
// the translation report.
type Workspace struct {
	BaseImage      string          `json:"base_image"                yaml:"base_image"`
	Workdir        string          `json:"workdir,omitempty"         yaml:"workdir,omitempty"`
	SetupCommands  []string        `json:"setup_commands,omitempty"  yaml:"setup_commands,omitempty"`
	Users          []WorkspaceUser `json:"users,omitempty"           yaml:"users,omitempty"`
	ExposedPorts   []int           `json:"exposed_ports,omitempty"   yaml:"exposed_ports,omitempty"`
	StartupCommand string          `json:"startup_command"           yaml:"startup_command"`

	Packages        WorkspacePackages `json:"packages,omitempty"         yaml:"packages,omitempty"`
	Mounts          []WorkspaceMount  `json:"mounts,omitempty"           yaml:"mounts,omitempty"`
	Networking      Networking        `json:"networking"                 yaml:"networking"`
	EnvironmentVars map[string]string `json:"environment_vars,omitempty" yaml:"environment_vars,omitempty"`
}

// WorkspaceUser is one OS user/group declaration. Anthropic ignores; OpenAI
// translates to Manifest.users.
type WorkspaceUser struct {
	Name string `json:"name"          yaml:"name"`
	UID  *int   `json:"uid,omitempty" yaml:"uid,omitempty"`
	GID  *int   `json:"gid,omitempty" yaml:"gid,omitempty"`
}

// WorkspacePackages buckets package names by package manager. Both adapters
// consume.
type WorkspacePackages struct {
	Pip   []string `json:"pip,omitempty"   yaml:"pip,omitempty"`
	Apt   []string `json:"apt,omitempty"   yaml:"apt,omitempty"`
	Npm   []string `json:"npm,omitempty"   yaml:"npm,omitempty"`
	Cargo []string `json:"cargo,omitempty" yaml:"cargo,omitempty"`
	Go    []string `json:"go,omitempty"    yaml:"go,omitempty"`
}

// WorkspaceMount is an external data mount. OpenAI Manifest GitRepo / S3Mount;
// Anthropic ignores + warns.
type WorkspaceMount struct {
	Type   string `json:"type"             yaml:"type"` // git_repo | s3 | local
	Repo   string `json:"repo,omitempty"   yaml:"repo,omitempty"`
	Ref    string `json:"ref,omitempty"    yaml:"ref,omitempty"`
	Bucket string `json:"bucket,omitempty" yaml:"bucket,omitempty"`
	Key    string `json:"key,omitempty"    yaml:"key,omitempty"`
	Dest   string `json:"dest"             yaml:"dest"`
	Mode   string `json:"mode,omitempty"   yaml:"mode,omitempty"` // ro | rw
}

// Networking is the egress policy for the agent runtime.
type Networking struct {
	Mode                 string   `json:"mode"                          yaml:"mode"` // limited | unrestricted
	AllowedHosts         []string `json:"allowed_hosts,omitempty"       yaml:"allowed_hosts,omitempty"`
	AllowMCPServers      bool     `json:"allow_mcp_servers"             yaml:"allow_mcp_servers"`
	AllowPackageManagers bool     `json:"allow_package_managers"        yaml:"allow_package_managers"`
}

// VaultHints declares secrets the subscriber must (or may) provide during
// onboarding. Values never live in the spec; the per-user Vault stores them.
type VaultHints struct {
	RequiredCredentials []VaultCredential `json:"required_credentials,omitempty" yaml:"required_credentials,omitempty"`
	OptionalCredentials []VaultCredential `json:"optional_credentials,omitempty" yaml:"optional_credentials,omitempty"`
}

// VaultCredential is one onboarding-time secret prompt. UsedBy is shape-free to
// allow {mcp_server: "github"} / {agent: true} / {tool: "..."}.
type VaultCredential struct {
	Name     string                 `json:"name"               yaml:"name"`
	Purpose  string                 `json:"purpose,omitempty"  yaml:"purpose,omitempty"`
	UsedBy   map[string]interface{} `json:"used_by,omitempty"  yaml:"used_by,omitempty"`
	From     string                 `json:"from,omitempty"     yaml:"from,omitempty"`
	Required bool                   `json:"required"           yaml:"required"`
	Note     string                 `json:"note,omitempty"     yaml:"note,omitempty"`
}

// HarnessSpecific is the escape hatch for adapter-only fields the neutral
// schema can not express. Each adapter only reads its own provider block.
type HarnessSpecific struct {
	Anthropic *AnthropicSpecific `json:"anthropic,omitempty" yaml:"anthropic,omitempty"`
	OpenAI    *OpenAISpecific    `json:"openai,omitempty"    yaml:"openai,omitempty"`
}

// AnthropicSpecific is the Anthropic Managed Agents-only field bag.
type AnthropicSpecific struct {
	FastMode       bool              `json:"fast_mode,omitempty"       yaml:"fast_mode,omitempty"`
	CallableAgents []string          `json:"callable_agents,omitempty" yaml:"callable_agents,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"        yaml:"metadata,omitempty"`
}

// OpenAISpecific is the OpenAI Agents SDK-only field bag (Phase 2).
type OpenAISpecific struct {
	SandboxProvider string      `json:"sandbox_provider,omitempty" yaml:"sandbox_provider,omitempty"` // docker | e2b | modal | unix_local | daytona | vercel
	Compaction      *Compaction `json:"compaction,omitempty"        yaml:"compaction,omitempty"`
}

// Compaction is the OpenAI session-state compaction policy.
type Compaction struct {
	Enabled         bool `json:"enabled"           yaml:"enabled"`
	TriggerAtTokens int  `json:"trigger_at_tokens" yaml:"trigger_at_tokens"`
}

// Memory is conductor-side memory policy (never reaches harness API).
type Memory struct {
	FactExtraction string `json:"fact_extraction,omitempty" yaml:"fact_extraction,omitempty"` // enabled | disabled
	EpisodeSummary string `json:"episode_summary,omitempty" yaml:"episode_summary,omitempty"`
}

// Guardrails is conductor-side filtering policy (never reaches harness API).
type Guardrails struct {
	CredentialFilter string `json:"credential_filter,omitempty" yaml:"credential_filter,omitempty"`
	KOLMemoryRedact  string `json:"kol_memory_redact,omitempty" yaml:"kol_memory_redact,omitempty"`
}

// Provenance carries the why-trail for transparency: which detection report,
// what the LLM reasoned, and a per-decision confidence breakdown.
type Provenance struct {
	DetectionReport    string              `json:"detection_report,omitempty"   yaml:"detection_report,omitempty"`
	ReasoningSummary   string              `json:"reasoning_summary,omitempty"  yaml:"reasoning_summary,omitempty"`
	ReasoningDecisions []ReasoningDecision `json:"reasoning_decisions,omitempty" yaml:"reasoning_decisions,omitempty"`
	GeneratedAt        time.Time           `json:"generated_at"                  yaml:"generated_at"`
	GeneratorVersion   string              `json:"generator_version"             yaml:"generator_version"`
}

// ReasoningDecision is one LLM-attributed decision with confidence.
type ReasoningDecision struct {
	Decision   string  `json:"decision"   yaml:"decision"`
	Reason     string  `json:"reason"     yaml:"reason"`
	Confidence float64 `json:"confidence" yaml:"confidence"`
}

// Status is written back by `askdao agent deploy`. Each adapter populates the
// remote_ids key matching its runtime.
type Status struct {
	LastAppliedAt      *time.Time      `json:"last_applied_at"     yaml:"last_applied_at"`
	ActiveHarness      string          `json:"active_harness"      yaml:"active_harness"`
	RemoteIDs          StatusRemoteIDs `json:"remote_ids"          yaml:"remote_ids"`
	VaultSetupComplete bool            `json:"vault_setup_complete" yaml:"vault_setup_complete"`
	DriftDetected      bool            `json:"drift_detected"      yaml:"drift_detected"`
}

// StatusRemoteIDs is the union of per-runtime remote-ID slots; only the keys
// matching the active adapter are populated.
type StatusRemoteIDs struct {
	AnthropicAgentID       string `json:"anthropic_agent_id"       yaml:"anthropic_agent_id"`
	AnthropicAgentVersion  string `json:"anthropic_agent_version"  yaml:"anthropic_agent_version"`
	AnthropicEnvironmentID string `json:"anthropic_environment_id" yaml:"anthropic_environment_id"`
	OpenAISessionStateID   string `json:"openai_session_state_id"  yaml:"openai_session_state_id"`
}
