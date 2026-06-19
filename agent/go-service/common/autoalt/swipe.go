package autoalt

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type autoAltSwipeParam struct {
	// Begin overrides the swipe start. When omitted, the sub-node Swipe action
	// uses arg.Box as begin (Swipe default begin=true).
	Begin []int `json:"begin,omitempty"`
	// End overrides the swipe end. When omitted, the sub-node Swipe action
	// uses arg.Box as end (Swipe default end=true).
	End []int `json:"end,omitempty"`
	// BeginOffset is forwarded to the sub-node Swipe action's begin_offset.
	BeginOffset []int `json:"begin_offset,omitempty"`
	// EndOffset is forwarded to the sub-node Swipe action's end_offset.
	EndOffset []int `json:"end_offset,omitempty"`
	Duration  *int  `json:"duration,omitempty"`
	EndHold   *int  `json:"end_hold,omitempty"`
	OnlyHover *bool `json:"only_hover,omitempty"`
}

type AutoAltSwipeAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &AutoAltSwipeAction{}

func (a *AutoAltSwipeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	swipeOverride := map[string]any{}
	if param := arg.CustomActionParam; param != "" {
		var p autoAltSwipeParam
		if err := json.Unmarshal([]byte(param), &p); err != nil {
			log.Error().
				Err(err).
				Str("component", "AutoAltSwipeAction").
				Str("custom_action_param", param).
				Msg("failed to parse custom action param")
			return false
		}
		if len(p.Begin) == 2 || len(p.Begin) == 4 {
			swipeOverride["begin"] = p.Begin
		}
		if len(p.End) == 2 || len(p.End) == 4 {
			swipeOverride["end"] = p.End
		}
		if len(p.BeginOffset) == 4 {
			swipeOverride["begin_offset"] = p.BeginOffset
		}
		if len(p.EndOffset) == 4 {
			swipeOverride["end_offset"] = p.EndOffset
		}
		if p.Duration != nil {
			swipeOverride["duration"] = *p.Duration
		}
		if p.EndHold != nil {
			swipeOverride["end_hold"] = *p.EndHold
		}
		if p.OnlyHover != nil {
			swipeOverride["only_hover"] = *p.OnlyHover
		}
	}

	if _, err := ctx.RunAction("__AutoAltClickAltKeyDownAction",
		maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltSwipeAction").
			Msg("failed to run __AutoAltClickAltKeyDownAction")
		return false
	}

	var swipeErr error
	// begin/end default to arg.Box via sub-node Swipe (begin/end=true); param fields override when set.
	if len(swipeOverride) > 0 {
		_, swipeErr = ctx.RunAction("__AutoAltSwipeMouseSwipeAction",
			arg.Box, "", map[string]any{
				"__AutoAltSwipeMouseSwipeAction": swipeOverride,
			})
	} else {
		_, swipeErr = ctx.RunAction("__AutoAltSwipeMouseSwipeAction",
			arg.Box, "", nil)
	}
	if swipeErr != nil {
		log.Error().
			Err(swipeErr).
			Str("component", "AutoAltSwipeAction").
			Msg("failed to run __AutoAltSwipeMouseSwipeAction")
	}

	if _, err := ctx.RunAction("__AutoAltClickAltKeyUpAction",
		maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltSwipeAction").
			Msg("failed to run __AutoAltClickAltKeyUpAction")
	}

	return swipeErr == nil
}
