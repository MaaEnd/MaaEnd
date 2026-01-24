package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// multiHandler dispatches log records to multiple handlers based on level.
type multiHandler struct {
	console slog.Handler // Error level and above
	file    slog.Handler // Debug level and above
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level) || h.file.Enabled(ctx, level)
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.file.Enabled(ctx, r.Level) {
		if err := h.file.Handle(ctx, r); err != nil {
			return err
		}
	}
	if h.console.Enabled(ctx, r.Level) {
		if err := h.console.Handle(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &multiHandler{
		console: h.console.WithAttrs(attrs),
		file:    h.file.WithAttrs(attrs),
	}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	return &multiHandler{
		console: h.console.WithGroup(name),
		file:    h.file.WithGroup(name),
	}
}

// initLogger initializes the logger with console and file outputs.
// Console outputs Error level and above as text.
// File outputs Debug level and above as JSON with log rotation.
// Returns a cleanup function to close the log file.
func initLogger() (func(), error) {
	debugDir := filepath.Join(".", "debug")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return nil, err
	}

	logPath := filepath.Join(debugDir, "go-service.log")

	// lumberjack handles log rotation
	lj := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10,   // 10MB
		MaxBackups: 3,    // 3 backups
		LocalTime:  true, // Use local time for backup file names
	}

	// Console handler: Error level and above, text format
	consoleHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelError,
		AddSource: true,
	})

	// File handler: Debug level and above, JSON format
	fileHandler := slog.NewJSONHandler(lj, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})

	multi := &multiHandler{
		console: consoleHandler,
		file:    fileHandler,
	}

	slog.SetDefault(slog.New(multi))

	cleanup := func() {
		if err := lj.Close(); err != nil {
			slog.Error("Failed to close log file", "error", err)
		}
	}

	return cleanup, nil
}
