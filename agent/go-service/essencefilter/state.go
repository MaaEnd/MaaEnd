package essencefilter

import "github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"

var currentRun *RunState

// RunState holds all runtime state for a single EssenceFilter run.
// Init allocates it; Finish clears it. Agent callbacks execute serially.
type RunState struct {
	// Stats
	MatchedCount            int
	ExtFuturePromisingCount int
	ExtSlot3PracticalCount  int

	// Target combinations and match summary
	MatchEngine *matchapi.Engine

	TargetSkillCombinations   []matchapi.SkillCombination
	MatchedCombinationSummary map[string]*matchapi.SkillCombinationSummary

	// Current item's three skills cache
	CurrentSkills      [3]string
	CurrentSkillLevels [3]int

	// After-battle grid cache
	RowBoxes [][4]int
	RowIndex int

	// EssenceMode derived from selection: flawless_only / pure_only / both
	EssenceMode EssenceMode

	// PipelineOpts is a copy of EssenceFilterInit attach JSON; filled in Init for the run (avoids re-parsing).
	PipelineOpts EssenceFilterOptions
}
