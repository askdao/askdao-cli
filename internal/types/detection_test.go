// [INPUT]: 依赖 internal/types 的 Detection schema、标准库 encoding/json + testing
// [OUTPUT]: 对外提供 TestDetectionRoundTrip / TestDetectionSchemaVersionStamp 测试
// [POS]: internal/types 的 detection.json schema 验证；保证 marshal→unmarshal→DeepEqual 通
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package types

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// TestDetectionRoundTrip builds a fully-populated Detection in code, marshals
// to JSON and unmarshals back, then verifies DeepEqual. Catches any encoding
// drift between struct tags and the schema.
func TestDetectionRoundTrip(t *testing.T) {
	finalStage := "final"
	uid := 1000
	original := newGoldenDetection(finalStage, uid)

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Detection
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("round-trip mismatch:\noriginal=%+v\ndecoded=%+v", original, decoded)
	}
}

// TestDetectionSchemaVersionStamp pins the schema version constant. Bump only
// in coordination with all consumers (scanner / recommender / conductor).
func TestDetectionSchemaVersionStamp(t *testing.T) {
	if DetectionSchemaVersion != "askdao/detection/v1" {
		t.Fatalf("DetectionSchemaVersion drifted: %q", DetectionSchemaVersion)
	}
}

func newGoldenDetection(finalStage string, uid int) Detection {
	return Detection{
		SchemaVersion:    DetectionSchemaVersion,
		GeneratedAt:      time.Date(2026, 5, 6, 14, 23, 11, 0, time.UTC),
		GeneratorVersion: "askdao-cli/0.1.0",
		Scan: ScanInfo{
			Root:           "/home/kol/workspace/my-fastapi-project",
			IsGitRepo:      true,
			GitRemote:      "github.com/example/my-fastapi-project",
			TotalFiles:     1247,
			ExcludedPaths:  []string{"node_modules/**", ".git/**"},
			ScanDurationMS: 1832,
		},
		DetectedLanguages: []DetectedLanguage{
			{Language: "Python", Bytes: 145239, Percentage: 67.4, Files: 47},
			{Language: "TypeScript", Bytes: 56812, Percentage: 26.4, Files: 23},
		},
		DetectedRuntimes: []DetectedRuntime{
			{Kind: "python", Version: "3.12", Source: "pyproject.toml", Constraint: ">=3.12,<3.14"},
			{Kind: "node", Version: "22", Source: ".nvmrc", Constraint: "22"},
		},
		DetectedManifests: []DetectedManifest{
			{Manifest: "pyproject.toml", PackageManager: "uv", Lockfile: "uv.lock", DirectProdDeps: 28, DirectDevDeps: 14, TransitiveDeps: 247},
		},
		DetectedPackages: map[string][]Package{
			"pip": {
				{Name: "fastapi", Version: "0.135.1", IsProd: true},
				{Name: "pytest", Version: "9.0.2", IsProd: false},
			},
		},
		DetectedFrameworks: []DetectedFramework{
			{Name: "FastAPI", Confidence: 0.95, Evidence: []string{"uses_dep:fastapi", "import_pattern:from fastapi import"}},
		},
		DetectedDockerfile: &DetectedDockerfile{
			Exists: true,
			Path:   "./Dockerfile",
			Stages: []DockerStage{
				{
					From: "python:3.12-slim",
					As:   &finalStage,
					Commands: []DockerCommand{
						{Instruction: "WORKDIR", Value: "/app"},
						{Instruction: "RUN", Value: "pip install --no-cache-dir -r requirements.txt"},
					},
				},
			},
			FinalStageName: &finalStage,
			BaseImage:      "python:3.12-slim",
			RunCommands:    []string{"pip install --no-cache-dir -r requirements.txt"},
			Users: []DockerUser{
				{Name: "appuser", UID: &uid, GID: nil},
			},
			Workdir:                "/app",
			EnvVars:                map[string]string{"PYTHONUNBUFFERED": "1"},
			ExposedPorts:           []int{8000},
			Cmd:                    []string{"uvicorn", "app.main:app"},
			Entrypoint:             nil,
			BuildArgs:              []string{},
			ExtractedAptPackages:   []string{"libpq-dev", "gcc"},
			ExtractedPipPackages:   []string{},
			ExtractedSetupCommands: []string{"rm -rf /var/lib/apt/lists/*"},
			AnthropicCompatibleWarnings: []DockerCompatWarning{
				{Field: "users", Issue: "USER appuser ignored; Anthropic runs as fixed user"},
			},
		},
		DetectedExternalServices: []DetectedExternalService{
			{Service: "PostgreSQL", Confidence: 0.95, Evidence: []string{"uses_dep:asyncpg"}},
		},
		DetectedEnvFiles: []DetectedEnvFile{
			{Path: ".env.example", DeclaredKeys: []string{"ANTHROPIC_API_KEY", "DATABASE_URL"}},
		},
		InferredAptPackages: []InferredAptPackage{
			{Name: "libpq-dev", Reason: "psycopg2/asyncpg need postgres headers"},
		},
		RepositoryLayout: RepositoryLayout{
			Layout:     "single",
			Workspaces: []string{},
		},
		DetectedMCPConfigs: []DetectedMCPConfig{
			{
				Source: ".mcp.json",
				Servers: []MCPServerConfig{
					{Name: "github", Type: "url", URL: "https://api.githubcopilot.com/mcp/", AnthropicCompatible: true},
					{Name: "filesystem", Type: "stdio", Command: "mcp-server-filesystem", AnthropicCompatible: false, Warning: "stdio MCP cannot be deployed"},
				},
			},
		},
		DetectedSkills: []DetectedSkill{
			{Source: ".claude/skills/portfolio-analyzer/SKILL.md", SkillName: "portfolio-analyzer", Kind: "custom_local", SizeBytes: 4231},
			{ImpliedAnthropicSkills: []ImpliedAnthropicSkill{
				{SkillID: "xlsx", Reason: "detected dependency: pandas + openpyxl", Confidence: 0.85},
			}},
		},
		DetectedRequiredSecrets: []DetectedRequiredSecret{
			{Name: "ANTHROPIC_API_KEY", From: ".env.example", PurposeGuess: "Anthropic API authentication", UsedByGuess: nil, Required: true},
			{Name: "GITHUB_TOKEN", From: ".env.example", PurposeGuess: "GitHub MCP server authentication",
				UsedByGuess: &SecretUsedByGuess{MCPServer: "github"}, Required: true},
		},
		DetectedToolRiskHints: DetectedToolRiskHints{
			ProductionSignals: []ToolRiskSignal{
				{Signal: "AWS deploy workflow detected", Evidence: ".github/workflows/deploy.yml"},
			},
			UserDataSignals:          []ToolRiskSignal{},
			RecommendedDefaultPolicy: "always_allow",
			ToolOverridesRecommended: []ToolPolicyOverride{
				{Tool: "bash", Policy: "always_ask", Reason: "Production deploy detected"},
			},
		},
		DetectedHarnessSignals: DetectedHarnessSignals{
			ClaudeCode:           HarnessProbe{Installed: true, Evidence: []string{"~/.claude/ exists"}},
			Codex:                HarnessProbe{Installed: false, Evidence: []string{}},
			Cursor:               HarnessProbe{Installed: false, Evidence: []string{}},
			GeminiCLI:            HarnessProbe{Installed: false, Evidence: []string{}},
			RecommendedHarness:   "anthropic_managed_agents",
			RecommendationReason: "Claude Code is the primary local harness on this machine",
		},
	}
}
