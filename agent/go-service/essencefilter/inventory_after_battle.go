package essencefilter

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Reward rows are indexed within one claim. A second pass can update marks but
// must see the same items before the batch is appended once.
type rewardInventory struct {
	items     map[int]inventoryItem
	seen      map[int]bool
	expected  int
	quality   string
	mark      string
	staged    bool
	committed bool
}

func newRewardInventory() *rewardInventory {
	return &rewardInventory{items: make(map[int]inventoryItem), seen: make(map[int]bool)}
}

func (r *rewardInventory) observe(row int, item inventoryItem) error {
	if row < 1 {
		return fmt.Errorf("missing reward position")
	}
	if prev, ok := r.items[row]; ok && (prev.Skills != item.Skills || prev.Quality != item.Quality) {
		return fmt.Errorf("reward at position %d changed between passes", row)
	}
	r.items[row] = item
	r.seen[row] = true
	return nil
}

func (r *rewardInventory) check(count int) error {
	if count < 1 || r.expected != 0 && count != r.expected || len(r.seen) != count {
		return fmt.Errorf("reward coverage: saw %d of %d positions (first pass %d)", len(r.seen), count, r.expected)
	}
	for i := 1; i <= count; i++ {
		if !r.seen[i] {
			return fmt.Errorf("missing reward at position %d", i)
		}
	}
	return nil
}

func (r *rewardInventory) appendTo(s *inventorySnapshot, version string) *inventorySnapshot {
	out := *s
	out.Items = append([]inventoryItem{}, s.Items...)
	if s.DataVersion != version {
		out = *newSnapshot(s.Account)
	}
	out.DataVersion = version
	for i := 1; i <= len(r.items); i++ {
		out.Items = append(out.Items, r.items[i])
	}
	return &out
}

// Acquire the journal before the claim click. Closing loot without observing it
// leaves Pending on disk, so the next target run must re-establish its baseline.
type EssenceInventoryBeginRewardAction struct{}

func (*EssenceInventoryBeginRewardAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	a := accountFor(ctx, arg.TaskID)
	a.release()
	if err := a.begin(); err != nil {
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
	a := accountFor(ctx, arg.TaskID)
	if err := a.cancelClaim(); err != nil {
		return inventoryFailed(err)
	}
	return true
}

func (a *inventoryAccount) cancelClaim() error {
	if a.base == nil {
		return nil
	}
	defer a.release()
	s := *a.base
	s.Pending = false
	return a.save(&s)
}

func prepareReward(st *RunState) error {
	if st.Account.base == nil {
		if err := st.Account.begin(); err != nil {
			return err
		}
	}
	if st.Loot == nil || st.Loot.committed {
		st.Loot = newRewardInventory()
	} else if st.Loot.staged && st.PipelineOpts.AfterBattlePass == AfterBattlePassExecute {
		clear(st.Loot.seen)
	} else {
		return fmt.Errorf("unexpected re-entry into reward scan")
	}
	return nil
}

func commitAfterBattleInventory(st *RunState, stage bool) bool {
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
	if stage {
		r.expected = len(st.RowBoxes)
		r.staged = true
		return true
	}
	if st.Account.base == nil {
		return inventoryFailed(fmt.Errorf("reward journal missing"))
	}
	if err := st.Account.commit(r.appendTo(st.Account.base, st.MatchEngine.DataVersion()), false); err != nil {
		return inventoryFailed(err)
	}
	r.committed = true
	return true
}

func captureRewardItem(st *RunState, ocr matchapi.OCRInput) error {
	r := st.Loot
	item, err := observeItem(st.MatchEngine, ocr, r.quality, r.mark)
	if err != nil {
		return err
	}
	return r.observe(st.RowIndex, item)
}
