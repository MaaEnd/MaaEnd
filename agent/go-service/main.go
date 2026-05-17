package main

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/taskersink/aspectratio"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/bytedance/sonic"
	"github.com/rs/zerolog/log"
)

func main() {
	if _, ok := os.LookupEnv("GOTRACEBACK"); !ok {
		debug.SetTraceback("crash")
	}
	debug.SetPanicOnFault(true)

	logFile, err := initLogger()
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to initialize logger")
	}
	defer logFile.Close()

	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 64<<10)
			for {
				n := runtime.Stack(buf, true)
				if n < len(buf) {
					buf = buf[:n]
					break
				}
				buf = make([]byte, 2*len(buf))
			}
			log.Error().
				Interface("panic", r).
				Str("stack", string(buf)).
				Msg("FATAL: go-service panicked")
			if err := logFile.Sync(); err != nil {
				log.Error().
					Err(err).
					Msg("FATAL: failed to sync log file")
			}
			panic(r)
		}
	}()

	if err := redirectStderr(); err != nil {
		log.Warn().
			Err(err).
			Msg("Failed to redirect stderr to file")
	}

	log.Info().
		Str("version", Version).
		Msg("MaaEnd Agent Service")

	pienv.Init()
	i18n.Init()

	if len(os.Args) < 2 {
		log.Fatal().Msg("Usage: go-service <identifier>")
	}

	identifier := os.Args[1]
	log.Info().
		Str("identifier", identifier).
		Msg("Starting agent server")

	// Initialize MAA framework first (required before any other MAA calls)
	// MAA DLL 位于工作目录下的 maafw 子目录
	libDir := filepath.Join(getCwd(), "maafw")
	log.Info().
		Str("libDir", libDir).
		Msg("Initializing MAA framework")
	if err := maa.Init(
		maa.WithLibDir(libDir),
		maa.WithJSONEncoder(sonic.Marshal),
		maa.WithJSONDecoder(sonic.Unmarshal),
	); err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to initialize MAA framework")
	}
	defer maa.Release()
	log.Info().
		Msg("MAA framework initialized")

	// Initialize toolkit config option
	userPath := getCwd()
	if err := maa.ConfigInitOption(userPath, "{}"); err != nil {
		log.Warn().
			Str("userPath", userPath).
			Err(err).
			Msg("Failed to init toolkit config option")
	} else {
		log.Info().
			Str("userPath", userPath).
			Msg("Toolkit config option initialized")
	}

	// Register all custom components and sinks
	registerAll()

	// Start the agent server
	if err := maa.AgentServerStartUp(identifier); err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to start agent server")
	}
	log.Info().
		Msg("Agent server started")

	// Wait for the server to finish
	maa.AgentServerJoin()

	// Restoring any window state we changed during the session.
	aspectratio.Cleanup()

	// Shutdown
	maa.AgentServerShutDown()
	log.Info().
		Msg("Agent server shutdown")
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
