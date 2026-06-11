// [INPUT]: 依赖 testing + reflect，internal/types
// [OUTPUT]: TestBuildAgentSpecSkills_* — 覆盖 4 个核心场景：
//
//  1. 1 原生 + 2 vendored 全部产 custom_local，path 是目录而非 SKILL.md
//  2. implied xlsx 进 builtin，且 path 排序稳定
//  3. duplicate ImpliedAnthropicSkill.SkillID 去重
//  4. 空 DetectedSkills 返 nil，不 panic
//
// [POS]: internal/pipeline 的 builder 单测，配 skills_builder.go
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package pipeline

import (
	"reflect"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestBuildAgentSpecSkills_NativeAndVendoredAllCustomLocal(t *testing.T) {
	// One repo-native skill, two vendored from skills-lock.json. All three
	// must show up as custom_local with path = skill *directory*, never
	// pointing to the SKILL.md file. Native and vendored are
	// indistinguishable from the deploy upload perspective (PR v0.7 contract).
	det := &types.Detection{
		DetectedSkills: []types.DetectedSkill{
			{Source: ".agents/skills/spelling-homework-generator/SKILL.md", SkillName: "spelling-homework-generator", Kind: "custom_local", IsLocalOriginal: true},
			{Source: ".agents/skills/asr/SKILL.md", SkillName: "asr", Kind: "custom_local", IsLocalOriginal: false, LockedSource: "marswaveai/skills", LockedHash: "4f9d213abc"},
			{Source: ".agents/skills/tts/SKILL.md", SkillName: "tts", Kind: "custom_local", IsLocalOriginal: false, LockedSource: "marswaveai/skills", LockedHash: "be3f838615"},
		},
	}

	got := BuildAgentSpecSkills(det)

	want := []types.Skill{
		{Type: "custom_local", Path: ".agents/skills/asr"},
		{Type: "custom_local", Path: ".agents/skills/spelling-homework-generator"},
		{Type: "custom_local", Path: ".agents/skills/tts"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildAgentSpecSkills custom_local mismatch:\n got=%+v\nwant=%+v", got, want)
	}

	// Defensive: no entry should point at SKILL.md
	for _, s := range got {
		if filepathBase(s.Path) == "SKILL.md" {
			t.Fatalf("path %q incorrectly points at SKILL.md (should be the skill dir)", s.Path)
		}
	}
}

func TestBuildAgentSpecSkills_ImpliedBuiltinAfterCustoms(t *testing.T) {
	det := &types.Detection{
		DetectedSkills: []types.DetectedSkill{
			{Source: ".agents/skills/foo/SKILL.md", SkillName: "foo", Kind: "custom_local", IsLocalOriginal: true},
			{ImpliedAnthropicSkills: []types.ImpliedAnthropicSkill{
				{SkillID: "xlsx", Reason: "pandas + openpyxl detected", Confidence: 0.85},
			}},
		},
	}

	got := BuildAgentSpecSkills(det)

	want := []types.Skill{
		{Type: "custom_local", Path: ".agents/skills/foo"},
		{Type: "builtin", Provider: "anthropic", ID: "xlsx"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildAgentSpecSkills builtin ordering wrong:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestBuildAgentSpecSkills_DuplicateImpliedBuiltinDeduped(t *testing.T) {
	// Two ImpliedAnthropicSkill entries with the same SkillID — possible when
	// multiple deps independently imply the same builtin (pandas → xlsx +
	// openpyxl → xlsx). Result must show xlsx once.
	det := &types.Detection{
		DetectedSkills: []types.DetectedSkill{
			{ImpliedAnthropicSkills: []types.ImpliedAnthropicSkill{
				{SkillID: "xlsx", Confidence: 0.85},
				{SkillID: "pdf", Confidence: 0.7},
			}},
			{ImpliedAnthropicSkills: []types.ImpliedAnthropicSkill{
				{SkillID: "xlsx", Confidence: 0.6},
			}},
		},
	}

	got := BuildAgentSpecSkills(det)

	// Sorted by ID after dedup: pdf, xlsx
	want := []types.Skill{
		{Type: "builtin", Provider: "anthropic", ID: "pdf"},
		{Type: "builtin", Provider: "anthropic", ID: "xlsx"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildAgentSpecSkills dedup failed:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestBuildAgentSpecSkills_EmptyDetection(t *testing.T) {
	got := BuildAgentSpecSkills(&types.Detection{})
	if len(got) != 0 {
		t.Fatalf("BuildAgentSpecSkills empty detection should return empty, got %+v", got)
	}

	// Defensive: nil detection should also be safe.
	if got := BuildAgentSpecSkills(nil); got != nil {
		t.Fatalf("BuildAgentSpecSkills(nil) should return nil, got %+v", got)
	}
}

// filepathBase is a tiny stand-in to avoid importing path/filepath just for the
// dirty-check assertion in the first test.
func filepathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
