package scenemanager

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type SceneManagerMenuListClickItemAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &SceneManagerMenuListClickItemAction{}

func (a *SceneManagerMenuListClickItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// https://github.com/MaaEnd/MaaEnd/issues/1456

	// 取 Box 上边中点并向上偏移 15px
	topMid := maa.Rect{
		arg.Box.X() + arg.Box.Width()/2,
		arg.Box.Y() - 15,
		1,
		1,
	}
	ctx.RunAction("__SceneClickAction",
		topMid, "", nil)
	return true
}
