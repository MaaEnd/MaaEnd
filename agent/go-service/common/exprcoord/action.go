package exprcoord

import (
	"encoding/json"
	"fmt"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &MpExprClickAction{}
var _ maa.CustomActionRunner = &MpExprTouchMoveAction{}
var _ maa.CustomActionRunner = &MpExprSwipeAction{}
var _ maa.CustomActionRunner = &MpExprOCRBottomStateResetAction{}

type MpExprClickAction struct{}

type MpExprTouchMoveAction struct{}

type MpExprSwipeAction struct{}

type MpExprOCRBottomStateResetAction struct{}

type clickParams struct {
	Target       []any `json:"target"`
	TargetOffset []any `json:"target_offset,omitempty"`
	Contact      int   `json:"contact,omitempty"`
}

type touchMoveParams struct {
	Target       []any `json:"target"`
	TargetOffset []any `json:"target_offset,omitempty"`
	Contact      int   `json:"contact,omitempty"`
	Pressure     int   `json:"pressure,omitempty"`
}

type swipeParams struct {
	Begin    []any `json:"begin"`
	End      []any `json:"end"`
	Duration any   `json:"duration,omitempty"`
	EndHold  any   `json:"end_hold,omitempty"`
	Contact  int   `json:"contact,omitempty"`
}

type ocrBottomStateResetParams struct {
	Key string `json:"key,omitempty"`
}

func (a *MpExprOCRBottomStateResetAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params ocrBottomStateResetParams
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().Err(err).Str("component", "MpExprOCRBottomStateReset").Msg("failed to parse params")
			return false
		}
	}
	resetMpExprOCRBottomState(params.Key)
	return true
}

func currentImageSize(ctx *maa.Context) (int, int, error) {
	controller := ctx.GetTasker().GetController()
	img, err := controller.CacheImage()
	if err == nil && img != nil {
		bounds := img.Bounds()
		return bounds.Dx(), bounds.Dy(), nil
	}
	rw, rh, err := controller.GetResolution()
	if err != nil {
		return 0, 0, err
	}
	return int(rw), int(rh), nil
}

// Run executes Click with expression-based target.
func (a *MpExprClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params clickParams
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "MpExprClick").Msg("failed to parse params")
		return false
	}
	w, h, err := currentImageSize(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprClick").Msg("failed to get image size")
		return false
	}
	var rect maa.Rect
	switch len(params.Target) {
	case 4:
		rect, err = ResolveRect(params.Target, w, h)
	case 2:
		var x, y int
		x, y, err = ResolvePoint(params.Target, w, h)
		rect = maa.Rect{x, y, 1, 1}
	default:
		err = fmt.Errorf("target requires 2 or 4 elements, got %d", len(params.Target))
	}
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprClick").Str("expr", marshalRaw(params.Target)).Msg("failed to resolve target")
		return false
	}

	targetOffset := maa.Rect{}
	if len(params.TargetOffset) > 0 {
		targetOffset, err = ResolveRect(params.TargetOffset, w, h)
		if err != nil {
			log.Error().Err(err).Str("component", "MpExprClick").Str("expr", marshalRaw(params.TargetOffset)).Msg("failed to resolve target_offset")
			return false
		}
	}

	pipeline := maa.NewPipeline()
	node := maa.NewNode("MpExprClickInner").SetAction(maa.ActClick(maa.ClickParam{
		Target:       maa.NewTargetRect(rect),
		TargetOffset: targetOffset,
		Contact:      params.Contact,
	}))
	pipeline.AddNode(node)
	_, err = ctx.RunAction(node.Name, rect, "", pipeline)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprClick").Msg("inner action failed")
		return false
	}
	return true
}

