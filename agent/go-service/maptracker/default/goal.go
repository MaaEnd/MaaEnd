// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomActionRunner = &MapTrackerGoal{}

// MapTrackerGoal is the legacy placeholder action of the removed MapTracker system.
type MapTrackerGoal struct{}

func (r *MapTrackerGoal) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	panic("MapTracker system was removed")
}
