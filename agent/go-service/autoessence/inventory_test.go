package autoessence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAddCount(t *testing.T) {
	t.Cleanup(Reset)

	dir := t.TempDir()
	path := filepath.Join(dir, "EssenceInventory.json")
	content := `[
  {
    "weapon_ids": ["wpn_sword_0016", "wpn_sword_0012"],
    "essences": [
      {"levels": [3, 2, 1], "count": 2},
      {"levels": [6, 6, 3], "count": 1}
    ]
  }
]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if !Loaded() {
		t.Fatal("expected loaded")
	}
	if got := Count([]string{"wpn_sword_0012"}); got != 3 {
		t.Fatalf("expected levels merged count 3, got %d", got)
	}
	if got := Count([]string{"wpn_missing"}); got != 0 {
		t.Fatalf("unknown weapon should be 0, got %d", got)
	}
	if Count(nil) != 0 {
		t.Fatal("empty weapon_ids must not match all")
	}

	Add([]string{"wpn_sword_0012", "wpn_sword_0016"}, 1)
	if got := Count([]string{"wpn_sword_0016"}); got != 4 {
		t.Fatalf("expected add to increase count to 4, got %d", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Cleanup(Reset)
	path := filepath.Join(t.TempDir(), "missing.json")
	if err := LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if !Loaded() {
		t.Fatal("missing file should still mark loaded")
	}
	if Count([]string{"wpn_sword_0012"}) != 0 {
		t.Fatal("empty inventory should have zero count")
	}
}
