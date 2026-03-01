// Package maafocus provides helpers to send focus payloads from go-service
// events so the client can render related UI focus hints.
package maafocus

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const nodeName = "_GO_SERVICE_FOCUS_"

// NodeActionStarting sends focus payload on node action starting event.
// The actual UI rendering is handled by client side.
func NodeActionStarting(ctx *maa.Context, content string) {
	if ctx == nil {
		log.Warn().
			Str("event", "node_action_starting").
			Msg("context is nil, skip sending focus")
		return
	}

	pp := maa.NewPipeline()
	node := maa.NewNode(nodeName).
		SetFocus(map[string]any{
			maa.EventNodeAction.Starting(): content,
		}).
		SetPreDelay(0).
		SetPostDelay(0)
	pp.AddNode(node)

	if _, err := ctx.RunTask(nodeName, pp); err != nil {
		log.Warn().
			Err(err).
			Str("event", "node_action_starting").
			Msg("failed to send focus")
	}
}
