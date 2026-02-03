package main

import (
	"os"
	"path/filepath"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/aspectratio"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/hdrcheck"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/importtask"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/itemtransfer"
	puzzle "github.com/MaaXYZ/MaaEnd/agent/go-service/puzzle-solver"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/realtime"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/resell"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func main() {
	logFile, err := initLogger()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize logger")
	}
	defer logFile.Close()

	log.Info().Str("version", Version).Msg("MaaEnd Agent Service")

	if len(os.Args) < 2 {
		log.Fatal().Msg("Usage: go-service <identifier>")
	}

	identifier := os.Args[1]
	log.Info().Str("identifier", identifier).Msg("Starting agent server")

	// Initialize MAA framework first (required before any other MAA calls)
	// MAA DLL 位于工作目录下的 maafw 子目录
	libDir := filepath.Join(getCwd(), "maafw")
	log.Info().Str("libDir", libDir).Msg("Initializing MAA framework")
	if err := maa.Init(maa.WithLibDir(libDir)); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize MAA framework")
	}
	defer maa.Release()
	log.Info().Msg("MAA framework initialized")

	// Initialize toolkit config option
	userPath := getCwd()
	if err := maa.ConfigInitOption(userPath, "{}"); err != nil {
		log.Warn().
			Str("userPath", userPath).
			Err(err).
			Msg("Failed to init toolkit config option")
	} else {
		log.Info().Str("userPath", userPath).Msg("Toolkit config option initialized")
	}

	// Register custom recognition and actions
	registerAll()

	// Start the agent server
	if err := maa.AgentServerStartUp(identifier); err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to start agent server")
	}
	log.Info().Msg("Agent server started")

	// Wait for the server to finish
	maa.AgentServerJoin()

	// Shutdown
	maa.AgentServerShutDown()
	log.Info().Msg("Agent server shutdown")
}

func registerAll() {
	// Register all custom components from each package
	realtime.Register()
	importtask.Register()
	resell.Register()
	puzzle.Register()
	itemtransfer.Register()

	// Register aspect ratio checker (uses TaskerSink, not custom action/recognition)
	aspectratio.Register()

	// Register HDR checker (uses TaskerSink, warns if HDR is enabled but doesn't stop task)
	hdrcheck.Register()

	log.Info().Msg("Registered custom recognition and actions")
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
