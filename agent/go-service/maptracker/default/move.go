// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomActionRunner = &MapTrackerMove{}

// MapTrackerMove is the legacy placeholder action of the removed MapTracker system.
type MapTrackerMove struct{}

func (r *MapTrackerMove) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	panic("MapTracker system was removed")
}
