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
	FilteredSkillStats      [3]map[int]int
	StatsLogged             bool

	// Target combinations and match summary
	MatchEngine *matchapi.Engine

	TargetSkillCombinations   []matchapi.SkillCombination
	MatchedCombinationSummary map[string]*matchapi.SkillCombinationSummary

	// Grid traversal
	CurrentRow          int
	MaxItemsPerRow      int
	TotalCount          int // OCR 得到的库存总数，0 表示未知；用于计算剩余是否 <= 45 以决定是否尾扫
	FirstRowSwipeDone   bool
	FinalLargeScanUsed  bool
	InFinalScan         bool // 当前 RowBoxes 来自 EssenceDetectFinal（尾扫大 ROI）
	PendingFinalScan    bool // 剩余 ≤ 45 时先补一次 swipe，下次进 RowNextItem 再进尾扫
	SwipeCalibrateRetry int

	// Current item's three skills cache
	CurrentSkills      [3]string
	CurrentSkillLevels [3]int

	// Row processing
	RowBoxes [][4]int
	RowIndex int

	// 记录本行扫描到的真实物理格子总数
	PhysicalItemCount int

	// Essence types selected for this run (e.g. Flawless, Pure)
	EssenceTypes []EssenceMeta

	// PipelineOpts is a copy of EssenceFilterInit attach JSON; filled in Init for the run (avoids re-parsing).
	PipelineOpts EssenceFilterOptions
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
	s.MatchEngine = nil
	s.CurrentRow = 1
	s.MaxItemsPerRow = 9
	s.TotalCount = 0
	s.FirstRowSwipeDone = false
	s.FinalLargeScanUsed = false
	s.InFinalScan = false
	s.PendingFinalScan = false
	s.SwipeCalibrateRetry = 0
	s.CurrentSkills = [3]string{}
	s.CurrentSkillLevels = [3]int{}
	s.RowBoxes = nil
	s.RowIndex = 0
	s.PhysicalItemCount = 0
	s.PipelineOpts = EssenceFilterOptions{}
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
