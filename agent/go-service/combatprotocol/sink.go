package combatprotocol

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.TaskerEventSink = &Sink{}

// Sink folds the container run outcome back into the rotation state:
//   - a failed run marks the current slot as failed and advances the rotation
//     in sanity mode, so the next run continues with the next slot;
//   - a succeeded run marks the current slot as completed in date mode, where
//     the calendar drives the rotation and a successful run completes the
//     day's slot.
type Sink struct{}

// OnTaskerTask handles tasker task lifecycle events for the container entry.
func (s *Sink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if detail.Entry != attachNodeName {
		return
	}
	switch event {
	case maa.EventStatusFailed:
		s.handleFailed(detail)
	case maa.EventStatusSucceeded:
		s.handleSucceeded(detail)
	}
}

// handleFailed marks the current slot as failed and advances the rotation in
// sanity mode. Date mode keeps the pointer (the day boundary drives that
// rotation); the failed mark stays visible until the round completes.
func (s *Sink) handleFailed(detail maa.TaskerTaskDetail) {
	stateMu.Lock()
	st, err := loadState()
	if err != nil {
		stateMu.Unlock()
		log.Error().
			Err(err).
			Str("component", "CombatProtocolSink").
			Msg("failed to load rotation state")
		return
	}
	slot, ok := st.currentSlot()
	if !ok {
		stateMu.Unlock()
		return
	}
	index := st.CurrentIndex
	st.Progress[index] = ProgressFailed
	if st.Mode == ModeSanity {
		advanceCurrent(&st)
	}
	roundCompleted := markRoundIfComplete(&st)
	if err := saveState(st); err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolSink").
			Msg("failed to save rotation state")
	}
	stateMu.Unlock()

	log.Error().
		Str("component", "CombatProtocolSink").
		Uint64("task_id", detail.TaskID).
		Str("task", slot.Task).
		Int("slot_index", index).
		Msg("container run failed, slot marked as failed")
	maafocus.PrintLargeContent(i18n.T("combatprotocol.failed", i18n.T(taskLabelKey(slot.Task))))
	if roundCompleted {
		maafocus.PrintLargeContent(i18n.T("combatprotocol.round_complete"))
	}
}

// handleSucceeded marks the current slot as completed when a date-mode run
// finishes successfully; in sanity mode the completion signal is reaching the
// threshold (handled by AccumulateAction).
func (s *Sink) handleSucceeded(detail maa.TaskerTaskDetail) {
	stateMu.Lock()
	st, err := loadState()
	if err != nil {
		stateMu.Unlock()
		log.Error().
			Err(err).
			Str("component", "CombatProtocolSink").
			Msg("failed to load rotation state")
		return
	}
	slot, ok := st.currentSlot()
	if st.Mode != ModeDate || !ok {
		stateMu.Unlock()
		return
	}
	index := st.CurrentIndex
	if st.Progress[index] != ProgressPending {
		stateMu.Unlock()
		return
	}
	st.Progress[index] = ProgressSuccess
	roundCompleted := markRoundIfComplete(&st)
	if err := saveState(st); err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolSink").
			Msg("failed to save rotation state")
	}
	stateMu.Unlock()

	log.Info().
		Str("component", "CombatProtocolSink").
		Uint64("task_id", detail.TaskID).
		Str("task", slot.Task).
		Int("slot_index", index).
		Msg("date-mode run succeeded, slot marked as completed")
	if roundCompleted {
		maafocus.PrintLargeContent(i18n.T("combatprotocol.round_complete"))
	}
}
