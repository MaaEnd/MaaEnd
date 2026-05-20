package aspectratio

import maa "github.com/MaaXYZ/maa-framework-go/v4"

var defaultChecker *AspectRatioChecker

// Register registers the aspect ratio checker and resolution actions.
func Register() {
	defaultChecker = &AspectRatioChecker{}
	maa.AgentServerAddTaskerSink(defaultChecker)
	maa.AgentServerAddContextSink(defaultChecker)
	maa.AgentServerRegisterCustomAction("AspectRatioRecordResolutionAndClick", &RecordResolutionAndClickAction{})
	maa.AgentServerRegisterCustomAction("AspectRatioRestoreResolution", &RestoreResolutionAction{})
}

// Cleanup is kept for main shutdown compatibility.
func Cleanup() {}
