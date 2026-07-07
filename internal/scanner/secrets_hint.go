package scanner

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// envSampleFiles are the dotenv-style files we probe for declared keys. We
// never read non-sample .env files — values may contain real secrets.
var envSampleFiles = []string{".env.example", ".env.sample", ".env.template"}

// secretRule classifies a recognized environment variable name into a purpose
// and a default required-ness. The first matching rule wins.
type secretRule struct {
	matchSuffixes []string // matches if env name ENDS WITH any of these (case-insensitive)
	matchSubstr   []string // matches if env name CONTAINS any of these
	purpose       string
	required      bool
	note          string
}

// secretRules are tried in order. Strict suffix rules first (the API key
// patterns), broader substring rules second.
var secretRules = []secretRule{
	{matchSuffixes: []string{"ANTHROPIC_API_KEY"}, purpose: "Anthropic API authentication", required: true},
	{matchSuffixes: []string{"OPENAI_API_KEY"}, purpose: "OpenAI API authentication", required: true},
	{matchSuffixes: []string{"GITHUB_TOKEN", "GH_TOKEN"}, purpose: "GitHub authentication (likely MCP server or CI)", required: true},
	{matchSuffixes: []string{"DATABASE_URL", "POSTGRES_URL", "POSTGRES_DSN"}, purpose: "PostgreSQL connection string (project-internal, not for agent runtime)", required: false, note: "Likely development-only, may not need to enter vault"},
	{matchSubstr: []string{"REDIS_URL"}, purpose: "Redis connection string (project-internal)", required: false, note: "Likely development-only"},
	{matchSubstr: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}, purpose: "AWS credentials", required: true},
	{matchSubstr: []string{"_API_KEY", "_TOKEN", "_SECRET"}, purpose: "Third-party API credential", required: true},
	{matchSubstr: []string{"PASSWORD"}, purpose: "Password credential", required: true},
	{matchSubstr: []string{"CREDENTIAL"}, purpose: "Service account credential", required: true},
}

// DetectRequiredSecrets reads .env.example / .env.sample / .env.template at
// root, classifies each declared key against secretRules, and cross-references
// MCP servers so a `GITHUB_TOKEN` lights up its UsedByGuess pointer when a
// `github` MCP server is also detected.
//
// Values are NEVER read from disk — only key names.
func DetectRequiredSecrets(root string, mcpConfigs []types.DetectedMCPConfig) ([]types.DetectedRequiredSecret, error) {
	if root == "" {
		return nil, errors.New("scanner: root must be non-empty")
	}

	mcpByKnownSecret := map[string]string{}
	for _, cfg := range mcpConfigs {
		for _, s := range cfg.Servers {
			low := strings.ToLower(s.Name)
			if strings.Contains(low, "github") {
				mcpByKnownSecret["GITHUB_TOKEN"] = s.Name
				mcpByKnownSecret["GH_TOKEN"] = s.Name
			}
			if strings.Contains(low, "slack") {
				mcpByKnownSecret["SLACK_BOT_TOKEN"] = s.Name
				mcpByKnownSecret["SLACK_TOKEN"] = s.Name
			}
		}
	}

	var out []types.DetectedRequiredSecret
	seen := map[string]bool{}
	for _, rel := range envSampleFiles {
		path := filepath.Join(root, rel)
		keys, err := readEnvKeys(path)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, classifySecret(k, rel, mcpByKnownSecret))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// readEnvKeys parses a dotenv file and returns ONLY the declared key names —
// values are deliberately discarded.
func readEnvKeys(path string) ([]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key != "" {
			out = append(out, key)
		}
	}
	return out, nil
}

func classifySecret(name, source string, mcpHints map[string]string) types.DetectedRequiredSecret {
	upper := strings.ToUpper(name)
	for _, rule := range secretRules {
		for _, suf := range rule.matchSuffixes {
			if upper == suf || strings.HasSuffix(upper, "_"+suf) {
				return buildSecret(name, source, rule, mcpHints)
			}
		}
		for _, sub := range rule.matchSubstr {
			if strings.Contains(upper, sub) {
				return buildSecret(name, source, rule, mcpHints)
			}
		}
	}
	return types.DetectedRequiredSecret{
		Name:         name,
		From:         source,
		PurposeGuess: types.UnknownSecretPurpose,
		Required:     false,
	}
}

func buildSecret(name, source string, rule secretRule, mcpHints map[string]string) types.DetectedRequiredSecret {
	out := types.DetectedRequiredSecret{
		Name:         name,
		From:         source,
		PurposeGuess: rule.purpose,
		Required:     rule.required,
		Note:         rule.note,
	}
	if mcpServer, ok := mcpHints[strings.ToUpper(name)]; ok {
		out.UsedByGuess = &types.SecretUsedByGuess{MCPServer: mcpServer}
		out.PurposeGuess = "MCP server \"" + mcpServer + "\" authentication"
	}
	return out
}
