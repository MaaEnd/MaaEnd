// Package maafocus provides helpers to send focus payloads from go-service
// events so the client can render related UI focus hints.
package maafocus

import (
	"fmt"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const nodeName = "_GO_SERVICE_FOCUS_"

// Print sends focus payload on node action starting event,
// so that the client can make the payload visible to users.
//
// The actual UI rendering is handled by client side.
// See https://maafw.com/en/docs/3.1-PipelineProtocol#node-notifications
//
// If the content is too large, consider using [PrintLargeContent]
// to avoid potential performance issues.
func Print(ctx *maa.Context, content string) {
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

	if _, err := ctx.RunAction(nodeName, maa.Rect{}, "", pp); err != nil {
		log.Warn().
			Err(err).
			Str("event", "node_action_starting").
			Msg("failed to send focus")
	}
}

// PrintLargeContent sends payload to [fmt.Println] for large content.
//
// Instead of [Print], this function will not record the content into Maa's log system.
//
// Note that this function does not require a context argument.
func PrintLargeContent(content string) {
	fmt.Println(content)
}
