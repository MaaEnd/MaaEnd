// Copyright (c) 2026 Harry Huang
package maptrackerbigmap

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomRecognitionRunner = &MapTrackerBigMapInfer{}

// MapTrackerBigMapInfer is the legacy placeholder recognition of the removed MapTracker system.
type MapTrackerBigMapInfer struct{}

func (r *MapTrackerBigMapInfer) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	panic("MapTracker system was removed")
}
