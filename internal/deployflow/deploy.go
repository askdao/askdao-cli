// [INPUT]: 依赖 context/errors/fmt/os/path·filepath/strings + gopkg.in/yaml.v3；internal/auth（Load / ErrNoCredentials）、internal/deploy（Client / DeployInput）、internal/types（AgentSpec）；同包 PackageSkills
// [OUTPUT]: 对外提供 Prepare(dir, harnessOverride) → *Prepared（读 yaml + 打包 skill + 读 detection + harness 默认链）+ (*Prepared).Deploy(ctx, url, token, force, confirmDowngrade) + ResolveServerAndToken（env pair > credentials.json）
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

// AgentFileName is the KOL-facing spec filename at the project root.
const AgentFileName = "askdao-agent.yml"

// DefaultHarnessID is the fallback when neither the flag nor the yaml names one.
const DefaultHarnessID = "anthropic_managed_agents"

// Prepared is the deploy bundle, assembled once. CLI callers can print diffs /
// progress between Prepare and Deploy; the desktop calls both back-to-back.
// Credentials are resolved by the caller (ResolveServerAndToken) so the CLI can
// keep its distinct auth-failure exit path.
type Prepared struct {
	AgentYAML []byte
	Spec      *types.AgentSpec
	Detection []byte // optional .askdao/detection.json, nil if absent
	HarnessID string
	SkillZips map[string][]byte
}

// Prepare reads <dir>/askdao-agent.yml, packages custom_local skills via
// PackageSkills (the single packaging source of truth), picks up the optional
// detection.json and resolves the harness (override > yaml.preferred_harness >
// default).
func Prepare(dir, harnessOverride string) (*Prepared, error) {
	agentYAML, err := os.ReadFile(filepath.Join(dir, AgentFileName))
	if err != nil {
		return nil, err
	}
	var spec types.AgentSpec
	if err := yaml.Unmarshal(agentYAML, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", AgentFileName, err)
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
		Detection: detection,
		HarnessID: harnessID,
		SkillZips: skillZips,
	}, nil
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
