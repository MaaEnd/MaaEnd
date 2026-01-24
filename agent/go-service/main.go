package main

import (
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/MaaXYZ/maa-framework-go/v3"
)

func main() {
	cleanup, err := initLogger()
	if err != nil {
		slog.Error("Failed to initialize logger", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	slog.Info("MaaEnd Agent Service", "version", Version)

	if len(os.Args) < 2 {
		slog.Error("Usage: go-service <identifier>")
		os.Exit(1)
	}

	identifier := os.Args[1]
	slog.Info("Starting agent server", "identifier", identifier)

	// Initialize MAA framework first (required before any other MAA calls)
	// MAA DLL 位于工作目录下的 maafw 子目录
	libDir := filepath.Join(getCwd(), "maafw")
	slog.Info("Initializing MAA framework", "libDir", libDir)
	if err := maa.Init(maa.WithLibDir(libDir)); err != nil {
		slog.Error("Failed to initialize MAA framework", "error", err)
		os.Exit(1)
	}
	defer maa.Release()
	slog.Info("MAA framework initialized")

	// Initialize toolkit config option
	userPath := getCwd()
	if ok := maa.ConfigInitOption(userPath, "{}"); !ok {
		slog.Warn("Failed to init toolkit config option", "userPath", userPath)
	} else {
		slog.Info("Toolkit config option initialized", "userPath", userPath)
	}

	// Register custom recognition and actions
	maa.AgentServerRegisterCustomRecognition("MyRecognition", &myRecognition{})
	maa.AgentServerRegisterCustomAction("MyAction", &myAction{})
	slog.Info("Registered custom recognition and actions")

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, shutdownSignals...)

	go func() {
		sig := <-sigChan
		slog.Info("Received signal, initiating shutdown", "signal", sig.String())
		maa.AgentServerShutDown()
	}()

	// Start the agent server
	if !maa.AgentServerStartUp(identifier) {
		slog.Error("Failed to start agent server")
		os.Exit(1)
	}
	slog.Info("Agent server started")

	// Wait for the server to finish
	maa.AgentServerJoin()

	// Shutdown (idempotent, safe to call even if already shut down by signal)
	maa.AgentServerShutDown()
	slog.Info("Agent server shutdown complete")
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
