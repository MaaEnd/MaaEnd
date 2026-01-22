package main

import (
	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// myAction implements a simple custom action that logs and succeeds.
type myAction struct{}

func (a *myAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Debug().
		Str("action", arg.CustomActionName).
		Str("task", arg.CurrentTaskName).
		Str("param", arg.CustomActionParam).
		Int("box_x", arg.Box.X()).
		Int("box_y", arg.Box.Y()).
		Int("box_w", arg.Box.Width()).
		Int("box_h", arg.Box.Height()).
		Msg("Running action")

	// Example: Run a nested task using context
	// ctx.RunTask("SomeOtherNode", `{"SomeOtherNode": {"action": "DoNothing"}}`)

	return true
}
