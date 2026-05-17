//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func redirectStderr() error {
	debugDir := filepath.Join(".", "debug")
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return fmt.Errorf("mkdir debug: %w", err)
	}

	stderrFile, err := os.OpenFile(
		filepath.Join(debugDir, "go-service.stderr.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644,
	)
	if err != nil {
		return fmt.Errorf("open stderr log: %w", err)
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(stderrFile.Fd())); err != nil {
		stderrFile.Close()
		return fmt.Errorf("set stderr handle: %w", err)
	}
	os.Stderr = stderrFile
	return nil
}
