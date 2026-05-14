// Package types defines the schema contracts shared across askdao-cli
// pipeline stages and persisted on disk.
//
// [INPUT]: 依赖 标准库 encoding/json、time
// [OUTPUT]: 对外提供 Detection（detection.json L1-L3 产物）及其 sub-types
// [POS]: internal/types 的 L1-L3 schema 真相源，被 scanner / recommender / cmd 消费
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package types

import "time"

// DetectionSchemaVersion is the canonical schema version stamp written into
// every persisted detection.json. Consumers MUST compare this string to detect
// breaking-format upgrades.
const DetectionSchemaVersion = "askdao/detection/v1"

// Detection is the structured output of the askdao-cli L1-L3 pipeline (syft
// packages → dev-filter / mcp / skills / secrets / harness signals → nixpacks
// providers + apt_map). It is determinstic, offline-reproducible, and serves as
// the single input to L4 LLM recommendation.
//
// Persisted at <agent_dir>/.askdao/detection.json.
type Detection struct {
	SchemaVersion    string    `json:"schema_version"`
	GeneratedAt      time.Time `json:"generated_at"`
	GeneratorVersion string    `json:"generator_version"`

	Scan                     ScanInfo                  `json:"scan"`
	DetectedLanguages        []DetectedLanguage        `json:"detected_languages"`
	DetectedRuntimes         []DetectedRuntime         `json:"detected_runtimes"`
	DetectedManifests        []DetectedManifest        `json:"detected_manifests"`
	DetectedPackages         map[string][]Package      `json:"detected_packages"`
	DetectedFrameworks       []DetectedFramework       `json:"detected_frameworks"`
	DetectedDockerfile       *DetectedDockerfile       `json:"detected_dockerfile,omitempty"`
	DetectedExternalServices []DetectedExternalService `json:"detected_external_services"`
	DetectedEnvFiles         []DetectedEnvFile         `json:"detected_env_files"`
	InferredAptPackages      []InferredAptPackage      `json:"inferred_apt_packages"`
	RepositoryLayout         RepositoryLayout          `json:"repository_layout"`
	DetectedMCPConfigs       []DetectedMCPConfig       `json:"detected_mcp_configs"`
	DetectedSkills           []DetectedSkill           `json:"detected_skills"`
	DetectedRequiredSecrets  []DetectedRequiredSecret  `json:"detected_required_secrets"`
	DetectedToolRiskHints    DetectedToolRiskHints     `json:"detected_tool_risk_hints"`
	DetectedHarnessSignals   DetectedHarnessSignals    `json:"detected_harness_signals"`

	// Archetype classifies what kind of project this is (code_app vs
	// skill_pipeline vs mixed) so downstream layers know whether the "agent"
	// is a service or a skill bundle. Deterministic; no LLM.
	Archetype ProjectArchetype `json:"archetype"`
	// DeploymentPayload is the explicit answer to "what gets uploaded when this
	// directory is deployed to the cloud" — an include list and an exclude list
	// (with reasons). As of v0.7 every custom skill (both repo-native and
	// vendored from skills-lock.json) ships inline; there is no "reinstall from
	// registry" path, because Anthropic Managed Agents has no public skill
	// registry (see harness-design/investigations/managed-agents-skill-installation.md).
	// Vendored-vs-native distinction lives on DetectedSkill metadata for UI
	// display only.
	DeploymentPayload DeploymentPayload `json:"deployment_payload"`
}

