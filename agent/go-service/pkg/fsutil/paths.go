package fsutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
)

var installDir, installDirErr = os.Getwd()
var userDataDir, userDataDirErr = resolveUserDataDir(os.Getenv(pienv.EnvUserDataDir))

func resolveUserDataDir(dir string) (string, error) {
	if dir == "" {
		return installDir, installDirErr
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%s must be an absolute path: %q", pienv.EnvUserDataDir, dir)
	}
	return filepath.Clean(dir), nil
}

// InstallDir returns the working directory captured at startup, which the host
// sets to the install root.
func InstallDir() (string, error) {
	return installDir, installDirErr
}

// InstallPath joins relative path components under the install root.
func InstallPath(parts ...string) string {
	return joinPath(installDir, installDirErr, parts)
}

// UserDataDir returns PI_USER_DATA_DIR, falling back to the install root when
// the Client does not inject it. Check its error during startup.
func UserDataDir() (string, error) {
	return userDataDir, userDataDirErr
}

// UserDataPath joins relative path components under the user data directory.
func UserDataPath(parts ...string) string {
	return joinPath(userDataDir, userDataDirErr, parts)
}

func joinPath(base string, err error, parts []string) string {
	if err != nil {
		panic(err)
	}
	return filepath.Join(append([]string{base}, parts...)...)
}
