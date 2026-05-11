package aspectratio

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	_ maa.TaskerEventSink = &AspectRatioChecker{}

	// defaultChecker is the singleton registered with the tasker. Held at
	// package scope so Cleanup() can reach it from main without plumbing.
	defaultChecker *AspectRatioChecker
)

// Register registers the aspect ratio checker as a tasker sink.
func Register() {
	defaultChecker = &AspectRatioChecker{}
	maa.AgentServerAddTaskerSink(defaultChecker)
}

// Cleanup mirrors the C++ Win32 input modules' destructor behavior
// (`MessageInput::~MessageInput` → `restore_pos()`): when the agent's
// lifecycle ends, restore any window state we changed during the session.
//
// Intended to be called right after `maa.AgentServerJoin()` returns in main —
// at that point the controller may already be tearing down, but our Alt+Enter
// and SetWindowPos calls go directly to user32 with the cached HWND, so they
// still work as long as the game process is alive.
func Cleanup() {
	if defaultChecker == nil {
		return
	}
	defaultChecker.mu.Lock()
	needRestore := defaultChecker.resized || defaultChecker.fullscreenToggled
	defaultChecker.mu.Unlock()
	if !needRestore {
		return
	}
	log.Info().Msg("Agent shutting down; restoring window state")
	defaultChecker.handlePostStop(maa.TaskerTaskDetail{Entry: "AgentCleanup"})
}
