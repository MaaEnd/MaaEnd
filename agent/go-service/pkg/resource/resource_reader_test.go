package resource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadJsonResourceAcceptsJsonc(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.json")
	// JSONC: line comment + trailing comma.
	data := `{
	  // a comment
	  "a": 1,
	  "b": [1, 2,],
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := ReadJsonResource(path, &out); err != nil {
		t.Fatalf("ReadJsonResource: %v", err)
	}
	if out["a"].(float64) != 1 {
		t.Fatalf("a=%v want 1", out["a"])
	}
}

func TestReadJsonResourceAcceptsBOM(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bom.json")
	if err := os.WriteFile(path, []byte("\xEF\xBB\xBF{\"x\": 2}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err := ReadJsonResource(path, &out); err != nil {
		t.Fatalf("ReadJsonResource BOM: %v", err)
	}
	if out["x"].(float64) != 2 {
		t.Fatalf("x=%v want 2", out["x"])
	}
}
