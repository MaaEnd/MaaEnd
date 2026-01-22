package main

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func initLogger() (*os.File, error) {
	debugDir := filepath.Join(".", "debug")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return nil, err
	}

	logPath := filepath.Join(debugDir, "service.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	// Output to both console and file
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}
	multi := io.MultiWriter(consoleWriter, logFile)

	log.Logger = zerolog.New(multi).
		With().
		Timestamp().
		Caller().
		Logger()

	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	return logFile, nil
}
