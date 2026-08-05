// Copyright (c) 2026 Harry Huang
package maptrackerbigmap

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomRecognitionRunner = &MapTrackerBigMapFindImage{}

// MapTrackerBigMapFindImage is the legacy placeholder recognition of the removed MapTracker system.
type MapTrackerBigMapFindImage struct{}

func (r *MapTrackerBigMapFindImage) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	panic("MapTracker system was removed")
}
