// Copyright (c) 2026 Harry Huang
package maptrackerbigmap

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomActionRunner = &MapTrackerBigMapPick{}

// MapTrackerBigMapPick is the legacy placeholder action of the removed MapTracker system.
type MapTrackerBigMapPick struct{}

func (r *MapTrackerBigMapPick) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	panic("MapTracker system was removed")
}