// Run executes TouchMove with expression-based target.
func (a *MpExprTouchMoveAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params touchMoveParams
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "MpExprTouchMove").Msg("failed to parse params")
		return false
	}
	w, h, err := currentImageSize(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprTouchMove").Msg("failed to get image size")
		return false
	}
	var rect maa.Rect
	switch len(params.Target) {
	case 4:
		rect, err = ResolveRect(params.Target, w, h)
	case 2:
		var x, y int
		x, y, err = ResolvePoint(params.Target, w, h)
		rect = maa.Rect{x, y, 1, 1}
	default:
		err = fmt.Errorf("target requires 2 or 4 elements, got %d", len(params.Target))
	}
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprTouchMove").Str("expr", marshalRaw(params.Target)).Msg("failed to resolve target")
		return false
	}

	targetOffset := maa.Rect{}
	if len(params.TargetOffset) > 0 {
		targetOffset, err = ResolveRect(params.TargetOffset, w, h)
		if err != nil {
			log.Error().Err(err).Str("component", "MpExprTouchMove").Str("expr", marshalRaw(params.TargetOffset)).Msg("failed to resolve target_offset")
			return false
		}
	}

	pipeline := maa.NewPipeline()
	node := maa.NewNode("MpExprTouchMoveInner").SetAction(maa.ActTouchMove(maa.TouchMoveParam{
		Target:       maa.NewTargetRect(rect),
		TargetOffset: targetOffset,
		Contact:      params.Contact,
		Pressure:     params.Pressure,
	}))
	pipeline.AddNode(node)
	_, err = ctx.RunAction(node.Name, rect, "", pipeline)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprTouchMove").Msg("inner action failed")
		return false
	}
	return true
}

// Run executes Swipe with expression-based begin and end points.
func (a *MpExprSwipeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params swipeParams
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "MpExprSwipe").Msg("failed to parse params")
		return false
	}
	w, h, err := currentImageSize(ctx)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprSwipe").Msg("failed to get image size")
		return false
	}
	begin, err := resolveSwipeTarget(params.Begin, w, h)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprSwipe").Str("expr", marshalRaw(params.Begin)).Msg("failed to resolve begin")
		return false
	}
	ends, err := resolveSwipeTargets(params.End, w, h)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprSwipe").Str("expr", marshalRaw(params.End)).Msg("failed to resolve end")
		return false
	}

	swipe := maa.SwipeParam{
		Begin:   begin,
		End:     ends,
		Contact: params.Contact,
	}
	if durations, ok := resolveDurations(params.Duration, len(ends)); ok {
		swipe.Duration = durations
	}
	if endHolds, ok := resolveDurations(params.EndHold, len(ends)); ok {
		swipe.EndHold = endHolds
	}

	pipeline := maa.NewPipeline()
	node := maa.NewNode("MpExprSwipeInner").SetAction(maa.ActSwipe(swipe))
	pipeline.AddNode(node)
	_, err = ctx.RunAction(node.Name, maa.Rect{}, "", pipeline)
	if err != nil {
		log.Error().Err(err).Str("component", "MpExprSwipe").Msg("inner action failed")
		return false
	}
	return true
}

func resolveSwipeTargets(raw []any, w, h int) ([]maa.Target, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("swipe end requires at least one target")
	}
	if _, ok := raw[0].([]any); !ok {
		target, err := resolveSwipeTarget(raw, w, h)
		if err != nil {
			return nil, err
		}
		return []maa.Target{target}, nil
	}

	targets := make([]maa.Target, 0, len(raw))
	for i, item := range raw {
		targetRaw, ok := item.([]any)
		if !ok {
			return nil, fmt.Errorf("swipe end target %d must be an array", i)
		}
		target, err := resolveSwipeTarget(targetRaw, w, h)
		if err != nil {
			return nil, fmt.Errorf("swipe end target %d: %w", i, err)
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func resolveSwipeTarget(raw []any, w, h int) (maa.Target, error) {
	switch len(raw) {
	case 2:
		x, y, err := ResolvePoint(raw, w, h)
		if err != nil {
			return maa.Target{}, err
		}
		return maa.NewTargetRect(maa.Rect{x, y, 1, 1}), nil
	case 4:
		rect, err := ResolveRect(raw, w, h)
		if err != nil {
			return maa.Target{}, err
		}
		return maa.NewTargetRect(rect), nil
	default:
		return maa.Target{}, fmt.Errorf("swipe target requires 2 or 4 elements, got %d", len(raw))
	}
}

func resolveDurations(raw any, count int) ([]time.Duration, bool) {
	switch value := raw.(type) {
	case nil:
		return nil, false
	case float64:
		return repeatDuration(int(value), count), true
	case []any:
		durations := make([]time.Duration, 0, len(value))
		for _, item := range value {
			number, ok := item.(float64)
			if !ok {
				continue
			}
			durations = append(durations, time.Duration(int(number))*time.Millisecond)
		}
		return durations, len(durations) > 0
	default:
		return nil, false
	}
}

func repeatDuration(value int, count int) []time.Duration {
	durations := make([]time.Duration, count)
	for i := range durations {
		durations[i] = time.Duration(value) * time.Millisecond
	}
	return durations
}
