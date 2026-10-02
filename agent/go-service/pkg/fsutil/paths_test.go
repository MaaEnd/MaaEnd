package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUserDataDirFallsBackToInstallDir(t *testing.T) {
	got, err := resolveUserDataDir("")
	if err != nil {
		t.Fatalf("resolveUserDataDir(\"\") returned error: %v", err)
	}
	if got != installDir {
		t.Fatalf("resolveUserDataDir(\"\") = %q, want install dir %q", got, installDir)
	}
}

func TestResolveUserDataDirCleansAbsolutePath(t *testing.T) {
	dir := t.TempDir()

	got, err := resolveUserDataDir(dir + string(filepath.Separator) + ".")
	if err != nil {
		t.Fatalf("resolveUserDataDir(%q) returned error: %v", dir, err)
	}
	if want := filepath.Clean(dir); got != want {
		t.Fatalf("resolveUserDataDir(%q) = %q, want %q", dir, got, want)
	}
}

func TestResolveUserDataDirRejectsRelativePath(t *testing.T) {
	if _, err := resolveUserDataDir(filepath.Join("relative", "dir")); err == nil {
		t.Fatal("resolveUserDataDir(relative path) returned nil error")
	}
}

func TestInstallPathJoinsUnderWorkingDirectory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if got, want := InstallPath("resource", "image"), filepath.Join(wd, "resource", "image"); got != want {
		t.Fatalf("InstallPath(\"resource\", \"image\") = %q, want %q", got, want)
	}
}
