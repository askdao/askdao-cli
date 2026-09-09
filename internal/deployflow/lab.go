// [INPUT]: 依赖 fmt/regexp/slices + internal/types（Lab / LabProducer / LabProvide）
// [OUTPUT]: 对外提供 ValidateLab(*types.Lab) error + LabStations / LabContracts 词表
// [POS]: internal/deployflow 的 lab 段结构校验 —— Prepare 在打包前调用，让 Builder
//
//	在本地就看到「producer 引用打错 / contract 不在词表」，而不是等 deploy 回 400。
//	types 包按设计只放 schema + tags（见 internal/types/CLAUDE.md），校验落在本编排层。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package deployflow

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/askdao/askdao-cli/internal/types"
)

// LabStations is the closed vocabulary for lab.provides[].station.
var LabStations = []string{"collect", "verify"}

// LabContracts is the closed vocabulary for lab.provides[].contract.
var LabContracts = []string{"lab-digest/v1", "lab-verdicts/v1"}

var labProducerID = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// ValidateLab checks the optional `lab` block's internal consistency. A nil
// block is valid — packages without a script production entrypoint omit it.
func ValidateLab(lab *types.Lab) error {
	if lab == nil {
		return nil
	}
	if len(lab.Producers) == 0 {
		return fmt.Errorf("lab.producers must not be empty")
	}
	seenID := make(map[string]bool, len(lab.Producers))
	for i, p := range lab.Producers {
		if !labProducerID.MatchString(p.ID) {
			return fmt.Errorf("lab.producers[%d].id %q must match [a-z0-9-]{1,40}", i, p.ID)
		}
		if seenID[p.ID] {
			return fmt.Errorf("lab.producers[%d].id %q is duplicated", i, p.ID)
		}
		seenID[p.ID] = true
		if p.Entrypoint == "" {
			return fmt.Errorf("lab.producers[%d] (%s): entrypoint must not be empty", i, p.ID)
		}
		if p.StateVersion < 0 {
			return fmt.Errorf("lab.producers[%d] (%s): state_version must be >= 1", i, p.ID)
		}
	}
	seenSlot := make(map[string]bool, len(lab.Provides))
	for i, pv := range lab.Provides {
		if !slices.Contains(LabStations, pv.Station) {
			return fmt.Errorf("lab.provides[%d].station %q must be one of %v", i, pv.Station, LabStations)
		}
		if !slices.Contains(LabContracts, pv.Contract) {
			return fmt.Errorf("lab.provides[%d].contract %q must be one of %v", i, pv.Contract, LabContracts)
		}
		if !seenID[pv.Producer] {
			return fmt.Errorf("lab.provides[%d].producer %q is not declared in lab.producers", i, pv.Producer)
		}
		if pv.Output == "" {
			return fmt.Errorf("lab.provides[%d]: output must not be empty", i)
		}
		slot := pv.Station + "|" + pv.Contract
		if seenSlot[slot] {
			return fmt.Errorf("lab.provides[%d]: duplicate (station=%s, contract=%s)", i, pv.Station, pv.Contract)
		}
		seenSlot[slot] = true
	}
	return nil
}
