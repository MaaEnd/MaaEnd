// Package combatprotocol implements the 「循环作战协议」 container task. It
// rotates over the slots the user assembled (each slot is one child entry of
// the container, e.g. several Protocol Space entries with different configs),
// either by accumulated sanity consumption or by game day, and persists the
// rotation state to debug/record/CombatProtocol.json so that a restart resumes
// from the recorded slot instead of starting over.
//
// One container run executes exactly one slot's turn: reaching the slot's
// sanity threshold ends the run (the client then starts the next slot's run),
// and in date mode the day's slot runs until sanity runs out.
package combatprotocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/rs/zerolog/log"
)

const (
	recordFileName          = "CombatProtocol.json"
	stateSchemaVersion      = 2
	corruptBackupTimeLayout = "20060102-150405.000"

	// attachNodeName is the container entry node. Every component of this
	// package reads the merged option attach values from this node, the same
	// way ScheduleRecognition reads its weekday flags.
	attachNodeName = "CombatProtocolMain"
)

// Combat task identifiers, used in attach values, params and state files.
const (
	TaskAutoEssence   = "AutoEssence"
	TaskProtocolSpace = "ProtocolSpace"
)

// Rotation modes.
const (
	ModeSanity = "sanity"
	ModeDate   = "date"
)

// Single-round progress values.
const (
	ProgressPending = "pending"
	ProgressSuccess = "success"
	ProgressFailed  = "failed"
)

// RotatedExpect is the expect value of the state recognition that reports
// "the rotation has just advanced" so the pipeline can end the current run.
const RotatedExpect = "rotated"

// Default per-slot thresholds (used when the client sends no threshold).
const (
	defaultThresholdAutoEssence   = 480
	defaultThresholdProtocolSpace = 240
)

// Per-run sanity costs: 80 without the sanity consumption permit, 160 with it.
const (
	sanityCostPerRun           = 80
	sanityCostPerRunWithCoupon = 160
)

// Slot is one rotation slot: a child entry of the container.
type Slot struct {
	// Task is the combat task the slot runs (AutoEssence / ProtocolSpace).
	Task string `json:"task"`
	// Threshold is the sanity-accumulation threshold of this slot.
	Threshold int `json:"threshold"`
}

