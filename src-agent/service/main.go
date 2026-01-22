package main

import (
	"os"

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
		log.Fatal().Msg("Usage: service <identifier>")
	}

	identifier := os.Args[1]
	log.Info().Str("identifier", identifier).Msg("Starting agent server")

	// Register custom recognition and actions
	maa.AgentServerRegisterCustomRecognition("MyRecognition", &myRecognition{})
	maa.AgentServerRegisterCustomAction("MyAction", &myAction{})
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
