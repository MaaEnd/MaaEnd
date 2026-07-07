package essencefilter

import (
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

var (
	currentRun   *RunState
	currentRunMu sync.RWMutex
)

// RunState holds all runtime state for a single EssenceFilter run.
// Init allocates/resets it; Finish clears it. Actions access via getRunState().
type RunState struct {
	// Stats
	VisitedCount            int
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

	// Essence types selected for this run (e.g. Flawless, Pure)
	EssenceTypes []EssenceMeta
	// EssenceMode derived from selection: flawless_only / pure_only / both
	EssenceMode EssenceMode

	// PipelineOpts is a copy of EssenceFilterInit attach JSON; filled in Init for the run (avoids re-parsing).
	PipelineOpts EssenceFilterOptions

	// InputLanguage is normalized match locale (CN|TC|EN|JP|KR), copied from PipelineOpts at Init.
	InputLanguage string
}

// Reset zeroes all fields for a new run. Call from Init after loading options.
func (s *RunState) Reset() {
	s.VisitedCount = 0
	s.MatchedCount = 0
	s.ExtFuturePromisingCount = 0
	s.ExtSlot3PracticalCount = 0
	s.TargetSkillCombinations = nil
	s.MatchedCombinationSummary = nil
	s.MatchEngine = nil
	s.CurrentSkills = [3]string{}
	s.CurrentSkillLevels = [3]int{}
	s.RowBoxes = nil
	s.RowIndex = 0
	s.PipelineOpts = EssenceFilterOptions{}
	s.InputLanguage = ""
	// EssenceTypes and EssenceMode are set by Init from options, not cleared here
}

// getRunState returns the current run state. Returns nil if no run is active.
func getRunState() *RunState {
	currentRunMu.RLock()
	defer currentRunMu.RUnlock()
	return currentRun
}

// setRunState sets the current run state. Call from Init with a new or reset RunState; from Finish with nil.
func setRunState(s *RunState) {
	currentRunMu.Lock()
	defer currentRunMu.Unlock()
	currentRun = s
}
