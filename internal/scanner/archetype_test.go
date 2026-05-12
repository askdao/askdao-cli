package scanner

import (
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func TestInferArchetype(t *testing.T) {
	cases := []struct {
		name string
		det  types.Detection
		want string
	}{
		{
			name: "skill pipeline",
			det: types.Detection{
				DetectedSkills:    []types.DetectedSkill{{SkillName: "homework-gen", IsLocalOriginal: true}},
				DetectedLanguages: []types.DetectedLanguage{{Language: "Markdown", Percentage: 70}, {Language: "HTML", Percentage: 30}},
			},
			want: "skill_pipeline",
		},
		{
			name: "code app via framework",
			det: types.Detection{
				DetectedFrameworks: []types.DetectedFramework{{Name: "FastAPI", Confidence: 0.95}},
				DetectedLanguages:  []types.DetectedLanguage{{Language: "Python", Percentage: 90}},
			},
			want: "code_app",
		},
		{
			name: "mixed: local skill plus a service",
			det: types.Detection{
				DetectedSkills:     []types.DetectedSkill{{SkillName: "helper", IsLocalOriginal: true}},
				DetectedFrameworks: []types.DetectedFramework{{Name: "Next.js", Confidence: 0.9}},
				DetectedLanguages:  []types.DetectedLanguage{{Language: "TypeScript", Percentage: 80}},
			},
			want: "mixed",
		},
		{
			name: "unknown: nothing decisive",
			det:  types.Detection{DetectedLanguages: []types.DetectedLanguage{{Language: "Shell", Percentage: 100}}},
			want: "unknown",
		},
		{
			name: "vendored-only skills are not 'local'",
			det: types.Detection{
				DetectedSkills:    []types.DetectedSkill{{SkillName: "tts", IsLocalOriginal: false}},
				DetectedLanguages: []types.DetectedLanguage{{Language: "Markdown", Percentage: 100}},
			},
			// no local skill → not skill_pipeline strong; doc-dominant but no
			// local skill → unknown
			want: "unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := InferArchetype(&tc.det)
			if got.Kind != tc.want {
				t.Errorf("got %q, want %q (%+v)", got.Kind, tc.want, got)
			}
		})
	}
}
