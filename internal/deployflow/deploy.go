// [INPUT]: 依赖 context/errors/fmt/os/path·filepath/strings + gopkg.in/yaml.v3；internal/auth（Load / ErrNoCredentials）、internal/deploy（Client / DeployInput）、internal/types（AgentSpec）；同包 PackageSkills / ValidateLab
// [OUTPUT]: 对外提供 Prepare(dir, harnessOverride) / PrepareSpec(dir, specFile, harnessOverride) → *Prepared（读 yaml + 校验 lab 段 + 打包 skill + 读 detection + harness 默认链）+ (*Prepared).Deploy(ctx, url, token, force, confirmDowngrade) + ResolveServerAndToken（env pair > credentials.json）
// [POS]: internal/deployflow 部署装配单源 —— 此前「读 yaml → 打包 skill → 取凭据 → Deploy」在
//
//	cmd/askdao runDeploy / deployFromDirWithConfirm / 桌面 App.deploy 三处各写一份，桌面版
//	已漂移（不带 Detection、无降级闸出口、env override 失效、harness 默认缺）。三入口统一
//	经 Prepare + Deploy 两拍：CLI 在两拍之间打 diff/进度，桌面直接连调。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package deployflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/auth"
	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/types"
)

// AgentFileName is the KOL-facing spec filename at the project root, and the
// spec `deploy` reads when `--spec` names nothing else.
const AgentFileName = "askdao-agent.yml"

// DefaultHarnessID is the fallback when neither the flag nor the yaml names one.
const DefaultHarnessID = HarnessManagedAgents

// Prepared is the deploy bundle, assembled once. CLI callers can print diffs /
// progress between Prepare and Deploy; the desktop calls both back-to-back.
// Credentials are resolved by the caller (ResolveServerAndToken) so the CLI can
// keep its distinct auth-failure exit path.
type Prepared struct {
	AgentYAML []byte
	Spec      *types.AgentSpec
	SpecPath  string // the spec file actually read, as <dir>/<name>
	Detection []byte // optional .askdao/detection.json, nil if absent
	HarnessID string
	SkillZips map[string][]byte
}

// Prepare reads <dir>/askdao-agent.yml, packages custom_local skills via
// PackageSkills (the single packaging source of truth), picks up the optional
// detection.json and resolves the harness (override > yaml.preferred_harness >
// default).
func Prepare(dir, harnessOverride string) (*Prepared, error) {
	return PrepareSpec(dir, AgentFileName, harnessOverride)
}

// PrepareSpec is Prepare reading an explicitly named spec file (`deploy
// --spec`). One package can ship one spec per runtime line — the sandbox-line
// yml and the managed-line yml differ in preferred_harness and in their lab
// producers' kind, each with its own complete provides. Only the declaration
// changes: skills are still resolved and zipped relative to dir, so the package
// contents are identical whichever spec is deployed.
//
// specFile may be relative to dir or absolute but must resolve inside dir — the
// spec is part of the package, not something pulled in from elsewhere. Empty
// means AgentFileName.
func PrepareSpec(dir, specFile, harnessOverride string) (*Prepared, error) {
	specPath, err := resolveSpecPath(dir, specFile)
	if err != nil {
		return nil, err
	}
	agentYAML, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	var spec types.AgentSpec
	if err := yaml.Unmarshal(agentYAML, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", specPath, err)
	}
	if err := ValidateLab(spec.Lab, spec.PreferredHarness); err != nil {
		return nil, fmt.Errorf("parse %s: %w", specPath, err)
	}
	skillZips, err := PackageSkills(dir, &spec)
	if err != nil {
		return nil, err
	}
	var detection []byte
	if d, derr := os.ReadFile(filepath.Join(dir, ".askdao", "detection.json")); derr == nil {
		detection = d
	}
	harnessID := harnessOverride
	if harnessID == "" {
		harnessID = spec.PreferredHarness
	}
	if harnessID == "" {
		harnessID = DefaultHarnessID
	}
	return &Prepared{
		AgentYAML: agentYAML,
		Spec:      &spec,
		SpecPath:  specPath,
		Detection: detection,
		HarnessID: harnessID,
		SkillZips: skillZips,
	}, nil
}

// resolveSpecPath turns a --spec value into a <dir>-relative path, refusing
// anything that escapes the package directory.
func resolveSpecPath(dir, specFile string) (string, error) {
	if specFile == "" {
		specFile = AgentFileName
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	cand := specFile
	if !filepath.IsAbs(cand) {
		cand = filepath.Join(absDir, cand)
	}
	rel, relErr := filepath.Rel(absDir, filepath.Clean(cand))
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("--spec %q must name a file inside the package directory %s", specFile, dir)
	}
	return filepath.Join(dir, rel), nil
}

// Deploy POSTs the prepared bundle to conductor /cli/deploy. No interactive
// prompting — callers handle typed errors (*deploy.ErrKolProfileRequired,
// *deploy.ErrVisibilityDowngradeConfirm, *deploy.ErrBlockingWarnings).
func (p *Prepared) Deploy(ctx context.Context, conductorURL, token string, force, confirmDowngrade bool) (*deploy.DeployResponse, error) {
	cl := deploy.NewClient(conductorURL)
	cl.AuthToken = token
	return cl.Deploy(ctx, deploy.DeployInput{
		AgentYAML:                  p.AgentYAML,
		Detection:                  p.Detection,
		HarnessID:                  p.HarnessID,
		Force:                      force,
		ConfirmVisibilityDowngrade: confirmDowngrade,
		SkillZips:                  p.SkillZips,
	})
}

// ResolveServerAndToken picks the conductor URL + bearer token for deploy.
//
// Precedence (docs/cli-auth-device-flow.md §6.3 — env-first, matches
// aws/gcloud/kubectl):
//
//  1. $ASKDAO_CONDUCTOR_TOKEN + $ASKDAO_CONDUCTOR_URL (both required if either
//     is set) — CI / one-off override
//  2. credentials.json from `askdao auth login` — interactive default
//  3. error — caller prints the actionable hint
//
// The two env vars travel as a pair: explicitly setting only one is almost
// certainly a misconfiguration and silently falling back to credentials.json
// would be more confusing than the error.
func ResolveServerAndToken() (string, string, error) {
	envToken := strings.TrimSpace(os.Getenv("ASKDAO_CONDUCTOR_TOKEN"))
	envURL := strings.TrimSpace(os.Getenv("ASKDAO_CONDUCTOR_URL"))

	if envToken != "" && envURL != "" {
		return envURL, envToken, nil
	}
	if envToken != "" && envURL == "" {
		return "", "", errors.New("ASKDAO_CONDUCTOR_TOKEN is set but ASKDAO_CONDUCTOR_URL is not")
	}
	if envURL != "" && envToken == "" {
		return "", "", errors.New("ASKDAO_CONDUCTOR_URL is set but ASKDAO_CONDUCTOR_TOKEN is not")
	}

	creds, err := auth.Load()
	if err != nil {
		if errors.Is(err, auth.ErrNoCredentials) {
			return "", "", errors.New("not logged in")
		}
		return "", "", err
	}
	return creds.Server, creds.AccessToken, nil
}
