// [INPUT]: 依赖 path/filepath、sort，internal/types 的 Detection / DetectedSkill / Skill
// [OUTPUT]: 对外提供 BuildAgentSpecSkills(det) []types.Skill —— 用 detection.detected_skills
//
//	deterministic 构造 agent.yml.skills 段（一个 custom_local 条目 = 一个 skill 目录；
//	implied builtin 去重）。cmd 层在 LLM 返回的 AgentSpec 上强覆盖这一段。
//
// [POS]: internal/pipeline 的 L3.5 builder —— 介于 scanner（L1-L3）和 recommender（L4 LLM）之间。
//
//	设计动机：skills 段是 deterministic 事实字段（哪些 skill 在 detection 里），不该让 LLM
//	自由发挥。这是 v0.7 引入的"信任边界 in L1-L4"哲学（design.md §9.13）的实例：
//	硬字段确定性填充，软字段才交 LLM。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package pipeline

import (
	"path/filepath"
	"sort"

	"github.com/askdao/askdao-cli/internal/types"
)

// BuildAgentSpecSkills assembles a deterministic skills[] array from the
// detection. Rules:
//
//   - Each concrete DetectedSkill (SkillName != "" && Source != "")
//     → one Skill{Type: "custom_local", Path: filepath.Dir(Source)}.
//     Path is the skill directory relative to project root (e.g.
//     ".agents/skills/tts"), NOT the SKILL.md file. Both repo-native and
//     vendored skills enter this list identically — Anthropic Managed Agents
//     has no "reinstall from registry" path, every custom skill uploads.
//
//   - Each unique ImpliedAnthropicSkill (across all DetectedSkill entries)
//     → one Skill{Type: "builtin", Provider: "anthropic", ID: <skill_id>}.
//     Duplicates are collapsed by SkillID.
//
// The output is stably sorted: custom_local entries first (by Path), then
// builtin entries (by ID). This keeps generated agent.yml diffs deterministic
// across re-runs.
//
// cmd-layer (init_auto.go) calls this and overwrites whatever the LLM emitted
// in spec.Skills with the result. The LLM prompt is also asked to omit the
// skills field entirely (PR 2 conductor follow-up) — this builder is the
// belt that makes the suspenders optional.
func BuildAgentSpecSkills(det *types.Detection) []types.Skill {
	if det == nil {
		return nil
	}

	var customs []types.Skill
	seenBuiltin := map[string]bool{}
	var builtins []types.Skill

	for _, s := range det.DetectedSkills {
		// Project-scope concrete skills become the default skills[]. User-scope
		// (global, ~/.claude/skills) skills are deliberately excluded here — the
		// web studio surfaces them as opt-in candidates, so a KOL's entire global
		// skill library does not auto-ship with every agent.
		if s.SkillName != "" && s.Source != "" && s.Scope != "user" {
			customs = append(customs, types.Skill{
				Type: "custom_local",
				Path: filepath.ToSlash(filepath.Dir(s.Source)),
			})
		}
		for _, b := range s.ImpliedAnthropicSkills {
			if b.SkillID == "" || seenBuiltin[b.SkillID] {
				continue
			}
			seenBuiltin[b.SkillID] = true
			builtins = append(builtins, types.Skill{
				Type:     "builtin",
				Provider: "anthropic",
				ID:       b.SkillID,
			})
		}
	}

	sort.Slice(customs, func(i, j int) bool { return customs[i].Path < customs[j].Path })
	sort.Slice(builtins, func(i, j int) bool { return builtins[i].ID < builtins[j].ID })

	out := make([]types.Skill, 0, len(customs)+len(builtins))
	out = append(out, customs...)
	out = append(out, builtins...)
	return out
}
