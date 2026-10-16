package combatprotocol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanityCost(t *testing.T) {
	t.Parallel()
	if got := sanityCost(false); got != 80 {
		t.Fatalf("sanityCost(false) = %d, want 80", got)
	}
	if got := sanityCost(true); got != 160 {
		t.Fatalf("sanityCost(true) = %d, want 160", got)
	}
}

func TestSlotListFromAttach(t *testing.T) {
	t.Parallel()
	opts := attachOptions{
		Slots:          []string{TaskAutoEssence, "UnknownTask", TaskProtocolSpace},
		SlotThresholds: []int{160, 0, 0},
	}
	slots := opts.slotList()
	if len(slots) != 2 {
		t.Fatalf("slotList() = %+v, want 2 slots", slots)
	}
	if slots[0].Task != TaskAutoEssence || slots[0].Threshold != 160 {
		t.Fatalf("slot 0 = %+v, want threshold from attach", slots[0])
	}
	if slots[1].Task != TaskProtocolSpace || slots[1].Threshold != defaultThresholdProtocolSpace {
		t.Fatalf("slot 1 = %+v, want default threshold fallback", slots[1])
	}

	// An empty slot table falls back to the default order.
	fallback := attachOptions{}.slotList()
	if len(fallback) != 2 ||
		fallback[0].Task != TaskAutoEssence || fallback[0].Threshold != defaultThresholdAutoEssence ||
		fallback[1].Task != TaskProtocolSpace || fallback[1].Threshold != defaultThresholdProtocolSpace {
		t.Fatalf("empty attach should fall back to the default slots, got %+v", fallback)
	}
}

func TestReconcileInitThresholdUpdateAndReset(t *testing.T) {
	t.Parallel()
	slots := []Slot{{TaskAutoEssence, 480}, {TaskProtocolSpace, 240}}
	st := emptyState()
	if !reconcileState(&st, slots, ModeSanity) {
		t.Fatal("reconcileState on empty state should report a change")
	}
	if st.CurrentIndex != 0 || len(st.Slots) != 2 {
		t.Fatalf("unexpected state after init: %+v", st)
	}

	st.Accum[0] = 160
	st.Progress[1] = ProgressSuccess
	updated := []Slot{{TaskAutoEssence, 320}, {TaskProtocolSpace, 240}}
	if !reconcileState(&st, updated, ModeSanity) {
		t.Fatal("threshold update should report a change")
	}
	if st.Slots[0].Threshold != 320 {
		t.Fatalf("threshold not updated: %+v", st.Slots[0])
	}
	if st.Accum[0] != 160 || st.Progress[1] != ProgressSuccess {
		t.Fatalf("threshold update must keep progress, got %+v", st)
	}

	seq := []Slot{{TaskProtocolSpace, 240}, {TaskAutoEssence, 320}}
	if !reconcileState(&st, seq, ModeSanity) {
		t.Fatal("slot sequence change should report a change")
	}
	if st.CurrentIndex != 0 || st.Accum[0] != 0 || st.Progress[0] != ProgressPending {
		t.Fatalf("slot sequence change should reset rotation, got %+v", st)
	}

	if !reconcileState(&st, seq, ModeDate) {
		t.Fatal("mode change should report a change")
	}
	if st.Mode != ModeDate {
		t.Fatalf("mode = %q, want %q", st.Mode, ModeDate)
	}
}

func TestAdvanceCurrentWraps(t *testing.T) {
	t.Parallel()
	st := newState([]Slot{{TaskAutoEssence, 480}, {TaskProtocolSpace, 240}}, ModeSanity)
	advanceCurrent(&st)
	if st.CurrentIndex != 1 {
		t.Fatalf("CurrentIndex = %d, want 1", st.CurrentIndex)
	}
	advanceCurrent(&st)
	if st.CurrentIndex != 0 {
		t.Fatalf("CurrentIndex = %d, want wrap to 0", st.CurrentIndex)
	}
}

func TestMarkRoundIfComplete(t *testing.T) {
	t.Parallel()
	st := newState([]Slot{{TaskAutoEssence, 480}, {TaskProtocolSpace, 240}}, ModeSanity)
	st.Progress[0] = ProgressSuccess
	if markRoundIfComplete(&st) {
		t.Fatal("round should not complete while a slot is pending")
	}
	st.Progress[1] = ProgressFailed
	if !markRoundIfComplete(&st) {
		t.Fatal("round with no pending slots should complete")
	}
	if st.Progress[0] != ProgressPending || st.Progress[1] != ProgressPending {
		t.Fatalf("round completion should reset progress, got %+v", st.Progress)
	}
}

