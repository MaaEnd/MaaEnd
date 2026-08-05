// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomActionRunner = &MapTrackerToward{}

// MapTrackerToward is the legacy placeholder action of the removed MapTracker system.
type MapTrackerToward struct{}

func (r *MapTrackerToward) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	panic("MapTracker system was removed")
}
