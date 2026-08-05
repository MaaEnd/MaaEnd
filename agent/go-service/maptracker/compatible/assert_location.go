// Copyright (c) 2026 Harry Huang
package maptrackercompatible

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomRecognitionRunner = &MapTrackerAssertLocationCompatible{}

// MapTrackerAssertLocationCompatible is the legacy placeholder recognition of the removed MapTracker system.
type MapTrackerAssertLocationCompatible struct{}

func (r *MapTrackerAssertLocationCompatible) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	panic("MapTracker system was removed")
}
