package main

import (
	"log/slog"

	"github.com/MaaXYZ/maa-framework-go/v3"
)

// myAction implements a simple custom action that logs and succeeds.
type myAction struct{}

func (a *myAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	slog.Debug("Running action",
		"action", arg.CustomActionName,
		"task", arg.CurrentTaskName,
		"param", arg.CustomActionParam,
		"box_x", arg.Box.X(),
		"box_y", arg.Box.Y(),
		"box_w", arg.Box.Width(),
		"box_h", arg.Box.Height(),
	)

	// Example: Run a nested task using context
	// ctx.RunTask("SomeOtherNode", `{"SomeOtherNode": {"action": "DoNothing"}}`)

	return true
}
