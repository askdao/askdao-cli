// [INPUT]: 依赖同包 ValidateLab、internal/types 的 Lab/LabProducer/LabProvide、标准库 strings/testing
// [OUTPUT]: 对外提供 TestValidateLab 表驱动用例
// [POS]: internal/deployflow 的 lab 段校验用例；钉死「nil 合法 / 引用不存在报错 / 词表外拒收」
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package deployflow

import (
	"strings"
	"testing"

	"github.com/askdao/askdao-cli/internal/types"
)

func okProducer() types.LabProducer {
	return types.LabProducer{
		ID:           "kalshi-paper",
		Entrypoint:   ".claude/skills/kalshi-trading/scripts/entry.py",
		StateVersion: 3,
	}
}

func okProvide() types.LabProvide {
	return types.LabProvide{
		Station:  "collect",
		Contract: "lab-digest/v1",
		Producer: "kalshi-paper",
		Output:   "lab/digest",
	}
}

func TestValidateLab(t *testing.T) {
	cases := []struct {
		name    string
		lab     *types.Lab
		wantErr string // "" = expect success
	}{
		{
			name: "nil block is valid",
			lab:  nil,
		},
		{
			name: "producer + provide",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides:  []types.LabProvide{okProvide()},
			},
		},
		{
			name: "producers only, no provides",
			lab:  &types.Lab{Producers: []types.LabProducer{okProducer()}},
		},
		{
			name: "report station",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides: []types.LabProvide{{
					Station: "report", Contract: "lab-digest/v1",
					Producer: "kalshi-paper", Output: "lab/digest",
				}},
			},
		},
		{
			name: "lab-notice contract",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides: []types.LabProvide{{
					Station: "report", Contract: "lab-notice/v1",
					Producer: "kalshi-paper", Output: "headline.txt",
				}},
			},
		},
		{
			name: "lab-notice/v2 contract",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides: []types.LabProvide{{
					Station: "report", Contract: "lab-notice/v2",
					Producer: "kalshi-paper", Output: "notice.json",
				}},
			},
		},
		{
			name:    "empty producers",
			lab:     &types.Lab{},
			wantErr: "must not be empty",
		},
		{
			name: "bad id charset",
			lab: &types.Lab{Producers: []types.LabProducer{
				{ID: "Kalshi_Paper", Entrypoint: "x.py"},
			}},
			wantErr: "must match [a-z0-9-]{1,40}",
		},
		{
			name: "duplicate producer id",
			lab: &types.Lab{Producers: []types.LabProducer{
				okProducer(), okProducer(),
			}},
			wantErr: "is duplicated",
		},
		{
			name: "empty entrypoint",
			lab: &types.Lab{Producers: []types.LabProducer{
				{ID: "kalshi-paper", Entrypoint: ""},
			}},
			wantErr: "entrypoint must not be empty",
		},
		{
			name: "negative state_version",
			lab: &types.Lab{Producers: []types.LabProducer{
				{ID: "kalshi-paper", Entrypoint: "x.py", StateVersion: -1},
			}},
			wantErr: "state_version must be >= 1",
		},
		{
			name: "unknown station",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides: []types.LabProvide{{
					Station: "publish", Contract: "lab-digest/v1",
					Producer: "kalshi-paper", Output: "lab/digest",
				}},
			},
			wantErr: "station \"publish\" must be one of",
		},
		{
			name: "unknown contract",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides: []types.LabProvide{{
					Station: "collect", Contract: "lab-digest/v2",
					Producer: "kalshi-paper", Output: "lab/digest",
				}},
			},
			wantErr: "contract \"lab-digest/v2\" must be one of",
		},
		{
			name: "unreleased notice revision",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides: []types.LabProvide{{
					Station: "report", Contract: "lab-notice/v3",
					Producer: "kalshi-paper", Output: "notice.json",
				}},
			},
			wantErr: "contract \"lab-notice/v3\" must be one of",
		},
		{
			name: "producer reference not declared",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides: []types.LabProvide{{
					Station: "collect", Contract: "lab-digest/v1",
					Producer: "typo-paper", Output: "lab/digest",
				}},
			},
			wantErr: "is not declared in lab.producers",
		},
		{
			name: "empty output",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides: []types.LabProvide{{
					Station: "collect", Contract: "lab-digest/v1",
					Producer: "kalshi-paper", Output: "",
				}},
			},
			wantErr: "output must not be empty",
		},
		{
			name: "duplicate station+contract slot",
			lab: &types.Lab{
				Producers: []types.LabProducer{okProducer()},
				Provides:  []types.LabProvide{okProvide(), okProvide()},
			},
			wantErr: "duplicate (station=collect, contract=lab-digest/v1)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateLab(tc.lab)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}
