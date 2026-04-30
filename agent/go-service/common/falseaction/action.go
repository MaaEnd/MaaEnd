package falseaction

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type FalseAction struct{}

var _ maa.CustomActionRunner = &FalseAction{}

func (a *FalseAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	return false
}
