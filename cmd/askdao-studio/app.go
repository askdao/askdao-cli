// [INPUT]: 依赖 context；internal/webstudio 的 Options/BuildStudioData/DeployResult；internal/types 的 AgentSpec
// [OUTPUT]: App（Wails bound-method 宿主）+ StudioOptions（桌面注入 webstudio 的数据与回调）
// [POS]: cmd/askdao-studio 应用层 —— 阶段2 最小骨架（占位 StudioData + stub 回调）；扫描/部署真实接线在后续阶段
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"

	"github.com/askdao/askdao-cli/internal/types"
	"github.com/askdao/askdao-cli/internal/webstudio"
)

// App hosts the desktop session context + Wails-bound methods.
type App struct {
	ctx context.Context
}

// NewApp constructs the desktop App.
func NewApp() *App { return &App{} }

// startup captures the Wails runtime context (later bound-method calls use it).
func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// StudioOptions builds the webstudio Options the desktop serves through the Wails
// AssetServer. 阶段2 skeleton: a placeholder StudioData so the workbench renders and
// Desktop=true flips on the desktop-only frontend blocks; real wiring (drag folder →
// pipeline.Run → BuildStudioData, and OnSave/OnDeploy → disk + deployFromDir) lands
// in later stages.
func (a *App) StudioOptions() webstudio.Options {
	data := webstudio.BuildStudioData(placeholderSpec(), nil, "Anthropic Managed Agents", false)
	data.Desktop = true
	return webstudio.Options{
		Data:   data,
		OnSave: func(*types.AgentSpec) error { return nil },
		OnDeploy: func(*types.AgentSpec) (*webstudio.DeployResult, error) {
			return &webstudio.DeployResult{Message: "Deploy not wired yet (阶段2 skeleton)."}, nil
		},
	}
}

// placeholderSpec is a minimal non-nil spec so BuildStudioData/studio.html render
// without a real scan. Replaced by the scanned draft once folder-drop lands.
func placeholderSpec() *types.AgentSpec {
	s := &types.AgentSpec{}
	s.Metadata.Name = "askdao-studio"
	return s
}
