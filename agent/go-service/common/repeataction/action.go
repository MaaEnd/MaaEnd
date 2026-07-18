package repeataction

import (
	"encoding/json"
	"image"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	defaultRepeatCount = 3
	defaultIntervalMs  = 200
	innerActionEntry   = "__RepeatUntilActionInner"
)

type repeatUntilFoundParam struct {
	Action       string   `json:"action,omitempty"`
	CustomAction string   `json:"custom_action,omitempty"`
	WaitNodes    []string `json:"wait_nodes"`
	RepeatCount  int      `json:"repeat_count,omitempty"`
	IntervalMs   int64    `json:"interval_ms,omitempty"`
}

type repeatUntilNotFoundParam struct {
	Action       string `json:"action,omitempty"`
	CustomAction string `json:"custom_action,omitempty"`
	WaitNode     string `json:"wait_node"`
	RepeatCount  int    `json:"repeat_count,omitempty"`
	IntervalMs   int64  `json:"interval_ms,omitempty"`
}

// RepeatUntilFoundAction runs a built-in or custom action until any wait node hits.
type RepeatUntilFoundAction struct{}

var _ maa.CustomActionRunner = &RepeatUntilFoundAction{}

func (a *RepeatUntilFoundAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p repeatUntilFoundParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		log.Error().Err(err).Str("component", "RepeatUntilFoundAction").Msg("failed to parse params")
		return false
	}
	return runRepeatUntil(ctx, arg, "RepeatUntilFoundAction", p.Action, p.CustomAction, p.WaitNodes, p.RepeatCount, p.IntervalMs, true)
}

// RepeatUntilNotFoundAction runs a built-in or custom action until the wait node misses.
type RepeatUntilNotFoundAction struct{}

var _ maa.CustomActionRunner = &RepeatUntilNotFoundAction{}

func (a *RepeatUntilNotFoundAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p repeatUntilNotFoundParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		log.Error().Err(err).Str("component", "RepeatUntilNotFoundAction").Msg("failed to parse params")
		return false
	}
	var waitNodes []string
	if p.WaitNode != "" {
		waitNodes = []string{p.WaitNode}
	}
	return runRepeatUntil(ctx, arg, "RepeatUntilNotFoundAction", p.Action, p.CustomAction, waitNodes, p.RepeatCount, p.IntervalMs, false)
}

func runRepeatUntil(
	ctx *maa.Context,
	arg *maa.CustomActionArg,
	component, action, customAction string,
	waitNodes []string,
	repeatCount int,
	intervalMs int64,
	untilFound bool,
) bool {
	hasAction := action != ""
	hasCustom := customAction != ""
	if hasAction == hasCustom || len(waitNodes) == 0 || intervalMs < 0 {
		log.Error().
			Str("component", component).
			Str("action", action).
			Str("custom_action", customAction).
			Int("wait_nodes", len(waitNodes)).
			Int64("interval_ms", intervalMs).
			Msg("invalid params")
		return false
	}

	if repeatCount <= 0 {
		repeatCount = defaultRepeatCount
	}
	if intervalMs == 0 {
		intervalMs = defaultIntervalMs
	}

	ctrl := ctx.GetTasker().GetController()
	interval := time.Duration(intervalMs) * time.Millisecond

	for i := 0; i < repeatCount; i++ {
		if ctx.GetTasker().Stopping() {
			return false
		}

		actionType, actionParam := action, map[string]any{}
		if hasCustom {
			actionType = "Custom"
			actionParam = map[string]any{"custom_action": customAction}
		}
		if _, err := ctx.RunAction(innerActionEntry, arg.Box, "", map[string]any{
			innerActionEntry: map[string]any{
				"action": map[string]any{"type": actionType, "param": actionParam},
			},
		}); err != nil {
			log.Warn().Err(err).Str("component", component).Int("attempt", i+1).Msg("inner action failed")
		}

		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil || img == nil {
			log.Warn().Err(err).Str("component", component).Msg("cache image failed")
		} else if waitConditionMet(ctx, img, waitNodes, untilFound) {
			return true
		}

		if i+1 < repeatCount {
			time.Sleep(interval) // interval_ms between failed attempts
		}
	}
	return false
}

func waitConditionMet(ctx *maa.Context, img image.Image, nodes []string, untilFound bool) bool {
	if untilFound {
		for _, node := range nodes {
			if detail, err := ctx.RunRecognition(node, img); err == nil && detail != nil && detail.Hit {
				return true
			}
		}
		return false
	}
	detail, err := ctx.RunRecognition(nodes[0], img)
	return err != nil || detail == nil || !detail.Hit
}
