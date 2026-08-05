// Copyright (c) 2026 Harry Huang
package maptrackercompatible

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomActionRunner = &MapTrackerMoveCompatible{}

// MapTrackerMoveCompatible is the legacy placeholder action of the removed MapTracker system.
type MapTrackerMoveCompatible struct{}

func (r *MapTrackerMoveCompatible) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	panic("MapTracker system was removed")
}
