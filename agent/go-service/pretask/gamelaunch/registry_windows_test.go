package gamelaunch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLauncherPathFromInstallLocation(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "Launcher.exe")
	if err := os.WriteFile(launcher, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{dir, `"` + dir + `"`, "  " + dir + "  "} {
		got, err := launcherPathFromInstallLocation(value)
		if err != nil {
			t.Fatalf("resolve %q: %v", value, err)
		}
		if !strings.EqualFold(got, launcher) {
			t.Fatalf("resolve %q = %q, want %q", value, got, launcher)
		}
	}
}

func TestLauncherPathFromInstallLocationRejectsMissingFile(t *testing.T) {
	if _, err := launcherPathFromInstallLocation(t.TempDir()); err == nil {
		t.Fatal("missing Launcher.exe accepted")
	}
}

func TestInstalledOfficialLauncherRegistration(t *testing.T) {
	expected := os.Getenv("MAAEND_TEST_LAUNCHER")
	if expected == "" {
		t.Skip("set MAAEND_TEST_LAUNCHER to compare installer registration with an installed official launcher")
	}
	got, err := installedLauncher()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(expected)) {
		t.Fatalf("registry launcher = %q, want %q", got, expected)
	}
}
