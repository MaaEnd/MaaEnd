package aspectratio

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	// defaultChecker is the singleton registered with the tasker. Held at
	// package scope so Cleanup() can reach it from main without plumbing.
	defaultChecker *AspectRatioChecker
)

// Register registers the aspect ratio checker as a tasker sink.
func Register() {
	defaultChecker = &AspectRatioChecker{}
	maa.AgentServerAddTaskerSink(defaultChecker)
	maa.AgentServerAddContextSink(defaultChecker)
	maa.AgentServerRegisterCustomAction("AspectRatioRecordResolutionAndClick", &RecordResolutionAndClickAction{})
}

// Cleanup restores fullscreen mode on agent shutdown if this checker switched
// the game to windowed mode during the session.
//
// Intended to be called right after `maa.AgentServerJoin()` returns in main —
// at that point the controller may already be tearing down, but our Alt+Enter
// call goes directly to user32 with the cached HWND, so it still works as long
// as the game process is alive.
func Cleanup() {
	if defaultChecker == nil {
		return
	}
	defaultChecker.mu.Lock()
	needRestore := defaultChecker.fullscreenToggled
	defaultChecker.mu.Unlock()
	if !needRestore {
		return
	}
	log.Info().Msg("Agent shutting down; restoring window state")
	defaultChecker.handlePostStop(nil, maa.TaskerTaskDetail{Entry: "AgentCleanup"}, true)
}
