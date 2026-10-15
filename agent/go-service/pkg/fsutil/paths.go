package fsutil

import (
	"os"
	"path/filepath"
)

var startupDir, startupDirErr = os.Getwd()

// StartupDir returns the working directory captured during process startup.
// An error means output paths cannot be resolved and startup must be aborted.
func StartupDir() (string, error) {
	return startupDir, startupDirErr
}

// OutputPath joins relative path components under the startup working directory.
// Callers must check StartupDir's error before using output paths.
func OutputPath(parts ...string) string {
	if startupDirErr != nil {
		panic(startupDirErr)
	}
	return filepath.Join(append([]string{startupDir}, parts...)...)
}
