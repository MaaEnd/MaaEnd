// Copyright (c) 2026 Harry Huang
package maptracker

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomRecognitionRunner = &Infer{}
)

// Register registers all custom recognition components for map-tracker package
func Register() {
	ensureResourcePathSink()
	maa.AgentServerRegisterCustomRecognition("MapTrackerInfer", &Infer{})
}
