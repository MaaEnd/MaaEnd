package main

import (
	"os"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/rs/zerolog/log"
)

const usage = "Usage: go-service --agent <identifier> | --pretask <taskname> [args...]"

func main() {
	logFile, err := initLogger()
	if err != nil {
		log.Fatal().
			Err(err).
			Msg("Failed to initialize logger")
	}
	defer logFile.Close()

	log.Info().
		Str("version", Version).
		Msg("MaaEnd Agent Service")

	pienv.Init()
	i18n.Init()

	if len(os.Args) < 2 {
		log.Fatal().Msg(usage)
	}

	mode := os.Args[1]
	switch mode {
	case "--agent":
		if len(os.Args) < 3 {
			log.Fatal().
				Msg("Usage: go-service --agent <identifier>")
		}
		runAgent(os.Args[2])
	case "--pretask":
		runPretask(os.Args[2:])
	default:
		log.Fatal().
			Str("arg", mode).
			Msg("Unknown mode, expected --agent or --pretask")
	}
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
