package i18n

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSiblingLocaleDir(t *testing.T) {
	root := t.TempDir()
	goDir := filepath.Join(root, "go-service")
	ifaceDir := filepath.Join(root, "interface")
	if err := os.MkdirAll(goDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ifaceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goDir, DefaultLang+".json"), []byte(`{"a":"1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ifaceDir, DefaultLang+".json"), []byte(`{"b":"2"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := siblingLocaleDir(goDir, "interface")
	if got != ifaceDir {
		t.Fatalf("got %q want %q", got, ifaceDir)
	}
	if siblingLocaleDir(goDir, "missing") != "" {
		t.Fatal("expected empty for missing sibling")
	}
}
