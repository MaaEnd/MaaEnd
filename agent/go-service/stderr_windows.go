package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func redirectStderr() {
	debugDir := filepath.Join(".", "debug")
	os.MkdirAll(debugDir, 0755)

	stderrFile, err := os.OpenFile(
		filepath.Join(debugDir, "go-service.stderr.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644,
	)
	if err != nil {
		return
	}
	windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(stderrFile.Fd()))
	os.Stderr = stderrFile
}
