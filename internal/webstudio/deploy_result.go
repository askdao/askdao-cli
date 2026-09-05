// [INPUT]: 依赖 fmt + internal/deploy 的 DeployResponse（deploy 回执线格式）
// [OUTPUT]: 对外提供 NewDeployResult / DeployMessage / DeployResultLine / DeployOpenLink
// [POS]: webstudio 的 deploy 回执映射层 —— DeployResponse → DeployResult 六字段映射与回执落点（agent 页）单源，
//
//	cmd/askdao edit.go 与 cmd/askdao-studio app.go 两个 main 包共用；只做纯映射，不发起 deploy（deploy 仍由 cmd 注入回调执行）
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package webstudio

import (
	"fmt"

	"github.com/askdao/askdao-cli/internal/deploy"
)

// NewDeployResult maps a conductor deploy response onto the success-card
// payload the studio frontend renders. message is the caller's headline text
// (DeployMessage for the desktop card, DeployResultLine for the CLI studio
// status bar) — everything else travels as its own field so the card can render
// a clickable link and the schedule warning instead of burying them in prose.
func NewDeployResult(resp *deploy.DeployResponse, message string) *DeployResult {
	return &DeployResult{
		Message:         message,
		AgentURL:        DeployOpenLink(resp),
		AgentID:         resp.AgentID,
		Created:         resp.Created,
		ScheduleWarning: resp.ScheduleWarning,
	}
}

// DeployMessage is the headline of a deploy outcome: whether this deploy created
// a fresh agent or updated the existing one in place, plus the agent id.
func DeployMessage(resp *deploy.DeployResponse) string {
	verb := "Updated"
	if resp.Created {
		verb = "Created"
	}
	return fmt.Sprintf("%s agent %s", verb, resp.AgentID)
}

// DeployResultLine renders a one-line deploy summary for the studio status bar:
// the headline plus the page the KOL should open next.
func DeployResultLine(resp *deploy.DeployResponse) string {
	s := DeployMessage(resp)
	if link := DeployOpenLink(resp); link != "" {
		s += " · " + link
	}
	return s
}

// DeployOpenLink is the single place that decides which URL a deploy hands back:
// the agent's own page, which conductor always fills in.
func DeployOpenLink(resp *deploy.DeployResponse) string {
	return resp.AgentURL
}
