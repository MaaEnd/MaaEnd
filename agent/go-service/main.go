package main

import (
	"os"
	"path/filepath"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/realtime"
	"github.com/MaaXYZ/maa-framework-go/v3"
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
	if ok := maa.ConfigInitOption(userPath, "{}"); !ok {
		log.Warn().Str("userPath", userPath).Msg("Failed to init toolkit config option")
	} else {
		log.Info().Str("userPath", userPath).Msg("Toolkit config option initialized")
	}

	// Register custom recognition and actions
	maa.AgentServerRegisterCustomAction("RealTimeAutoFightEndSkillAction", &realtime.RealTimeAutoFightEndSkillAction{})
	maa.AgentServerRegisterCustomAction("RealTimeAutoFightSkillAction", &realtime.RealTimeAutoFightSkillAction{})
	log.Info().Msg("Registered custom recognition and actions")

	// Start the agent server
	if !maa.AgentServerStartUp(identifier) {
		log.Fatal().Msg("Failed to start agent server")
	}
	log.Info().Msg("Agent server started")

	// Wait for the server to finish
	maa.AgentServerJoin()

	// Shutdown
	maa.AgentServerShutDown()
	log.Info().Msg("Agent server shutdown")
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
