package combatprotocol

import (
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/schedule"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &EnterAction{}

// EnterAction prepares the rotation state at the start of a container run: it
// reconciles the persisted state with the assembled slot list, advances the
// rotation when a new game day starts in date mode, clears per-run flags, and
// announces the slot about to run.
type EnterAction struct{}

// Run implements maa.CustomActionRunner.
func (a *EnterAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	opts, err := readAttachOptions(ctx)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolEnter").
			Msg("failed to read attach options")
		return false
	}
	slots := opts.slotList()
	if len(slots) == 0 {
		log.Error().
			Str("component", "CombatProtocolEnter").
			Msg("no combat task slot assembled, nothing to rotate")
		return false
	}

	stateMu.Lock()
	st, err := loadState()
	if err != nil {
		stateMu.Unlock()
		log.Error().
			Err(err).
			Str("component", "CombatProtocolEnter").
			Msg("failed to load rotation state")
		return false
	}
	changed := reconcileState(&st, slots, opts.normalizedMode())
	if st.Mode == ModeDate {
		previous := st.LastRunDate
		if advanceDateOnNewDay(&st, schedule.GameDate(time.Now())) {
			changed = true
			if previous != "" {
				log.Info().
					Str("component", "CombatProtocolEnter").
					Str("last_run_date", previous).
					Str("today", st.LastRunDate).
					Int("slot_index", st.CurrentIndex).
					Msg("new game day, date rotation advanced")
			}
		}
	}
	if st.PendingCoupon || st.RotatedThisBattle {
		st.PendingCoupon = false
		st.RotatedThisBattle = false
		changed = true
	}
	if changed {
		if err := saveState(st); err != nil {
			log.Error().
				Err(err).
				Str("component", "CombatProtocolEnter").
				Msg("failed to save rotation state")
		}
	}
	slot, ok := st.currentSlot()
	mode := st.Mode
	index := st.CurrentIndex
	total := len(st.Slots)
	accum := 0
	if ok {
		accum = st.Accum[index]
	}
	stateMu.Unlock()

	if !ok {
		log.Error().
			Str("component", "CombatProtocolEnter").
			Msg("rotation state has no current slot")
		return false
	}

	log.Info().
		Str("component", "CombatProtocolEnter").
		Str("mode", mode).
		Int("slot_index", index).
		Str("task", slot.Task).
		Int("threshold", slot.Threshold).
		Int("accum", accum).
		Int("slots", total).
		Msg("container run starting")
	maafocus.Print(ctx, i18n.T(
		"combatprotocol.start",
		i18n.T(taskLabelKey(slot.Task)),
		i18n.T(modeLabelKey(mode)),
		index+1,
		total,
	))
	return true
}