func TestAdvanceDateOnNewDay(t *testing.T) {
	t.Parallel()
	slots := []Slot{{TaskAutoEssence, 480}, {TaskProtocolSpace, 240}}
	st := newState(slots, ModeSanity)
	if advanceDateOnNewDay(&st, "2026-01-01") {
		t.Fatal("date logic must not run in sanity mode")
	}

	st = newState(slots, ModeDate)
	if !advanceDateOnNewDay(&st, "2026-01-01") {
		t.Fatal("first run should record the date")
	}
	if st.LastRunDate != "2026-01-01" || st.CurrentIndex != 0 {
		t.Fatalf("first run should not advance, got %+v", st)
	}
	if advanceDateOnNewDay(&st, "2026-01-01") {
		t.Fatal("same-day run should not change anything")
	}
	if !advanceDateOnNewDay(&st, "2026-01-02") {
		t.Fatal("new day should advance the rotation")
	}
	if st.CurrentIndex != 1 || st.LastRunDate != "2026-01-02" {
		t.Fatalf("new day should advance to next slot, got %+v", st)
	}
}

func TestAccumulateBattle(t *testing.T) {
	t.Parallel()
	st := newState([]Slot{{TaskAutoEssence, 160}, {TaskProtocolSpace, 240}}, ModeSanity)

	// First battle without the permit costs 80 and does not reach the threshold.
	res, changed := accumulateBattle(&st, TaskAutoEssence)
	if !changed || res.Cost != 80 || res.Accum != 80 || res.Rotated {
		t.Fatalf("unexpected first accumulation: %+v changed=%v", res, changed)
	}
	// Second battle with the permit costs 160: 240 >= 160, rotate to slot 1.
	st.PendingCoupon = true
	res, changed = accumulateBattle(&st, TaskAutoEssence)
	if !changed || res.Cost != 160 || !res.Rotated || res.Next != 1 {
		t.Fatalf("unexpected rotation result: %+v changed=%v", res, changed)
	}
	if st.CurrentIndex != 1 || st.Accum[0] != 0 || st.Progress[0] != ProgressSuccess {
		t.Fatalf("unexpected state after rotation: %+v", st)
	}
	if !st.RotatedThisBattle {
		t.Fatal("RotatedThisBattle should be set after rotation")
	}

	// A completed battle for a task that is not the current slot is ignored.
	if _, changed := accumulateBattle(&st, TaskAutoEssence); changed {
		t.Fatal("accumulation for a non-current slot must be ignored")
	}

	// Date mode never accumulates.
	dateState := newState(st.Slots, ModeDate)
	if _, changed := accumulateBattle(&dateState, TaskProtocolSpace); changed {
		t.Fatal("date mode must not accumulate")
	}
}

func TestAccumulateBattleRoundCompletion(t *testing.T) {
	t.Parallel()
	st := newState([]Slot{{TaskAutoEssence, 80}, {TaskProtocolSpace, 80}}, ModeSanity)
	st.Progress[1] = ProgressSuccess
	res, changed := accumulateBattle(&st, TaskAutoEssence)
	if !changed || !res.Rotated || !res.RoundDone {
		t.Fatalf("expected rotation and round completion, got %+v changed=%v", res, changed)
	}
	if st.Progress[0] != ProgressPending || st.Progress[1] != ProgressPending {
		t.Fatalf("round completion should reset progress, got %+v", st.Progress)
	}
}

func TestStateRoundTripCorruptAndOutdated(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "CombatProtocol.json")
	st := newState([]Slot{{TaskAutoEssence, 480}, {TaskProtocolSpace, 240}}, ModeSanity)
	st.Accum[0] = 160
	st.PendingCoupon = true
	st.RotatedThisBattle = true
	if err := saveStateTo(path, st); err != nil {
		t.Fatalf("saveStateTo: %v", err)
	}

	got, err := loadStateFrom(path)
	if err != nil {
		t.Fatalf("loadStateFrom: %v", err)
	}
	if got.Mode != ModeSanity || got.CurrentIndex != 0 || len(got.Slots) != 2 ||
		got.Slots[0].Threshold != 480 || got.Accum[0] != 160 ||
		!got.PendingCoupon || !got.RotatedThisBattle || got.SchemaVersion != stateSchemaVersion {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// An outdated schema yields an empty state instead of a mixed one.
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"mode":"sanity"}`), 0o644); err != nil {
		t.Fatalf("write outdated file: %v", err)
	}
	outdated, err := loadStateFrom(path)
	if err != nil {
		t.Fatalf("loadStateFrom on outdated file: %v", err)
	}
	if outdated.Mode != "" || len(outdated.Slots) != 0 {
		t.Fatalf("outdated file should yield empty state, got %+v", outdated)
	}

	// A corrupt file is renamed aside and treated as empty.
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	recovered, err := loadStateFrom(path)
	if err != nil {
		t.Fatalf("loadStateFrom on corrupt file: %v", err)
	}
	if recovered.Mode != "" || len(recovered.Slots) != 0 {
		t.Fatalf("corrupt file should yield empty state, got %+v", recovered)
	}
	backups, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("corrupt file should be renamed aside once, got %v", backups)
	}
}
