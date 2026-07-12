package poststop

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type PostStop struct{}

var _ maa.CustomActionRunner = &PostStop{}

func (a *PostStop) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	ctx.GetTasker().PostStop()
	return true
}
