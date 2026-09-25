package essencefilter

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var currentRun *RunState

// RunState holds all runtime state for a single EssenceFilter run.
// Init allocates it; Finish clears it. Agent callbacks execute serially.
type RunState struct {
	TaskID    int64
	Inventory *inventoryState
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

// runStateResetSink clears state even when a task is stopped before Finish.
type runStateResetSink struct{}

var _ maa.TaskerEventSink = &runStateResetSink{}

func (*runStateResetSink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if event != maa.EventStatusSucceeded && event != maa.EventStatusFailed {
		return
	}
	st := currentRun
	if st == nil || st.TaskID != int64(detail.TaskID) {
		return
	}
	currentRun = nil
	if st.Inventory != nil {
		maafocus.PrintLargeContent(i18n.T("essencefilter.inventory.failed"))
	}
}
