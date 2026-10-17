package autoalt

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	autoSwipeClickHoverNode = "__AutoSwipeClickHoverAction"
	autoSwipeClickClickNode = "__AutoSwipeClickClickAction"
)

// AutoSwipeClickAction hovers from the screen center to the recognition box, waits, then clicks.
// PC runs both pipeline nodes. ADB overrides the hover node to DoNothing and only clicks.
type AutoSwipeClickAction struct{}

var _ maa.CustomActionRunner = &AutoSwipeClickAction{}

func (a *AutoSwipeClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "AutoSwipeClickAction").Msg("nil context or arg")
		return false
	}
	if arg.Box.Width() <= 0 || arg.Box.Height() <= 0 {
		log.Error().
			Str("component", "AutoSwipeClickAction").
			Int("width", arg.Box.Width()).
			Int("height", arg.Box.Height()).
			Msg("recognition box is empty")
		return false
	}

	if !runPipelineAction(ctx, "AutoSwipeClickAction", autoSwipeClickHoverNode, arg.Box) {
		return false
	}
	return runPipelineAction(ctx, "AutoSwipeClickAction", autoSwipeClickClickNode, arg.Box)
}
