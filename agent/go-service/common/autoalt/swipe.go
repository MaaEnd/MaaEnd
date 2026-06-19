package autoalt

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type autoAltSwipeParam struct {
	Begin       []int `json:"begin,omitempty"`
	End         []int `json:"end,omitempty"`
	BeginOffset []int `json:"begin_offset,omitempty"`
	EndOffset   []int `json:"end_offset,omitempty"`
	Duration    *int  `json:"duration,omitempty"`
	EndHold     *int  `json:"end_hold,omitempty"`
	OnlyHover   *bool `json:"only_hover,omitempty"`
}

type AutoAltSwipeAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &AutoAltSwipeAction{}

func validSwipePoint(p []int) bool {
	return len(p) == 2 || len(p) == 4
}

func validRectOffset(p []int) bool {
	return len(p) == 4
}

func (a *AutoAltSwipeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p autoAltSwipeParam
	if param := arg.CustomActionParam; param != "" {
		if err := json.Unmarshal([]byte(param), &p); err != nil {
			log.Error().
				Err(err).
				Str("component", "AutoAltSwipeAction").
				Str("custom_action_param", param).
				Msg("failed to parse custom action param")
			return false
		}
	}

	hasBegin := validSwipePoint(p.Begin)
	hasEnd := validSwipePoint(p.End)
	if hasBegin != hasEnd {
		log.Error().
			Str("component", "AutoAltSwipeAction").
			Interface("begin", p.Begin).
			Interface("end", p.End).
			Msg("begin and end must both be provided or both omitted")
		return false
	}
	if len(p.BeginOffset) > 0 && !validRectOffset(p.BeginOffset) {
		log.Error().
			Str("component", "AutoAltSwipeAction").
			Interface("begin_offset", p.BeginOffset).
			Msg("begin_offset must be [dx, dy, dw, dh]")
		return false
	}
	if len(p.EndOffset) > 0 && !validRectOffset(p.EndOffset) {
		log.Error().
			Str("component", "AutoAltSwipeAction").
			Interface("end_offset", p.EndOffset).
			Msg("end_offset must be [dx, dy, dw, dh]")
		return false
	}

	box := arg.Box
	if hasBegin {
		box = maa.Rect{0, 0, 0, 0}
	} else if arg.RecognitionDetail == nil || !arg.RecognitionDetail.Hit {
		log.Error().
			Str("component", "AutoAltSwipeAction").
			Msg("recognition box is required when begin/end are omitted")
		return false
	}

	swipeOverride := map[string]any{}
	if hasBegin {
		swipeOverride["begin"] = p.Begin
		swipeOverride["end"] = p.End
	} else {
		swipeOverride["begin"] = true
		swipeOverride["end"] = true
	}
	if validRectOffset(p.BeginOffset) {
		swipeOverride["begin_offset"] = p.BeginOffset
	}
	if validRectOffset(p.EndOffset) {
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

	if _, err := ctx.RunAction("__AutoAltClickAltKeyDownAction",
		maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltSwipeAction").
			Msg("failed to run __AutoAltClickAltKeyDownAction")
		return false
	}

	_, swipeErr := ctx.RunAction("__AutoAltSwipeMouseSwipeAction",
		box, "", map[string]any{
			"__AutoAltSwipeMouseSwipeAction": swipeOverride,
		})
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
