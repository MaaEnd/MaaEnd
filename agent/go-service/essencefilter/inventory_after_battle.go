package essencefilter

import (
	"fmt"
	"maps"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Reward rows are indexed within one claim. A second pass can update marks but
// must see the same items before the batch is appended once.
type rewardInventory struct {
	items map[int]inventoryItem
	// A position is complete only when its observed mark agrees with the decision for this pass.
	expectedMarks map[int]string
	expected      int
	quality       string
	mark          string
	staged        bool
	committed     bool
}

func newRewardInventory() *rewardInventory {
	return &rewardInventory{items: make(map[int]inventoryItem), expectedMarks: make(map[int]string)}
}

func (r *rewardInventory) observe(row int, item inventoryItem) error {
	if row < 1 {
		return fmt.Errorf("missing reward position")
	}
	if prev, ok := r.items[row]; ok {
		if prev.Quality != item.Quality || prev.Mark != item.Mark ||
			len(prev.Skills) != 0 && len(item.Skills) != 0 && prev.resolved != item.resolved {
			return fmt.Errorf("reward at position %d changed between passes", row)
		}
	}
	r.items[row] = item
	delete(r.expectedMarks, row)
	return nil
}

func (r *rewardInventory) check(count int) error {
	if count < 1 || r.expected != 0 && count != r.expected || len(r.expectedMarks) != count {
		return fmt.Errorf("reward coverage: processed %d of %d positions (first pass %d)", len(r.expectedMarks), count, r.expected)
	}
	for i := 1; i <= count; i++ {
		item, observed := r.items[i]
		want, decided := r.expectedMarks[i]
		if !observed || !decided || item.Mark != want {
			return fmt.Errorf("reward %d: observed mark %q, expected %q", i, item.Mark, want)
		}
	}
	return nil
}

func (r *rewardInventory) appendTo(s *inventorySnapshot) *inventorySnapshot {
	out := *s
	out.Coverage = maps.Clone(s.Coverage)
	out.Items = append([]inventoryItem{}, s.Items...)
	for i := 1; i <= len(r.items); i++ {
		out.appendObservation(r.items[i])
	}
	return &out
}

// Acquire the journal before the claim click. Closing loot without observing it
// leaves gold coverage unconfirmed until the next full scan.
type EssenceInventoryBeginRewardAction struct{}

func (*EssenceInventoryBeginRewardAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	a, err := accountFor(ctx, taskController(ctx), arg.TaskID)
	if err != nil {
		return inventoryFailed(err)
	}
	var engine *matchapi.Engine
	if currentRun != nil && currentRun.TaskID == arg.TaskID && currentRun.MatchEngine != nil {
		engine = currentRun.MatchEngine
	} else {
		engine, err = matchapi.NewEngineFromDir(essenceFilterDataDir)
		if err != nil {
			return inventoryFailed(err)
		}
	}
	a.release()
	if err := a.begin(engine); err != nil {
		return inventoryFailed(err)
	}
	if currentRun != nil && currentRun.TaskID == arg.TaskID {
		currentRun.Loot = nil
	}
	return true
}

// A recognized insufficient-stamina dialog proves the claim was rejected. Keep
// the previous coverage so ending a farming session on low stamina is resumable.
type EssenceInventoryCancelRewardAction struct{}

func (*EssenceInventoryCancelRewardAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	ctrl := taskController(ctx)
	a, err := accountFor(ctx, ctrl, arg.TaskID)
	if err != nil {
		return inventoryFailed(err)
	}
	if err := a.cancelClaim(a.observedUID(ctx, ctrl)); err != nil {
		return inventoryFailed(err)
	}
	return true
}

func (a *inventoryAccount) cancelClaim(observedUID string) error {
	if a.base == nil {
		return nil
	}
	defer a.release()
	if !a.matchesUID(observedUID) {
		return errInventoryAccountChanged
	}
	return a.save(a.base)
}

func prepareReward(st *RunState) error {
	if st.Account.base == nil {
		if err := st.Account.begin(st.MatchEngine); err != nil {
			return err
		}
	}
	if st.Loot == nil || st.Loot.committed {
		st.Loot = newRewardInventory()
	} else if st.Loot.staged && st.PipelineOpts.AfterBattlePass == AfterBattlePassExecute {
		clear(st.Loot.expectedMarks)
	} else {
		return fmt.Errorf("unexpected re-entry into reward scan")
	}
	return nil
}

func commitAfterBattleInventory(st *RunState, observedUID string) bool {
	r := st.Loot
	if r == nil {
		return false
	}
	if r.committed {
		return true
	}
	if err := r.check(len(st.RowBoxes)); err != nil {
		return inventoryFailed(err)
	}
	if st.PipelineOpts.AfterBattlePass == AfterBattlePassReadOnly {
		if !st.Account.matchesUID(observedUID) {
			return inventoryFailed(errInventoryAccountChanged)
		}
		r.expected = len(st.RowBoxes)
		r.staged = true
		return true
	}
	if st.Account.base == nil {
		return inventoryFailed(fmt.Errorf("reward journal missing"))
	}
	s := r.appendTo(st.Account.base)
	if err := st.Account.commit(s, false, observedUID); err != nil {
		return inventoryFailed(err)
	}
	r.committed = true
	return true
}

// decideRewardMark records every item and the final mark required for publication.
// The returned mark selects the UI action; an existing mark needs no click.
func decideRewardMark(ctx *maa.Context, st *RunState, ocr matchapi.OCRInput) (string, bool) {
	r := st.Loot
	item, err := observeItem(st.MatchEngine, ocr, r.quality, r.mark)
	if err != nil {
		return "", inventoryFailed(err)
	}
	if err := r.observe(st.RowIndex, item); err != nil {
		return "", inventoryFailed(err)
	}
	want := item.Mark
	if st.PipelineOpts.AfterBattlePass != AfterBattlePassReadOnly && item.Mark == unmarked && item.Quality == gold {
		var ok bool
		want, ok = decideInventoryMark(ctx, st, ocr, item)
		if !ok {
			return "", false
		}
	}
	r.expectedMarks[st.RowIndex] = want
	if want == item.Mark {
		return unmarked, true
	}
	return want, true
}
