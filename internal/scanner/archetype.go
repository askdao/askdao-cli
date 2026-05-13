// [INPUT]: 依赖 标准库 strings、internal/types 的 Detection（DetectedSkills / DetectedLanguages / DetectedFrameworks）
// [OUTPUT]: 对外提供 InferArchetype —— 把已装配的 Detection 归类成 code_app / skill_pipeline / mixed / unknown
// [POS]: internal/scanner 的原型判定器；纯函数，pipeline 在 scanner + provider 跑完后调一次；产物给 payload.go 调整剔除策略、给 detect/bundle 展示
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package scanner

import (
	"fmt"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// backendLanguages are languages whose dominant presence signals a code app
// (a service / library) rather than a content/skill pipeline.
var backendLanguages = map[string]bool{
	"Python": true, "Go": true, "TypeScript": true, "JavaScript": true,
	"Rust": true, "Java": true, "Ruby": true, "C#": true, "PHP": true,
	"Kotlin": true, "Swift": true, "C": true, "C++": true,
}

// docLanguages are languages typical of a skill/content pipeline repo whose
// "code" is mostly prose, markup and config.
var docLanguages = map[string]bool{
	"Markdown": true, "HTML": true, "JSON": true, "YAML": true, "CSS": true, "Text": true,
}

// InferArchetype classifies the project from an already-assembled Detection.
// Deterministic; no LLM. Confidence = matched-signal-count / examined-signals.
func InferArchetype(det *types.Detection) types.ProjectArchetype {
	if det == nil {
		return types.ProjectArchetype{Kind: "unknown", Confidence: 0}
	}

	localSkills := 0
	for _, s := range det.DetectedSkills {
		if s.SkillName == "" {
			continue // implied-builtin placeholder, no directory
		}
		if s.IsLocalOriginal {
			localSkills++
		}
	}

	dominantBackend, dominantDoc := dominantLanguageBucket(det.DetectedLanguages)
	hasServiceFramework := len(det.DetectedFrameworks) > 0

	var pipelineEv, appEv []string
	if localSkills > 0 {
		pipelineEv = append(pipelineEv, fmt.Sprintf("%d repo-native skill(s) on disk", localSkills))
	}
	if !hasServiceFramework {
		pipelineEv = append(pipelineEv, "no service framework detected")
	}
	if dominantDoc {
		pipelineEv = append(pipelineEv, "languages dominated by Markdown/HTML/JSON")
	}

	if hasServiceFramework {
		names := make([]string, 0, len(det.DetectedFrameworks))
		for _, f := range det.DetectedFrameworks {
			names = append(names, f.Name)
		}
		appEv = append(appEv, "service framework(s): "+strings.Join(names, ", "))
	}
	if dominantBackend {
		appEv = append(appEv, "languages dominated by application source code")
	}

	pipelineSignal := localSkills > 0
	appSignal := hasServiceFramework || dominantBackend

	const examined = 3.0 // local-skill / framework-absence / doc-dominant
	switch {
	case pipelineSignal && appSignal:
		ev := append([]string{}, pipelineEv...)
		ev = append(ev, appEv...)
		return types.ProjectArchetype{Kind: "mixed", Confidence: 0.6, Evidence: ev}
	case pipelineSignal:
		return types.ProjectArchetype{
			Kind:       "skill_pipeline",
			Confidence: round2(float64(len(pipelineEv)) / examined),
			Evidence:   pipelineEv,
		}
	case appSignal:
		conf := 0.7
		if dominantBackend && hasServiceFramework {
			conf = 0.9
		}
		return types.ProjectArchetype{Kind: "code_app", Confidence: conf, Evidence: appEv}
	default:
		return types.ProjectArchetype{Kind: "unknown", Confidence: 0.3, Evidence: nil}
	}
}

// dominantLanguageBucket reports which bucket holds >50% of the detected code
// bytes. A repo can be neither (mixed) — both return false then.
func dominantLanguageBucket(langs []types.DetectedLanguage) (backend, doc bool) {
	var backendPct, docPct float64
	for _, l := range langs {
		switch {
		case backendLanguages[l.Language]:
			backendPct += l.Percentage
		case docLanguages[l.Language]:
			docPct += l.Percentage
		}
	}
	return backendPct > 50, docPct > 50
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
