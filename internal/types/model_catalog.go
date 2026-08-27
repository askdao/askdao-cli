// [INPUT]: 无（纯数据 struct）
// [OUTPUT]: 对外提供 ModelClassEntry + FallbackModelClasses + ModelEntry
// [POS]: internal/types —— conductor model catalog (GET /api/v1/cli/model-classes)
//
//	的客户端镜像（对称 conductor app/api/cli.py:ModelClassEntry / ModelEntryOut）。
//	ModelClassEntry = 旧 `classes[]` 三档视图（离线回退用）；ModelEntry = `models[]`
//	白名单（cloud#84：Studio 显式选模型，选模型即切 harness）。concrete model id 与价目只活在
//	conductor（Admin 后台维护）—— 换模型/改价是后台操作、非客户端二进制重下载。
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

// ModelEntry is one selectable model in conductor's `models[]` whitelist
// (GET /api/v1/cli/model-classes?harness=all). HarnessID tells the studio which
// harness to deploy to when this model is picked; prices are raw USD per 1M tokens.
type ModelEntry struct {
	ModelID               string   `json:"model_id"`
	Provider              string   `json:"provider"`   // anthropic | openai | siliconflow | deepseek | …（小写 slug，Studio 只用来分组 Anthropic / OpenAI / Open Source）
	HarnessID             string   `json:"harness_id"` // anthropic_managed_agents | openai_agents_sdk
	DisplayName           string   `json:"display_name"`
	Blurb                 string   `json:"blurb"`
	ModelClass            string   `json:"model_class"` // high_reasoning | balanced | fast | "" (no tier)
	Recommended           bool     `json:"recommended"`
	SortOrder             int      `json:"sort_order"`
	InputUSDPerMTok       float64  `json:"input_usd_per_mtok"`
	CachedInputUSDPerMTok *float64 `json:"cached_input_usd_per_mtok"`
	OutputUSDPerMTok      float64  `json:"output_usd_per_mtok"`
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
