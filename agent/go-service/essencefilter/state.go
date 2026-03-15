package essencefilter

import "sync"

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
	FilteredSkillStats      [3]map[int]int
	StatsLogged             bool

	// Target combinations and match summary
	TargetSkillCombinations   []SkillCombination
	MatchedCombinationSummary map[string]*SkillCombinationSummary

	// Grid traversal
	CurrentCol          int
	CurrentRow          int
	MaxItemsPerRow      int
	TotalCount          int // OCR 得到的库存总数，0 表示未知；用于计算剩余是否 <= 45 以决定是否尾扫
	FirstRowSwipeDone   bool
	FinalLargeScanUsed  bool
	SwipeCalibrateRetry int

	// Current item's three skills cache
	CurrentSkills      [3]string
	CurrentSkillLevels [3]int

	// Row processing
	RowBoxes [][4]int
	RowIndex int

	// TryLastFirst: global; true at init to skip locked rows by clicking last first; set false permanently when a row's last item is not locked
	TryLastFirst bool

	// Essence types selected for this run (e.g. Flawless, Pure)
	EssenceTypes []EssenceMeta
}

// Reset zeroes all fields for a new run. Call from Init after loading options.
func (s *RunState) Reset() {
	s.VisitedCount = 0
	s.MatchedCount = 0
	s.ExtFuturePromisingCount = 0
	s.ExtSlot3PracticalCount = 0
	for i := range s.FilteredSkillStats {
		s.FilteredSkillStats[i] = nil
	}
	s.StatsLogged = false
	s.TargetSkillCombinations = nil
	s.MatchedCombinationSummary = nil
	s.CurrentCol = 1
	s.CurrentRow = 1
	s.MaxItemsPerRow = 9
	s.TotalCount = 0
	s.FirstRowSwipeDone = false
	s.FinalLargeScanUsed = false
	s.SwipeCalibrateRetry = 0
	s.CurrentSkills = [3]string{}
	s.CurrentSkillLevels = [3]int{}
	s.RowBoxes = nil
	s.RowIndex = 0
	s.TryLastFirst = true
	// EssenceTypes is set by Init from options, not cleared here
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