// ProjectArchetype is the deterministic classification of the scanned project.
// Kind is one of "code_app" | "skill_pipeline" | "mixed" | "unknown".
type ProjectArchetype struct {
	Kind       string   `json:"kind"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

// DeploymentPayload is the upload manifest: what files travel with the agent
// and what is deliberately left out (with reasons). As of v0.7 every custom
// skill — repo-native or vendored — ships inline; "vendored" is metadata
// (rendered in bundle UI as `skill (vendored: <source> @ <hash>)`) but does
// NOT change upload behaviour. See docs/design.md §9.10 for the rationale.
type DeploymentPayload struct {
	Includes   []PayloadEntry `json:"includes"`
	Excludes   []PayloadEntry `json:"excludes"`
	TotalBytes int64          `json:"total_bytes"`
	TotalFiles int            `json:"total_files"`
	// IgnoreSources lists which ignore mechanisms actually matched something:
	// "builtin" | ".gitignore" | ".dockerignore" | ".askdaoignore".
	IgnoreSources []string `json:"ignore_sources"`
}

// PayloadEntry is one path in the include or exclude list. For directories,
// Bytes/Files are the recursive totals and Path ends with "/".
type PayloadEntry struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	Files  int    `json:"files"`
	Reason string `json:"reason"`
	// Kind buckets the entry: skill | agent_doc | manifest | source | junk |
	// generated | user_data | vendored | other.
	Kind string `json:"kind"`
}

// ScanInfo records what was scanned and how long it took.
type ScanInfo struct {
	Root           string   `json:"root"`
	IsGitRepo      bool     `json:"is_git_repo"`
	GitRemote      string   `json:"git_remote,omitempty"`
	TotalFiles     int      `json:"total_files"`
	ExcludedPaths  []string `json:"excluded_paths"`
	ScanDurationMS int64    `json:"scan_duration_ms"`
}

// DetectedLanguage is one entry from enry's byte-level language identification.
type DetectedLanguage struct {
	Language   string  `json:"language"`
	Bytes      int64   `json:"bytes"`
	Percentage float64 `json:"percentage"`
	Files      int     `json:"files"`
}

// DetectedRuntime is a runtime declaration parsed from version pin files
// (.nvmrc, .python-version, rust-toolchain.toml, go.mod, ...).
type DetectedRuntime struct {
	Kind       string `json:"kind"` // "python" | "node" | "go" | "rust"
	Version    string `json:"version"`
	Source     string `json:"source"`     // file path
	Constraint string `json:"constraint"` // semver range when present
}

// DetectedManifest is a top-level manifest file with dep counts split by scope.
type DetectedManifest struct {
	Manifest       string `json:"manifest"`
	PackageManager string `json:"package_manager"` // "uv" | "poetry" | "pip" | "npm" | "pnpm" | ...
	Lockfile       string `json:"lockfile,omitempty"`
	DirectProdDeps int    `json:"direct_prod_deps"`
	DirectDevDeps  int    `json:"direct_dev_deps"`
	TransitiveDeps int    `json:"transitive_deps"`
}

// Package is one dependency record produced by syft + dev-filter.
type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	IsProd  bool   `json:"is_prod"`
}

// DetectedFramework is a heuristic match (FastAPI, Next.js, ...) from the
// nixpacks-port providers.
type DetectedFramework struct {
	Name       string   `json:"name"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

// DetectedDockerfile holds the full v0.4 Dockerfile AST plus extracted artifacts
// and Anthropic-compatibility warnings produced by askdao-cli.
type DetectedDockerfile struct {
	Exists bool   `json:"exists"`
	Path   string `json:"path,omitempty"`

	// Full multi-stage AST (v0.4 upgrade).
	Stages         []DockerStage `json:"stages,omitempty"`
	FinalStageName *string       `json:"final_stage_name"`
	BaseImage      string        `json:"base_image,omitempty"`

	// Per-instruction extracted fields.
	RunCommands  []string          `json:"run_commands,omitempty"`
	Users        []DockerUser      `json:"users,omitempty"`
	Workdir      string            `json:"workdir,omitempty"`
	EnvVars      map[string]string `json:"env_vars,omitempty"`
	ExposedPorts []int             `json:"exposed_ports,omitempty"`
	Cmd          []string          `json:"cmd"`
	Entrypoint   []string          `json:"entrypoint"`
	BuildArgs    []string          `json:"build_args"`

	// Auto-extracted artifacts fed into agent.yml workspace.
	ExtractedAptPackages   []string `json:"extracted_apt_packages"`
	ExtractedPipPackages   []string `json:"extracted_pip_packages"`
	ExtractedSetupCommands []string `json:"extracted_setup_commands"`

	// v0.4: per-field Anthropic Managed Agents compatibility warnings.
	AnthropicCompatibleWarnings []DockerCompatWarning `json:"anthropic_compatible_warnings,omitempty"`
}

// DockerStage represents a single FROM ... AS clause and its instructions.
type DockerStage struct {
	From     string          `json:"from"`
	As       *string         `json:"as"` // nullable; null when no AS clause
	Commands []DockerCommand `json:"commands"`
}

// DockerCommand is one Dockerfile instruction tuple.
type DockerCommand struct {
	Instruction string `json:"instruction"`
	Value       string `json:"value"`
}

// DockerUser captures USER directives or RUN useradd extraction results.
type DockerUser struct {
	Name string `json:"name"`
	UID  *int   `json:"uid"`
	GID  *int   `json:"gid"`
}

// DockerCompatWarning is one v0.4 Anthropic-compat issue tied to a field path.
type DockerCompatWarning struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}

