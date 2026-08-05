// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomRecognitionRunner = &MapTrackerAssertLocation{}

// MapTrackerAssertLocation is the legacy placeholder recognition of the removed MapTracker system.
type MapTrackerAssertLocation struct{}

func (r *MapTrackerAssertLocation) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	panic("MapTracker system was removed")
}
