package autoaltclick

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type autoAltLongPressParam struct {
	Duration int64 `json:"duration"`
}

type AutoAltClickAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &AutoAltClickAction{}

func (a *AutoAltClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	ctx.RunAction("__AutoAltClickAltKeyDownAction",
		maa.Rect{0, 0, 0, 0}, "", nil)
	ctx.RunAction("__AutoAltClickMouseClickAction",
		arg.Box, "", nil)
	ctx.RunAction("__AutoAltClickAltKeyUpAction",
		maa.Rect{0, 0, 0, 0}, "", nil)
	return true
}

type AutoAltLongPressAction struct{}

var _ maa.CustomActionRunner = &AutoAltLongPressAction{}

func (a *AutoAltLongPressAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p autoAltLongPressParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil || p.Duration <= 0 {
		return false
	}

	ctx.RunAction("__AutoAltClickAltKeyDownAction",
		maa.Rect{0, 0, 0, 0}, "", nil)
	ctx.RunAction("__AutoAltLongPressMouseLongPressAction",
		arg.Box, "", map[string]any{
			"__AutoAltLongPressMouseLongPressAction": map[string]any{
				"duration": p.Duration,
			},
		})
	ctx.RunAction("__AutoAltClickAltKeyUpAction",
		maa.Rect{0, 0, 0, 0}, "", nil)
	return true
}
