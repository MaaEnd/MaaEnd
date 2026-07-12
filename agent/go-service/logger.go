package main

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// levelFilterWriter 根据日志级别过滤输出，只有达到指定级别的日志才会写入
type levelFilterWriter struct {
	writer   io.Writer
	minLevel zerolog.Level
}

func (w *levelFilterWriter) Write(p []byte) (n int, err error) {
	return w.writer.Write(p)
}

func (w *levelFilterWriter) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	if level >= w.minLevel {
		return w.writer.Write(p)
	}
	return len(p), nil
}

func initLogger() (*os.File, error) {
	debugDir := filepath.Join(".", "debug")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return nil, err
	}

	logPath := filepath.Join(debugDir, "go-service.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	// 控制台只输出 Error 及以上级别的日志
	consoleWriter := &levelFilterWriter{
		writer: zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		},
		minLevel: zerolog.ErrorLevel,
	}

	// 文件输出所有级别的日志
	multi := zerolog.MultiLevelWriter(consoleWriter, logFile)

	log.Logger = zerolog.New(multi).
		With().
		Timestamp().
		Caller().
		Logger()

	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	return logFile, nil
}
