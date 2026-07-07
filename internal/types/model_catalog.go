// [INPUT]: 无（纯数据 struct）
// [OUTPUT]: 对外提供 ModelClassEntry + FallbackModelClasses
// [POS]: internal/types —— conductor model-class catalog (GET /api/v1/cli/model-classes)
//
//	的客户端镜像（对称 conductor app/api/cli.py:ModelClassEntry）。桌面/CLI 第二步选择器
//	从一组 ModelClassEntry 渲染，故 concrete model id 只活在 conductor —— 换 Anthropic
//	模型是 conductor 重部署、非客户端二进制重下载。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package types

// ModelClassEntry is one offered model tier in the conductor model-class catalog.
type ModelClassEntry struct {
	Slug         string `json:"slug"`
	Label        string `json:"label"`
	ModelID      string `json:"model_id"`
	FriendlyName string `json:"friendly_name"`
	Blurb        string `json:"blurb"`
	CostTier     string `json:"cost_tier"` // lower | moderate | higher | unknown
	Recommended  bool   `json:"recommended"`
}

// FallbackModelClasses is the minimal offline catalog used when conductor is
// unreachable (e.g. `askdao agent edit` offline). It carries only stable
// slug/label/blurb and NO concrete model id — the binary stays free of model
// ids, and the concrete model is resolved server-side from model_class at deploy
// time. Slugs kept in sync with conductor's OFFERED_CLASSES.
func FallbackModelClasses() []ModelClassEntry {
	return []ModelClassEntry{
		{Slug: "high_reasoning", Label: "Most capable", Blurb: "Deepest reasoning for the hardest work."},
		{Slug: "balanced", Label: "Balanced", Blurb: "Great quality at moderate cost — fits most agents.", Recommended: true},
		{Slug: "fast", Label: "Fastest", Blurb: "Quickest and cheapest for simpler tasks."},
	}
}
