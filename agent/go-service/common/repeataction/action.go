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
	defaultIntervalMs  = 1000
	innerActionEntry   = "__RepeatUntilActionInner"
)

type repeatUntilFoundParam struct {
	// Action is a built-in action type (e.g. "Click"). Mutually exclusive
	// with CustomAction; exactly one of them must be provided.
	Action string `json:"action"`
	// CustomAction repeats a registered custom action (e.g. "AutoAltClickAction").
	CustomAction string `json:"custom_action,omitempty"`
	// CustomActionParam is forwarded to the custom action as-is.
	CustomActionParam json.RawMessage `json:"custom_action_param,omitempty"`
	WaitNodes         []string        `json:"wait_nodes"`
	RepeatCount       int             `json:"repeat_count,omitempty"`
	IntervalMs        int64           `json:"interval_ms,omitempty"`
}

type repeatUntilNotFoundParam struct {
	Action            string          `json:"action"`
	CustomAction      string          `json:"custom_action,omitempty"`
	CustomActionParam json.RawMessage `json:"custom_action_param,omitempty"`
	WaitNode          string          `json:"wait_node"`
	RepeatCount       int             `json:"repeat_count,omitempty"`
	IntervalMs        int64           `json:"interval_ms,omitempty"`
}

// RepeatUntilFoundAction repeats a built-in or custom action until any wait node hits.
type RepeatUntilFoundAction struct{}

var _ maa.CustomActionRunner = &RepeatUntilFoundAction{}

func (a *RepeatUntilFoundAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p repeatUntilFoundParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		log.Error().Err(err).Str("component", "RepeatUntilFoundAction").Msg("failed to parse params")
		return false
	}
	actionNode, ok := buildActionNode(p.Action, p.CustomAction, p.CustomActionParam)
	if !ok {
		log.Error().Str("component", "RepeatUntilFoundAction").Msg("either action or custom_action is required")
		return false
	}
	return runRepeatUntil(ctx, arg, "RepeatUntilFoundAction", actionNode, p.WaitNodes, p.RepeatCount, p.IntervalMs, true)
}

// RepeatUntilNotFoundAction repeats a built-in or custom action until the wait node misses.
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
	actionNode, ok := buildActionNode(p.Action, p.CustomAction, p.CustomActionParam)
	if !ok {
		log.Error().Str("component", "RepeatUntilNotFoundAction").Msg("either action or custom_action is required")
		return false
	}
	return runRepeatUntil(ctx, arg, "RepeatUntilNotFoundAction", actionNode, waitNodes, p.RepeatCount, p.IntervalMs, false)
}

// buildActionNode constructs the v2 action override for the inner node. When
// customAction is set, it wraps the custom action (and its param); otherwise it
// falls back to the built-in action type. Returns false when neither is given.
func buildActionNode(action, customAction string, customActionParam json.RawMessage) (map[string]any, bool) {
	if customAction != "" {
		param := map[string]any{"custom_action": customAction}
		if len(customActionParam) > 0 {
			var v any
			if err := json.Unmarshal(customActionParam, &v); err != nil {
				log.Warn().Err(err).Str("custom_action", customAction).Msg("invalid custom_action_param, forwarding empty param")
			} else {
				param["custom_action_param"] = v
			}
		}
		return map[string]any{"type": "Custom", "param": param}, true
	}
	if action != "" {
		return map[string]any{"type": action, "param": map[string]any{}}, true
	}
	return nil, false
}

func runRepeatUntil(
	ctx *maa.Context,
	arg *maa.CustomActionArg,
	component string,
	actionNode map[string]any,
	waitNodes []string,
	repeatCount int,
	intervalMs int64,
	untilFound bool,
) bool {
	if len(waitNodes) == 0 || intervalMs < 0 {
		log.Error().
			Str("component", component).
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

		if _, err := ctx.RunAction(innerActionEntry, arg.Box, "", map[string]any{
			innerActionEntry: map[string]any{
				"action": actionNode,
			},
		}); err != nil {
			log.Warn().Err(err).Str("component", component).Int("attempt", i+1).Msg("inner action failed")
		}

		// Wait after the action so UI transitions can settle before recognition.
		time.Sleep(interval)

		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil || img == nil {
			log.Warn().Err(err).Str("component", component).Msg("cache image failed")
		} else if waitConditionMet(ctx, img, waitNodes, untilFound) {
			return true
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
