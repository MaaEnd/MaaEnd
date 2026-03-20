// Copyright (c) 2026 Harry Huang
package maptracker

import (
	mt "github.com/MaaXYZ/MaaEnd/agent/go-service/map-tracker/internal"
	"github.com/MaaXYZ/maa-framework-go/v4"
)

// Register registers all custom recognition components for map-tracker package
func Register() {
	mt.EnsureResourcePathSink()

	maa.AgentServerRegisterCustomRecognition("MapTrackerInfer", &MapTrackerInfer{})
	maa.AgentServerRegisterCustomRecognition("MapTrackerBigMapInfer", &MapTrackerBigMapInfer{})
	maa.AgentServerRegisterCustomRecognition("MapTrackerAssertLocation", &MapTrackerAssertLocation{})
	maa.AgentServerRegisterCustomAction("MapTrackerMove", &MapTrackerMove{})
	maa.AgentServerRegisterCustomAction("MapTrackerBigMapPick", &MapTrackerBigMapPick{})
}