// DetectedExternalService is an inferred external dependency (Postgres, Redis,
// third-party APIs, ...) cross-referenced from deps + imports + .env.
type DetectedExternalService struct {
	Service    string   `json:"service"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

// DetectedEnvFile is one .env / .env.example file with declared keys (values
// are NEVER captured).
type DetectedEnvFile struct {
	Path         string   `json:"path"`
	DeclaredKeys []string `json:"declared_keys"`
}

// InferredAptPackage is one apt package suggested by the nixpacks reverse map
// based on detected pip / npm / cargo deps.
type InferredAptPackage struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// RepositoryLayout describes monorepo / single layout and any detected
// workspaces.
type RepositoryLayout struct {
	Layout     string   `json:"layout"` // "single" | "monorepo"
	Workspaces []string `json:"workspaces"`
}

// DetectedMCPConfig is one MCP configuration source (.mcp.json,
// claude_desktop_config.json, ...) with Anthropic-compat tagging on each server.
type DetectedMCPConfig struct {
	Source  string            `json:"source"`
	Servers []MCPServerConfig `json:"servers"`
}

// MCPServerConfig is one MCP server entry with its transport type and the
// Anthropic Managed Agents compatibility verdict.
type MCPServerConfig struct {
	Name                string `json:"name"`
	Type                string `json:"type"` // "url" | "stdio"
	URL                 string `json:"url,omitempty"`
	Command             string `json:"command,omitempty"`
	AnthropicCompatible bool   `json:"anthropic_compatible"`
	Warning             string `json:"warning,omitempty"`
}

// DetectedSkill is a heterogeneous entry: either a concrete skill found on disk
// (Source / SkillName / Kind / SizeBytes) or an inferred-builtin block
// (ImpliedAnthropicSkills). Both shapes share this struct via omitempty.
type DetectedSkill struct {
	Source                 string                  `json:"source,omitempty"`
	SkillName              string                  `json:"skill_name,omitempty"`
	Kind                   string                  `json:"kind,omitempty"` // "custom_local" | ...
	SizeBytes              int64                   `json:"size_bytes,omitempty"`
	ImpliedAnthropicSkills []ImpliedAnthropicSkill `json:"implied_anthropic_skills,omitempty"`

	// Description is the `description:` from the SKILL.md YAML frontmatter, when
	// present — feeds the L4 recommender's primary-vs-supporting judgement.
	Description string `json:"description,omitempty"`
	// BundleBytes / BundleFiles are the recursive totals for the whole skill
	// directory (<dir>/<name>/), not just SKILL.md.
	BundleBytes int64 `json:"bundle_bytes,omitempty"`
	BundleFiles int   `json:"bundle_files,omitempty"`
	// LockedSource is non-empty when this skill is pinned in skills-lock.json
	// (a vendored external dependency); empty means it is repo-native (authored
	// here, must travel with the agent).
	LockedSource string `json:"locked_source,omitempty"`
	// LockedHash is the `computedHash` field passed through from skills-lock.json
	// for the vendored case. Used by bundle UI to render an origin tag like
	// `skill (vendored: marswaveai/skills @ <short-hash>)`. Empty when not vendored.
	LockedHash string `json:"locked_hash,omitempty"`
	// IsLocalOriginal == (LockedSource == ""). Materialized for JSON consumers
	// that would rather read a bool than test emptiness.
	IsLocalOriginal bool `json:"is_local_original"`
}

// ImpliedAnthropicSkill is a builtin skill recommendation derived from project
// dependencies (e.g. pandas+openpyxl → xlsx).
type ImpliedAnthropicSkill struct {
	SkillID    string  `json:"skill_id"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// DetectedRequiredSecret is one secret key inferred from .env.example plus the
// service-mapping heuristics; values are never captured.
type DetectedRequiredSecret struct {
	Name         string             `json:"name"`
	From         string             `json:"from"`
	PurposeGuess string             `json:"purpose_guess"`
	UsedByGuess  *SecretUsedByGuess `json:"used_by_guess"`
	Required     bool               `json:"required"`
	Note         string             `json:"note,omitempty"`
}

// SecretUsedByGuess attributes a secret to a specific consumer (an MCP server,
// the agent runtime itself, ...). At most one field is typically populated.
type SecretUsedByGuess struct {
	MCPServer string `json:"mcp_server,omitempty"`
	Agent     bool   `json:"agent,omitempty"`
	Tool      string `json:"tool,omitempty"`
}

// DetectedToolRiskHints is the tool-permission policy recommendation derived
// from production / user-data signals in the project.
type DetectedToolRiskHints struct {
	ProductionSignals        []ToolRiskSignal     `json:"production_signals"`
	UserDataSignals          []ToolRiskSignal     `json:"user_data_signals"`
	RecommendedDefaultPolicy string               `json:"recommended_default_policy"`
	ToolOverridesRecommended []ToolPolicyOverride `json:"tool_overrides_recommended"`
}

// ToolRiskSignal records one heuristic signal (e.g. AWS deploy workflow found).
type ToolRiskSignal struct {
	Signal   string `json:"signal"`
	Evidence string `json:"evidence"`
}

// ToolPolicyOverride upgrades a single tool from the default policy when risk
// signals warrant it.
type ToolPolicyOverride struct {
	Tool   string `json:"tool"`
	Policy string `json:"policy"` // always_allow | always_ask | ask_for_dangerous
	Reason string `json:"reason"`
}

// DetectedHarnessSignals captures which agent harnesses the user has installed
// locally and what askdao-cli recommends as the deploy target.
type DetectedHarnessSignals struct {
	ClaudeCode           HarnessProbe `json:"claude_code"`
	Codex                HarnessProbe `json:"codex"`
	Cursor               HarnessProbe `json:"cursor"`
	GeminiCLI            HarnessProbe `json:"gemini_cli"`
	RecommendedHarness   string       `json:"recommended_harness"`
	RecommendationReason string       `json:"recommendation_reason"`
}

// HarnessProbe is the result of probing a single harness install footprint.
type HarnessProbe struct {
	Installed bool     `json:"installed"`
	Evidence  []string `json:"evidence"`
}
