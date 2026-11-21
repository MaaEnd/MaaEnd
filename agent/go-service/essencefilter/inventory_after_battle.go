package essencefilter

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// Reward positions and their confirmed marks belong to a single claim.
type rewardInventory struct {
	gridVerified  bool
	qualities     []string
	items         map[int]inventoryItem
	originalMarks map[int]string
	// A position is complete only when its observed mark agrees with the decision for this position.
	expectedMarks map[int]string
	quality       string
	mark          string
	committed     bool
}

func newRewardInventory() *rewardInventory {
	return &rewardInventory{
		items: make(map[int]inventoryItem), originalMarks: make(map[int]string), expectedMarks: make(map[int]string),
	}
}

func (r *rewardInventory) observe(row int, item inventoryItem) error {
	if row < 1 {
		return fmt.Errorf("missing reward position")
	}
	if prev, ok := r.items[row]; ok {
		if prev.Quality != item.Quality || prev.Mark != item.Mark ||
			len(prev.Skills) != 0 && len(item.Skills) != 0 && prev.resolved != item.resolved {
			return fmt.Errorf("reward at position %d changed during the claim", row)
		}
	}
	r.items[row] = item
	if _, recorded := r.originalMarks[row]; !recorded {
		r.originalMarks[row] = item.Mark
	}
	delete(r.expectedMarks, row)
	return nil
}

func (r *rewardInventory) check(count int) error {
	if count < 0 || (count == 0 && !r.gridVerified) || len(r.expectedMarks) != count {
		return fmt.Errorf("reward coverage: processed %d of %d positions", len(r.expectedMarks), count)
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
	out.Coverage = make(map[string]tierCoverage)
	if coverage := s.Coverage[lockedGold]; coverage.CompleteAt != nil {
		out.Coverage[lockedGold] = coverage
	} else if coverage := s.Coverage[gold]; coverage.CompleteAt != nil {
		out.Coverage[lockedGold] = coverage
	}
	out.Items = []inventoryItem{}
	for _, item := range s.Items {
		out.appendLockedMatch(item)
	}
	for i := 1; i <= len(r.items); i++ {
		out.appendLockedMatch(r.items[i])
	}
	return &out
}

func prepareReward(st *RunState) error {
	if st.Account.base == nil {
		if err := st.Account.openBaseline(st.MatchEngine); err != nil {
			return err
		}
	}
	if st.Loot == nil || st.Loot.committed {
		st.Loot = newRewardInventory()
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
	if !r.gridVerified {
		return inventoryFailed(fmt.Errorf("reward grid not verified"))
	}
	if err := r.check(len(st.RowBoxes)); err != nil {
		return inventoryFailed(err)
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
	if want, ok := r.expectedMarks[st.RowIndex]; ok {
		if want == r.mark {
			return unmarked, true
		}
		return want, true
	}
	item := inventoryItem{Quality: r.quality, Mark: r.mark}
	if item.Quality != purple {
		var err error
		item, err = observeItem(st.MatchEngine, ocr, r.quality, r.mark)
		if err != nil {
			return "", inventoryFailed(err)
		}
	}
	if err := r.observe(st.RowIndex, item); err != nil {
		return "", inventoryFailed(err)
	}
	want := item.Mark
	if item.Mark == lockedMark && item.Quality == gold && len(item.weaponIDs) > 0 && st.LockLimit != nil {
		var ids [3]int
		for _, skill := range item.resolved {
			ids[skill.Pool-1] = skill.ID
		}
		if combo := st.LockLimit.combinations[ids]; combo != nil {
			combo.batchLocks++
		}
	}
	switch {
	case item.Mark == unmarked && item.Quality == purple:
		if st.PipelineOpts.DiscardUnmatched {
			want = discarded
		}
	case item.Mark == unmarked:
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
