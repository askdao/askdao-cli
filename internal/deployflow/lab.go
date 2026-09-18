// [INPUT]: 依赖 fmt/regexp/slices + internal/types（Lab / LabProducer / LabProvide）
// [OUTPUT]: 对外提供 ValidateLab(*types.Lab, preferredHarness) error + LabStations / LabContracts /
//
//	LabProducerModes / LabProducerKinds 词表 + HarnessManagedAgents 常量
//
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
var LabStations = []string{"collect", "verify", "report"}

// LabContracts is the closed vocabulary for lab.provides[].contract.
// lab-notice/v1 is the plain-text one: the output file is UTF-8 text (no
// envelope), broadcast as-is, truncated past 1800 characters.
// lab-notice/v2 is the enveloped JSON one: {contract, product_key, revision,
// generated_at, data:{title, blocks}}, rendered per channel by the platform.
// lab-report/v1 is the daily-report one, also enveloped JSON: {contract,
// product_key, revision, generated_at, data:{date, rules_version, budget,
// fallback, sections[]}} — the platform reads the sections and lays them out
// as one daily card per lab.
var LabContracts = []string{
	"lab-digest/v1", "lab-verdicts/v1", "lab-notice/v1", "lab-notice/v2",
	"lab-report/v1",
}

// LabProducerModes is the closed vocabulary for lab.producers[].mode.
// dedicated (also the empty default) = one instance per consuming space;
// shared = the producer's owner runs a single instance, many spaces consume it.
var LabProducerModes = []string{"dedicated", "shared"}

// LabProducerKinds is the closed vocabulary for lab.producers[].kind — the two
// ways one production round can run.
// script (also the empty default) = a script in the platform's sandbox; its
// model steps go through the platform delegation slot, so the platform makes
// the call and bills the lab.
// turn = one round is one Managed Agent turn: the snapshot is the turn's input
// and the Agent writes the artifact with its own tools. Only the
// anthropic_managed_agents harness can run it.
var LabProducerKinds = []string{"script", "turn"}

// LabProducerKindTurn is the kind that only HarnessManagedAgents can run.
const LabProducerKindTurn = "turn"

// HarnessManagedAgents is the harness id whose rounds run as Anthropic-side
// Managed Agent turns.
const HarnessManagedAgents = "anthropic_managed_agents"

var labProducerID = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// ValidateLab checks the optional `lab` block's internal consistency. A nil
// block is valid — packages without a production entrypoint omit it.
//
// preferredHarness is the package's own `preferred_harness` (empty = the
// DefaultHarnessID the deploy would fall back to); a `kind: turn` producer is
// only runnable on HarnessManagedAgents. The per-deploy `--harness` override is
// deliberately NOT what is checked here: it only redirects one request body,
// while `kind` is a property the package ships.
func ValidateLab(lab *types.Lab, preferredHarness string) error {
	if lab == nil {
		return nil
	}
	harness := preferredHarness
	if harness == "" {
		harness = DefaultHarnessID
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
		if p.Mode != "" && !slices.Contains(LabProducerModes, p.Mode) {
			return fmt.Errorf("lab.producers[%d] (%s): mode %q must be one of %v", i, p.ID, p.Mode, LabProducerModes)
		}
		if p.Kind != "" && !slices.Contains(LabProducerKinds, p.Kind) {
			return fmt.Errorf("lab.producers[%d] (%s): kind %q must be one of %v", i, p.ID, p.Kind, LabProducerKinds)
		}
		if p.Kind == LabProducerKindTurn && harness != HarnessManagedAgents {
			return fmt.Errorf(
				"lab.producers[%d] (%s): kind: turn needs preferred_harness: %s, this package declares %q — "+
					"either set `preferred_harness: %s` at the top level of askdao-agent.yml, "+
					"or give this producer `kind: script` so its model steps go through the delegation slot",
				i, p.ID, HarnessManagedAgents, harness, HarnessManagedAgents)
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
