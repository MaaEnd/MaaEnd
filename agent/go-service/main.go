package main

import (
	"os"
	"runtime"
	"runtime/debug"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/exetask"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/parentwatch"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/rs/zerolog/log"
)

const usage = "Usage: go-service <identifier> | go-service --exetask <taskname> [args...]"

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

	// 父进程一旦退出立刻结束自己，避免 MXU/MFAA 崩溃后 go-service 残留。
	parentwatch.Start()

	pienv.Init()
	i18n.Init()

	if len(os.Args) < 2 {
		log.Fatal().Msg(usage)
	}

	switch os.Args[1] {
	case "--exetask":
		exetask.Run(os.Args[2:])
	default:
		runAgent(os.Args[1])
	}
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