// State is the persisted rotation state. Slots, accum and progress are
// parallel arrays: the entry index is the slot identity.
type State struct {
	SchemaVersion     int       `json:"schema_version"`
	Mode              string    `json:"mode"`
	Slots             []Slot    `json:"slots"`
	CurrentIndex      int       `json:"current_index"`
	Accum             []int     `json:"accum"`
	Progress          []string  `json:"progress"`
	LastRunDate       string    `json:"last_run_date,omitempty"`
	PendingCoupon     bool      `json:"pending_coupon,omitempty"`
	RotatedThisBattle bool      `json:"rotated_this_battle,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// stateMu guards every load-modify-save sequence on the state file.
var stateMu sync.Mutex

// recordPathFunc resolves the state file path; overridden in tests.
var recordPathFunc = defaultRecordPath

func defaultRecordPath() string {
	return fsutil.OutputPath("debug", "record", recordFileName)
}

// emptyState returns a state with no rotation information yet.
func emptyState() State {
	return State{SchemaVersion: stateSchemaVersion}
}

// newState returns a fresh rotation state for the given slots and mode.
func newState(slots []Slot, mode string) State {
	st := emptyState()
	st.Mode = mode
	st.Slots = append([]Slot(nil), slots...)
	st.Accum = make([]int, len(slots))
	st.Progress = make([]string, len(slots))
	for i := range st.Progress {
		st.Progress[i] = ProgressPending
	}
	return st
}

// currentSlot returns the slot the rotation currently points at.
func (st *State) currentSlot() (Slot, bool) {
	if len(st.Slots) == 0 || st.CurrentIndex < 0 || st.CurrentIndex >= len(st.Slots) {
		return Slot{}, false
	}
	return st.Slots[st.CurrentIndex], true
}

// loadState reads the state file from its default location.
func loadState() (State, error) {
	return loadStateFrom(recordPathFunc())
}

// loadStateFrom reads a state file. A missing or empty file yields an empty
// state; a corrupt or outdated file is renamed aside (never deleted) and
// treated as empty, mirroring the IMS record handling.
func loadStateFrom(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyState(), nil
		}
		return State{}, fmt.Errorf("read combat protocol record: %w", err)
	}
	if len(raw) == 0 {
		return emptyState(), nil
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		resetCorruptState(path, err)
		return emptyState(), nil
	}
	if st.SchemaVersion != stateSchemaVersion {
		log.Info().
			Str("component", "CombatProtocol").
			Int("found", st.SchemaVersion).
			Int("expected", stateSchemaVersion).
			Msg("combat protocol record schema outdated, resetting rotation")
		return emptyState(), nil
	}
	if st.CurrentIndex < 0 || st.CurrentIndex >= len(st.Slots) {
		st.CurrentIndex = 0
	}
	st.normalizeSlices()
	return st, nil
}

// normalizeSlices keeps accum/progress aligned with slots.
func (st *State) normalizeSlices() {
	for len(st.Accum) < len(st.Slots) {
		st.Accum = append(st.Accum, 0)
	}
	st.Accum = st.Accum[:len(st.Slots)]
	for len(st.Progress) < len(st.Slots) {
		st.Progress = append(st.Progress, ProgressPending)
	}
	st.Progress = st.Progress[:len(st.Slots)]
}

// resetCorruptState renames a damaged state file aside and continues with an
// empty state. Rename failure is logged only; callers continue with empty
// in-memory state and the next save writes a fresh file.
func resetCorruptState(path string, cause error) {
	stamp := time.Now().UTC().Format(corruptBackupTimeLayout)
	backup := path + ".corrupt-" + stamp
	if err := os.Rename(path, backup); err != nil {
		log.Error().
			Err(err).
			AnErr("cause", cause).
			Str("component", "CombatProtocol").
			Str("path", path).
			Msg("failed to rename corrupt combat protocol record, continuing with empty state")
		return
	}
	log.Warn().
		Err(cause).
		Str("component", "CombatProtocol").
		Str("path", path).
		Str("backup", backup).
		Msg("combat protocol record corrupt, renamed old file and resetting")
}

// saveState writes the state to its default location.
func saveState(st State) error {
	return saveStateTo(recordPathFunc(), st)
}

// saveStateTo persists the state atomically (temp file + rename).
func saveStateTo(path string, st State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create combat protocol record dir: %w", err)
	}
	st.SchemaVersion = stateSchemaVersion
	st.UpdatedAt = time.Now().UTC()
	st.normalizeSlices()
	raw, err := json.MarshalIndent(st, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal combat protocol record: %w", err)
	}
	raw = append(raw, '\n')
	return fsutil.WriteFileAtomic(path, raw, 0o644)
}

// readCurrentState returns a snapshot of the persisted state.
func readCurrentState() (State, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	return loadState()
}

// reconcileState aligns the persisted state with the current slots:
//   - a mode switch or a changed task sequence resets the whole rotation,
//     because accumulated values are tied to the slot order;
//   - when only thresholds change, they are updated in place and progress
//     is kept.
//
// It reports whether anything changed.
func reconcileState(st *State, slots []Slot, mode string) bool {
	if st.Mode == "" && len(st.Slots) == 0 {
		*st = newState(slots, mode)
		return true
	}
	if st.Mode != mode || !sameTaskSequence(st.Slots, slots) {
		log.Info().
			Str("component", "CombatProtocol").
			Str("from_mode", st.Mode).
			Str("to_mode", mode).
			Int("from_slots", len(st.Slots)).
			Int("to_slots", len(slots)).
			Msg("rotation mode or slot sequence changed, resetting rotation state")
		*st = newState(slots, mode)
		return true
	}

	changed := false
	for i := range st.Slots {
		if st.Slots[i].Threshold != slots[i].Threshold {
			log.Info().
				Str("component", "CombatProtocol").
				Int("slot_index", i).
				Int("from", st.Slots[i].Threshold).
				Int("to", slots[i].Threshold).
				Msg("slot threshold updated")
			st.Slots[i].Threshold = slots[i].Threshold
			changed = true
		}
	}
	st.normalizeSlices()
	return changed
}

// sameTaskSequence reports whether both slot lists run the same tasks in the
// same order (thresholds ignored).
func sameTaskSequence(a, b []Slot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Task != b[i].Task {
			return false
		}
	}
	return true
}

// advanceCurrent moves the rotation pointer to the next slot, wrapping around.
// Accumulated sanity is kept; only reaching a threshold clears it (see
// accumulateBattle).
func advanceCurrent(st *State) {
	if len(st.Slots) == 0 {
		return
	}
	st.CurrentIndex = (st.CurrentIndex + 1) % len(st.Slots)
}

// markRoundIfComplete resets the single-round progress once every slot has run
// this round (success or failure), starting a new round.
func markRoundIfComplete(st *State) bool {
	if len(st.Slots) == 0 {
		return false
	}
	for _, p := range st.Progress {
		if p == ProgressPending {
			return false
		}
	}
	for i := range st.Progress {
		st.Progress[i] = ProgressPending
	}
	return true
}

// advanceDateOnNewDay moves the rotation to the next slot the first time a run
// happens on a new game day. The very first run only records the date. It
// reports whether the state changed.
func advanceDateOnNewDay(st *State, today string) bool {
	if st.Mode != ModeDate || len(st.Slots) == 0 {
		return false
	}
	switch {
	case st.LastRunDate == "":
		st.LastRunDate = today
		return true
	case st.LastRunDate != today:
		advanceCurrent(st)
		st.LastRunDate = today
		return true
	}
	return false
}

// sanityCost returns the sanity points consumed by one completed run: 160 when
// the run used the sanity consumption permit, otherwise 80.
func sanityCost(usedCoupon bool) int {
	if usedCoupon {
		return sanityCostPerRunWithCoupon
	}
	return sanityCostPerRun
}

// accumulateResult describes what one completed battle changed.
type accumulateResult struct {
	Cost      int  // sanity consumed by this battle
	Accum     int  // accumulated sanity of the current slot after adding Cost
	Threshold int  // configured threshold of the current slot
	Rotated   bool // true when the threshold was reached and the rotation advanced
	RoundDone bool // true when every slot has run this round
	Next      int  // slot index the rotation points at after this battle
}

// accumulateBattle adds one completed battle's sanity cost to the current
// slot and applies the sanity-mode rotation rules: reaching the slot's
// threshold clears its accumulator, marks the round progress, advances to the
// next slot and flags RotatedThisBattle so the pipeline ends the current run.
// Date mode does not accumulate and reports changed=false.
func accumulateBattle(st *State, task string) (accumulateResult, bool) {
	slot, ok := st.currentSlot()
	if st.Mode != ModeSanity || !ok || slot.Task != task {
		return accumulateResult{}, false
	}
	cost := sanityCost(st.PendingCoupon)
	st.PendingCoupon = false
	st.Accum[st.CurrentIndex] += cost
	res := accumulateResult{
		Cost:      cost,
		Accum:     st.Accum[st.CurrentIndex],
		Threshold: slot.Threshold,
		Next:      st.CurrentIndex,
	}
	if res.Accum >= slot.Threshold {
		st.Accum[st.CurrentIndex] = 0
		st.Progress[st.CurrentIndex] = ProgressSuccess
		advanceCurrent(st)
		res.Rotated = true
		res.RoundDone = markRoundIfComplete(st)
		res.Next = st.CurrentIndex
		st.RotatedThisBattle = true
	}
	return res, true
}

// taskLabelKey maps a task identifier to its localized interface label key.
func taskLabelKey(task string) string {
	if task == TaskProtocolSpace {
		return "task.ProtocolSpace.label"
	}
	return "task.AutoEssence.label"
}

// modeLabelKey maps a rotation mode to its localized label key.
func modeLabelKey(mode string) string {
	if mode == ModeDate {
		return "combatprotocol.mode_date"
	}
	return "combatprotocol.mode_sanity"
}
