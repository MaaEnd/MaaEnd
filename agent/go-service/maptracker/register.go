// Copyright (c) 2026 Harry Huang
package maptracker

import (
	maptrackerbigmap "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/bigmap"
	maptrackerdefault "github.com/MaaXYZ/MaaEnd/agent/go-service/maptracker/default"
	"github.com/MaaXYZ/maa-framework-go/v4"
)

// Register registers all custom recognition components for maptracker package
func Register() {
	maa.AgentServerRegisterCustomRecognition("MapTrackerInfer", &maptrackerdefault.MapTrackerInfer{})
	maa.AgentServerRegisterCustomRecognition("MapTrackerBigMapInfer", &maptrackerbigmap.MapTrackerBigMapInfer{})
	maa.AgentServerRegisterCustomRecognition("MapTrackerAssertLocation", &maptrackerdefault.MapTrackerAssertLocation{})
	maa.AgentServerRegisterCustomAction("MapTrackerMove", &maptrackerdefault.MapTrackerMove{})
	maa.AgentServerRegisterCustomAction("MapTrackerBigMapPick", &maptrackerbigmap.MapTrackerBigMapPick{})
}
