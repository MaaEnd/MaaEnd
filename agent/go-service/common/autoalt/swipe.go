package autoalt

import (
	"encoding/json"
	"time"

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

func intsToTarget(p []int) (maa.Target, bool) {
	switch len(p) {
	case 2:
		return maa.NewTargetRect(maa.Rect{p[0], p[1], 1, 1}), true
	case 4:
		return maa.NewTargetRect(maa.Rect{p[0], p[1], p[2], p[3]}), true
	default:
		return maa.Target{}, false
	}
}

func intsToRect(p []int) (maa.Rect, bool) {
	if len(p) != 4 {
		return maa.Rect{}, false
	}
	return maa.Rect{p[0], p[1], p[2], p[3]}, true
}

func buildSwipeParam(p autoAltSwipeParam) (maa.SwipeParam, bool) {
	hasBegin := validSwipePoint(p.Begin)
	hasEnd := validSwipePoint(p.End)
	if hasBegin != hasEnd {
		return maa.SwipeParam{}, false
	}

	var param maa.SwipeParam
	if hasBegin {
		begin, ok := intsToTarget(p.Begin)
		if !ok {
			return maa.SwipeParam{}, false
		}
		end, ok := intsToTarget(p.End)
		if !ok {
			return maa.SwipeParam{}, false
		}
		param.Begin = begin
		param.End = []maa.Target{end}
	} else {
		param.Begin = maa.NewTargetBool(true)
		param.End = []maa.Target{maa.NewTargetBool(true)}
		if r, ok := intsToRect(p.BeginOffset); ok {
			param.BeginOffset = r
		}
		if r, ok := intsToRect(p.EndOffset); ok {
			param.EndOffset = []maa.Rect{r}
		}
	}

	if p.Duration != nil {
		param.Duration = []time.Duration{time.Duration(*p.Duration) * time.Millisecond}
	}
	if p.EndHold != nil {
		param.EndHold = []time.Duration{time.Duration(*p.EndHold) * time.Millisecond}
	}
	if p.OnlyHover != nil {
		param.OnlyHover = *p.OnlyHover
	}

	return param, true
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

	swipeParam, ok := buildSwipeParam(p)
	if !ok {
		log.Error().
			Str("component", "AutoAltSwipeAction").
			Interface("begin", p.Begin).
			Interface("end", p.End).
			Msg("begin and end must both be provided or both omitted")
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

	if _, err := ctx.RunAction("__AutoAltClickAltKeyDownAction",
		maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoAltSwipeAction").
			Msg("failed to run __AutoAltClickAltKeyDownAction")
		return false
	}

	// Match __AutoAltSwipeMouseSwipeAction pre_delay for SeizeInput reliability.
	time.Sleep(300 * time.Millisecond)

	_, swipeErr := ctx.RunActionDirect(maa.ActionTypeSwipe, &swipeParam, box, arg.RecognitionDetail)
	if swipeErr != nil {
		log.Error().
			Err(swipeErr).
			Str("component", "AutoAltSwipeAction").
			Interface("box", box).
			Interface("begin_offset", p.BeginOffset).
			Interface("end_offset", p.EndOffset).
			Msg("failed to run swipe action")
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
