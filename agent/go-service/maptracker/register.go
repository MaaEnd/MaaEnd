// Copyright (c) 2026 Harry Huang
package maptracker

import (
	maptrackerbigmap "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/bigmap"
	maptrackercompatible "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/compatible"
	maptrackerdefault "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/default"
	"github.com/MaaXYZ/maa-framework-go/v4"
)

// Register registers all custom recognition components for maptracker package
func Register() {
	maa.AgentServerRegisterCustomRecognition("MapTrackerInfer", &maptrackerdefault.MapTrackerInfer{})
	maa.AgentServerRegisterCustomRecognition("MapTrackerBigMapInfer", &maptrackerbigmap.MapTrackerBigMapInfer{})
	maa.AgentServerRegisterCustomRecognition("MapTrackerAssertLocation", &maptrackerdefault.MapTrackerAssertLocation{})
	maa.AgentServerRegisterCustomRecognition("MapTrackerAssertLocationCompatible", &maptrackercompatible.MapTrackerAssertLocationCompatible{})
	maa.AgentServerRegisterCustomAction("MapTrackerMove", &maptrackerdefault.MapTrackerMove{})
	maa.AgentServerRegisterCustomAction("MapTrackerMoveCompatible", &maptrackercompatible.MapTrackerMoveCompatible{})
	maa.AgentServerRegisterCustomAction("MapTrackerBigMapPick", &maptrackerbigmap.MapTrackerBigMapPick{})
}
