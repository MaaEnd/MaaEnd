// Copyright (c) 2026 Harry Huang
package maptrackerdefault

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
)

var _ maa.CustomActionRunner = &MapTrackerZipline{}

// MapTrackerZipline is the legacy placeholder action of the removed MapTracker system.
type MapTrackerZipline struct{}

func (r *MapTrackerZipline) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	panic("MapTracker system was removed")
}
