package combatprotocol

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &SetCouponAction{}

// setCouponParam carries whether the sanity consumption permit is active.
type setCouponParam struct {
	Used bool `json:"used"`
}

// SetCouponAction records whether the current battle uses the sanity
// consumption permit (80 extra sanity, doubled rewards). Pipeline nodes call
// it with used=true on the permit / double-reward branch, and with used=false
// when a battle starts or falls back to a single claim, so that
// AccumulateAction can account the correct per-run cost.
type SetCouponAction struct{}

// Run implements maa.CustomActionRunner.
func (a *SetCouponAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	var params setCouponParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolSetCoupon").
			Msg("failed to parse params")
		return false
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	st, err := loadState()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolSetCoupon").
			Msg("failed to load rotation state")
		return false
	}
	if st.PendingCoupon == params.Used {
		return true
	}
	st.PendingCoupon = params.Used
	if err := saveState(st); err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolSetCoupon").
			Msg("failed to save rotation state")
		return false
	}
	log.Debug().
		Str("component", "CombatProtocolSetCoupon").
		Bool("used", params.Used).
		Msg("pending coupon flag updated")
	return true
}
